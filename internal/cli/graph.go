package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/jlim/claude-forge/internal/state"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(graphCmd)
	graphCmd.Flags().Bool("all", false, "Show project structure with all recipe internals")
}

var graphCmd = &cobra.Command{
	Use:     "graph [recipe-id]",
	Short:   "Visualize document relationships (Mermaid output)",
	GroupID: "tools",
	Args:    cobra.MaximumNArgs(1),
	RunE:    runGraph,
}

func runGraph(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	root, err := state.FindRoot(cwd)
	if err != nil {
		return fmt.Errorf("not a forge project: %w", err)
	}

	if len(args) > 0 {
		fmt.Println(renderRecipeGraph(root, args[0]))
		return nil
	}

	all, _ := cmd.Flags().GetBool("all")
	if all {
		fmt.Println(renderFullGraph(root))
	} else {
		fmt.Println(renderProjectGraph(root))
	}
	return nil
}

// renderProjectGraph shows vision → roadmap → recipes with inter-recipe refs.
func renderProjectGraph(root string) string {
	var lines []string
	lines = append(lines, "```mermaid")
	lines = append(lines, "flowchart TD")

	// Vision & Roadmap
	if state.VisionExists(root) {
		lines = append(lines, `    vision["vision.md"]`)
	}
	if state.RoadmapExists(root) {
		done, total, _ := state.RoadmapProgress(root)
		label := "roadmap.md"
		if total > 0 {
			label = fmt.Sprintf("roadmap.md %d/%d", done, total)
		}
		lines = append(lines, fmt.Sprintf(`    roadmap["%s"]`, label))
		if state.VisionExists(root) {
			lines = append(lines, "    vision --> roadmap")
		}
	}

	// Recipes
	recipes, _ := state.ListRecipes(root)
	sort.Slice(recipes, func(i, j int) bool {
		return recipes[i].StartedAt < recipes[j].StartedAt
	})

	recipeIDs := make(map[string]bool)
	for _, r := range recipes {
		recipeIDs[r.ID] = true
		nid := nodeID("", r.ID)
		icon := phaseIcon(r.Phase)
		topic := truncateGraph(r.Topic, 30)
		label := fmt.Sprintf("%s %s\\n%s\\n%s · %s", r.ID, icon, topic, r.Type, r.Phase)
		lines = append(lines, fmt.Sprintf(`    %s["%s"]`, nid, label))

		if state.RoadmapExists(root) {
			lines = append(lines, fmt.Sprintf("    roadmap --> %s", nid))
		}
	}

	// Cross-recipe references (fix → parent)
	for _, r := range recipes {
		if r.RefRecipe != "" && recipeIDs[r.RefRecipe] {
			lines = append(lines, fmt.Sprintf("    %s -.ref.-> %s",
				nodeID("", r.RefRecipe), nodeID("", r.ID)))
		}
	}

	lines = append(lines, "```")
	return strings.Join(lines, "\n")
}

