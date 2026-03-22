package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jlim/bts/internal/state"
	"github.com/jlim/bts/pkg/version"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(doctorCmd)
	doctorCmd.Flags().Bool("strict", false, "Treat warnings as errors")
}

var doctorCmd = &cobra.Command{
	Use:     "doctor [recipe-id]",
	Short:   "Run system diagnostics and recipe health checks",
	Long:    "Without arguments: system diagnostics. With recipe-id: inspect recipe documents, manifest, and flow for issues.",
	Args:    cobra.MaximumNArgs(1),
	GroupID: "tools",
	RunE:    runDoctor,
}

type doctorIssue struct {
	level   string // "error", "warning"
	section string // "documents", "manifest", "flow"
	message string
}

func runDoctor(cmd *cobra.Command, args []string) error {
	// System diagnostics (always)
	fmt.Println("bts doctor")
	fmt.Println("----------")
	fmt.Printf("bts version:  %s\n", version.GetFullVersion())
	fmt.Printf("Platform:     %s/%s (%s)\n", runtime.GOOS, runtime.GOARCH, runtime.Version())

	// Template version check
	{
		cwd, _ := os.Getwd()
		if dr, err := state.FindBTSRoot(cwd); err == nil {
			vf := filepath.Join(dr, ".bts", "config", ".template-version")
			if data, err := os.ReadFile(vf); err == nil {
				tmplVer := strings.TrimSpace(string(data))
				binVer := version.GetTemplateVersion()
				if tmplVer == binVer {
					fmt.Printf("Templates:    %s (up to date)\n", tmplVer)
				} else {
					fmt.Printf("Templates:    %s (outdated, binary: %s, run 'bts update')\n", tmplVer, binVer)
				}
			}
		}
	}

	if claudePath, err := exec.LookPath("claude"); err == nil {
		fmt.Printf("Claude Code:  %s\n", claudePath)
	} else {
		fmt.Println("Claude Code:  NOT FOUND")
	}
	if gitPath, err := exec.LookPath("git"); err == nil {
		fmt.Printf("Git:          %s\n", gitPath)
	} else {
		fmt.Println("Git:          NOT FOUND")
	}

	// Recipe health checks
	cwd, _ := os.Getwd()
	btsRoot, err := state.FindBTSRoot(cwd)
	if err != nil {
		fmt.Println("\nNo .bts/ project found. System checks only.")
		return nil
	}

	strict, _ := cmd.Flags().GetBool("strict")

	var recipes []*state.RecipeState
	if len(args) > 0 {
		r, err := state.LoadRecipeState(btsRoot, args[0])
		if err != nil {
			return fmt.Errorf("load recipe %s: %w", args[0], err)
		}
		recipes = append(recipes, r)
	} else {
		recipes, err = state.ListRecipes(btsRoot)
		if err != nil {
			return fmt.Errorf("list recipes: %w", err)
		}
	}

	if len(recipes) == 0 {
		fmt.Println("\nNo recipes found.")
		return nil
	}

	totalErrors, totalWarnings := 0, 0

	for _, recipe := range recipes {
		recipeDir := state.RecipeDir(btsRoot, recipe.ID)
		fmt.Printf("\n── Recipe: %s (%s) \"%s\" — %s\n",
			recipe.ID, recipe.Type, truncate(recipe.Topic, 35), recipe.Phase)

		var issues []doctorIssue
		issues = append(issues, checkDocuments(recipeDir, recipe)...)

		manifest, _ := state.LoadManifest(btsRoot, recipe.ID)
		issues = append(issues, checkManifestConsistency(recipeDir, manifest)...)
		issues = append(issues, checkFlowCompliance(recipeDir, recipe)...)

		if len(issues) == 0 {
			fmt.Println("   ✓ All checks pass")
		} else {
			for _, iss := range issues {
				mark := "✗"
				if iss.level == "warning" {
					mark = "⚠"
				}
				fmt.Printf("   %s [%s] %s\n", mark, iss.section, iss.message)
			}
		}

		for _, iss := range issues {
			if iss.level == "error" {
				totalErrors++
			} else {
				totalWarnings++
			}
		}
	}

	// Project-level checks
	fmt.Println()
	mapPath := filepath.Join(state.StatePath(btsRoot), "project-map.md")
	if _, err := os.Stat(mapPath); os.IsNotExist(err) {
		fmt.Println("   ⚠ project-map.md not found")
		totalWarnings++
	} else {
		fmt.Println("   ✓ project-map.md exists")
	}

	fmt.Printf("\n%d error(s), %d warning(s)\n", totalErrors, totalWarnings)

	if totalErrors > 0 || (strict && totalWarnings > 0) {
		os.Exit(1)
	}

	return nil
}

