package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/imtemp-dev/claude-forge/internal/state"
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

	// Emit nodes (use short filename for label, full path for tooltip)
	for _, p := range paths {
		entry := manifest.Documents[p]
		nid := nodeID("", p)
		shortName := filepath.Base(p)
		label := shortName + "\\n" + entry.Type
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
// Shows only key documents per recipe for readability at scale.
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

	// Recipes as subgraphs with key documents only
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
		keyDocs := selectKeyDocs(manifest)
		if len(keyDocs) > 0 {
			for _, kd := range keyDocs {
				lines = append(lines, fmt.Sprintf(`        %s["%s"]`, prefix+nodeID("", kd.path), kd.label))
			}
			// Chain key docs linearly
			for i := 1; i < len(keyDocs); i++ {
				prev := prefix + nodeID("", keyDocs[i-1].path)
				curr := prefix + nodeID("", keyDocs[i].path)
				lines = append(lines, fmt.Sprintf("        %s --> %s", prev, curr))
			}
		} else {
			lines = append(lines, fmt.Sprintf(`        %sempty["(no documents)"]`, prefix))
		}

		lines = append(lines, "    end")

		// Connect roadmap → first key doc in subgraph
		if state.RoadmapExists(root) && len(keyDocs) > 0 {
			lines = append(lines, fmt.Sprintf("    roadmap --> %s", prefix+nodeID("", keyDocs[0].path)))
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

type keyDoc struct {
	path  string
	label string
}

// selectKeyDocs picks the most important documents for a compact view.
// Returns them in lifecycle order: research → draft → final → tasks → tests → review → deviation.
func selectKeyDocs(m *state.Manifest) []keyDoc {
	// Ordered by lifecycle stage. For each, find the first matching doc.
	candidates := []struct {
		paths []string // explicit paths to try
		dtype string   // fallback: match by document type
		label string   // display label
	}{
		{[]string{"scope.md"}, "research", "scope"},
		{[]string{"draft.md"}, "draft", "draft"},
		{[]string{"final.md"}, "", "final"},
		{[]string{"tasks.json"}, "implementation", "tasks"},
		{[]string{"test-results.json"}, "test-result", "tests"},
		{[]string{"review.md"}, "review", "review"},
		{[]string{"deviation.md"}, "deviation", "deviation"},
	}

	var result []keyDoc
	for _, c := range candidates {
		// Try explicit paths first
		for _, p := range c.paths {
			if _, ok := m.Documents[p]; ok {
				result = append(result, keyDoc{path: p, label: c.label})
				goto next
			}
		}
		// Fallback: find by type
		if c.dtype != "" {
			var paths []string
			for p, e := range m.Documents {
				if e.Type == c.dtype {
					paths = append(paths, p)
				}
			}
			if len(paths) > 0 {
				sort.Strings(paths)
				result = append(result, keyDoc{path: paths[0], label: c.label})
			}
		}
	next:
	}
	return result
}

func truncateGraph(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