// renderRecipeGraph shows documents and their relationships within a recipe.
func renderRecipeGraph(root, recipeID string) string {
	manifest, err := state.LoadManifest(root, recipeID)
	if err != nil || len(manifest.Documents) == 0 {
		return fmt.Sprintf("No documents found for recipe %s.", recipeID)
	}

	var lines []string
	lines = append(lines, "```mermaid")
	lines = append(lines, "flowchart TD")

	// Collect and sort document paths for deterministic output
	var paths []string
	for p := range manifest.Documents {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	// Emit nodes
	for _, p := range paths {
		entry := manifest.Documents[p]
		nid := nodeID("", p)
		label := p + "\\n" + entry.Type
		if entry.VerifiedBy != "" {
			label += " ✓"
		}
		lines = append(lines, fmt.Sprintf(`    %s["%s"]`, nid, label))
	}

	lines = append(lines, "")

	// Emit edges
	for _, p := range paths {
		entry := manifest.Documents[p]
		nid := nodeID("", p)

		// based_on: solid arrow (parent → this)
		for _, dep := range entry.BasedOn {
			if _, ok := manifest.Documents[dep]; ok {
				lines = append(lines, fmt.Sprintf("    %s --> %s", nodeID("", dep), nid))
			}
		}

		// verified_by: dotted arrow
		if entry.VerifiedBy != "" {
			if _, ok := manifest.Documents[entry.VerifiedBy]; ok {
				lines = append(lines, fmt.Sprintf("    %s -.verified_by.-> %s", nid, nodeID("", entry.VerifiedBy)))
			}
		}

		// incorporates: dotted arrow
		for _, inc := range entry.Incorporates {
			if _, ok := manifest.Documents[inc]; ok {
				lines = append(lines, fmt.Sprintf("    %s -.incorporates.-> %s", nodeID("", inc), nid))
			}
		}
	}

	lines = append(lines, "```")
	return strings.Join(lines, "\n")
}

// renderFullGraph combines project structure with recipe internals.
func renderFullGraph(root string) string {
	var lines []string
	lines = append(lines, "```mermaid")
	lines = append(lines, "flowchart TD")

	// Vision & Roadmap
	if state.VisionExists(root) {
		lines = append(lines, `    vision["vision.md"]`)
	}
	if state.RoadmapExists(root) {
		done, total, _ := state.RoadmapProgress(root)
		label := "roadmap.md"
		if total > 0 {
			label = fmt.Sprintf("roadmap.md %d/%d", done, total)
		}
		lines = append(lines, fmt.Sprintf(`    roadmap["%s"]`, label))
		if state.VisionExists(root) {
			lines = append(lines, "    vision --> roadmap")
		}
	}

	// Recipes as subgraphs
	recipes, _ := state.ListRecipes(root)
	sort.Slice(recipes, func(i, j int) bool {
		return recipes[i].StartedAt < recipes[j].StartedAt
	})

	recipeIDs := make(map[string]bool)
	for _, r := range recipes {
		recipeIDs[r.ID] = true
		prefix := nodeID("", r.ID) + "_"
		icon := phaseIcon(r.Phase)
		topic := truncateGraph(r.Topic, 25)
		sgLabel := fmt.Sprintf("%s %s %s", r.ID, icon, topic)

		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf(`    subgraph %s["%s"]`, nodeID("sg", r.ID), sgLabel))

		manifest, _ := state.LoadManifest(root, r.ID)
		if len(manifest.Documents) > 0 {
			var paths []string
			for p := range manifest.Documents {
				paths = append(paths, p)
			}
			sort.Strings(paths)

			// Nodes
			for _, p := range paths {
				entry := manifest.Documents[p]
				nid := prefix + nodeID("", p)
				label := p + "\\n" + entry.Type
				lines = append(lines, fmt.Sprintf(`        %s["%s"]`, nid, label))
			}

			// Edges within recipe
			for _, p := range paths {
				entry := manifest.Documents[p]
				nid := prefix + nodeID("", p)
				for _, dep := range entry.BasedOn {
					if _, ok := manifest.Documents[dep]; ok {
						lines = append(lines, fmt.Sprintf("        %s --> %s", prefix+nodeID("", dep), nid))
					}
				}
				if entry.VerifiedBy != "" {
					if _, ok := manifest.Documents[entry.VerifiedBy]; ok {
						lines = append(lines, fmt.Sprintf("        %s -.-> %s", nid, prefix+nodeID("", entry.VerifiedBy)))
					}
				}
			}
		} else {
			// No manifest — show recipe as single node
			lines = append(lines, fmt.Sprintf(`        %sempty["(no documents)"]`, prefix))
		}

		lines = append(lines, "    end")

		// Connect roadmap → first document in recipe
		if state.RoadmapExists(root) && len(manifest.Documents) > 0 {
			// Find the earliest document (by type priority: research > draft > other)
			first := findFirstDoc(manifest)
			if first != "" {
				lines = append(lines, fmt.Sprintf("    roadmap --> %s", prefix+nodeID("", first)))
			}
		}
	}

	// Cross-recipe refs
	for _, r := range recipes {
		if r.RefRecipe != "" && recipeIDs[r.RefRecipe] {
			lines = append(lines, fmt.Sprintf("    %s -.ref.-> %s",
				nodeID("sg", r.RefRecipe), nodeID("sg", r.ID)))
		}
	}

	lines = append(lines, "```")
	return strings.Join(lines, "\n")
}

// nodeID creates a valid Mermaid node identifier from a prefix and path.
func nodeID(prefix, path string) string {
	r := strings.NewReplacer(
		"/", "_", ".", "_", "-", "_", " ", "_",
	)
	id := r.Replace(path)
	if prefix != "" {
		return prefix + "_" + id
	}
	return id
}

// phaseIcon returns a status icon for the recipe phase.
func phaseIcon(phase string) string {
	switch phase {
	case "complete":
		return "✓"
	case "cancelled":
		return "✗"
	case "finalize":
		return "◆"
	default:
		return "●"
	}
}

// findFirstDoc returns the earliest/root document in a manifest.
func findFirstDoc(m *state.Manifest) string {
	// Priority: intent.md > scope.md > research > draft > anything
	priorities := []string{"intent.md", "scope.md"}
	for _, p := range priorities {
		if _, ok := m.Documents[p]; ok {
			return p
		}
	}
	// Find any research doc
	for p, e := range m.Documents {
		if e.Type == "research" {
			return p
		}
	}
	// Find any draft
	for p, e := range m.Documents {
		if e.Type == "draft" {
			return p
		}
	}
	// First alphabetically
	var paths []string
	for p := range m.Documents {
		paths = append(paths, p)
	}
	if len(paths) > 0 {
		sort.Strings(paths)
		return paths[0]
	}
	return ""
}

func truncateGraph(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