// checkDocuments verifies expected files exist based on recipe type and phase.
func checkDocuments(recipeDir string, recipe *state.RecipeState) []doctorIssue {
	var issues []doctorIssue

	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(recipeDir, name))
		return err == nil
	}

	phaseWeight := map[string]int{
		"scoping": 1, "research": 2, "draft": 3, "assess": 3, "improve": 3,
		"verify": 3, "debate": 3, "simulate": 3, "audit": 3,
		"finalize": 4, "implement": 5, "test": 6, "sync": 7, "status": 8, "complete": 9,
	}
	pw := phaseWeight[recipe.Phase]

	switch recipe.Type {
	case "fix":
		if pw >= 2 && !exists("diagnosis.md") {
			issues = append(issues, doctorIssue{"warning", "documents", "diagnosis.md — missing"})
		}
		if pw >= 3 && !exists("fix-spec.md") {
			issues = append(issues, doctorIssue{"error", "documents", "fix-spec.md — missing"})
		}
		if pw >= 6 {
			issues = append(issues, checkTestFile(recipeDir)...)
		}

	case "debug":
		if pw >= 2 && !exists("perspectives.md") {
			issues = append(issues, doctorIssue{"warning", "documents", "perspectives.md — missing"})
		}
		if pw >= 4 {
			if !exists("final.md") {
				issues = append(issues, doctorIssue{"error", "documents", "final.md — missing"})
			}
			issues = append(issues, checkVerifyLog(recipeDir)...)
		}
		if pw >= 5 {
			issues = append(issues, checkTasks(recipeDir)...)
		}
		if pw >= 6 {
			issues = append(issues, checkTestFile(recipeDir)...)
		}

	default: // blueprint, analyze, design
		if pw >= 1 && recipe.Type == "blueprint" {
			if exists("scope.md") {
				// Check scope status
				data, _ := os.ReadFile(filepath.Join(recipeDir, "scope.md"))
				if len(data) > 0 {
					content := string(data)
					if strings.Contains(content, "Status: DRAFT") && !strings.Contains(content, "Status: CONFIRMED") {
						issues = append(issues, doctorIssue{"warning", "documents", "scope.md — Status: DRAFT (not confirmed)"})
					}
				}
			}
		}
		if pw >= 4 {
			if !exists("final.md") {
				issues = append(issues, doctorIssue{"error", "documents", "final.md — missing"})
			}
			issues = append(issues, checkVerifyLog(recipeDir)...)
		}
		if pw >= 5 {
			issues = append(issues, checkTasks(recipeDir)...)
		}
		if pw >= 6 {
			issues = append(issues, checkTestFile(recipeDir)...)
		}
		if pw >= 7 && !exists("deviation.md") {
			issues = append(issues, doctorIssue{"warning", "documents", "deviation.md — missing"})
		}
	}

	return issues
}

func checkVerifyLog(recipeDir string) []doctorIssue {
	var issues []doctorIssue
	logPath := filepath.Join(recipeDir, "verify-log.jsonl")
	changelogPath := filepath.Join(recipeDir, "changelog.jsonl")

	verifyCount := countActions(changelogPath, "verify")
	logCount := countLines(logPath)

	if verifyCount > 0 && logCount == 0 {
		issues = append(issues, doctorIssue{"error", "flow",
			fmt.Sprintf("verify-log.jsonl — %d verify in changelog, 0 in verify-log", verifyCount)})
	}
	return issues
}

func checkTasks(recipeDir string) []doctorIssue {
	var issues []doctorIssue
	path := filepath.Join(recipeDir, "tasks.json")
	data, err := os.ReadFile(path)
	if err != nil {
		issues = append(issues, doctorIssue{"error", "documents", "tasks.json — missing"})
		return issues
	}
	var ts state.TaskState
	if err := json.Unmarshal(data, &ts); err != nil {
		return issues
	}
	done, blocked, pending, total := 0, 0, 0, len(ts.Tasks)
	for _, t := range ts.Tasks {
		switch t.Status {
		case "done", "skipped":
			done++
		case "blocked":
			blocked++
		case "pending", "in_progress":
			pending++
		}
	}
	fmt.Printf("   · tasks: %d/%d done", done, total)
	if blocked > 0 {
		fmt.Printf(", %d blocked", blocked)
		issues = append(issues, doctorIssue{"warning", "documents",
			fmt.Sprintf("tasks.json — %d task(s) blocked", blocked)})
	}
	if pending > 0 {
		fmt.Printf(", %d pending", pending)
	}
	fmt.Println()
	return issues
}

func checkTestFile(recipeDir string) []doctorIssue {
	var issues []doctorIssue
	path := filepath.Join(recipeDir, "test-results.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return issues // optional at some phases
	}
	var tr state.TestResults
	if err := json.Unmarshal(data, &tr); err != nil {
		return issues
	}
	fmt.Printf("   · tests: %d/%d %s\n", tr.Passed, tr.Total, tr.Status)
	if tr.Status != "pass" {
		issues = append(issues, doctorIssue{"warning", "documents",
			fmt.Sprintf("tests — %d/%d failed", tr.Failed, tr.Total)})
	}
	return issues
}

// checkManifestConsistency compares disk files vs manifest entries.
func checkManifestConsistency(recipeDir string, manifest *state.Manifest) []doctorIssue {
	var issues []doctorIssue

	// Files in manifest but not on disk
	for path := range manifest.Documents {
		fullPath := filepath.Join(recipeDir, path)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			issues = append(issues, doctorIssue{"error", "manifest",
				path + " — in manifest but not on disk"})
		}
	}

	// Known files on disk but not in manifest
	knownFiles := []string{
		"scope.md", "final.md", "tasks.json", "test-results.json",
		"deviation.md", "diagnosis.md", "fix-spec.md", "perspectives.md", "review.md",
		"draft.md", "verification.md",
	}
	for _, name := range knownFiles {
		fullPath := filepath.Join(recipeDir, name)
		if _, err := os.Stat(fullPath); err == nil {
			if _, inManifest := manifest.Documents[name]; !inManifest {
				issues = append(issues, doctorIssue{"warning", "manifest",
					name + " — on disk but not in manifest"})
			}
		}
	}

	return issues
}

// checkFlowCompliance analyzes changelog action sequence.
func checkFlowCompliance(recipeDir string, recipe *state.RecipeState) []doctorIssue {
	var issues []doctorIssue
	changelogPath := filepath.Join(recipeDir, "changelog.jsonl")
	actions := readActions(changelogPath)

	// Check: improve before finalize without verify
	for i, action := range actions {
		if action == "finalize" {
			for j := i - 1; j >= 0; j-- {
				if actions[j] == "verify" || actions[j] == "sync-check" {
					break
				}
				if actions[j] == "improve" {
					issues = append(issues, doctorIssue{"warning", "flow",
						"improve before finalize without verify"})
					break
				}
			}
		}
	}

	return issues
}

func readActions(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var actions []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		if a, ok := raw["action"].(string); ok {
			actions = append(actions, a)
		}
	}
	return actions
}

func countActions(path, target string) int {
	count := 0
	for _, a := range readActions(path) {
		if a == target {
			count++
		}
	}
	return count
}

func countLines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			count++
		}
	}
	return count
}
