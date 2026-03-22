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
	fix     string // resolution guidance
}

func runDoctor(cmd *cobra.Command, args []string) error {
	// System diagnostics (always)
	fmt.Println("bts doctor")
	fmt.Println("----------")
	fmt.Printf("bts version:  %s\n", version.GetFullVersion())
	fmt.Printf("Platform:     %s/%s (%s)\n", runtime.GOOS, runtime.GOARCH, runtime.Version())

	// Template version check
	cwd, _ := os.Getwd()
	btsRoot, btsErr := state.FindBTSRoot(cwd)
	if btsErr == nil {
		vf := filepath.Join(btsRoot, ".bts", "config", ".template-version")
		if data, err := os.ReadFile(vf); err == nil {
			tmplVer := strings.TrimSpace(string(data))
			binVer := version.GetTemplateVersion()
			if tmplVer == binVer {
				fmt.Printf("Templates:    %s (up to date)\n", tmplVer)
			} else {
				fmt.Printf("Templates:    %s (outdated, binary: %s)\n", tmplVer, binVer)
				fmt.Println("              → Run 'bts update' to refresh templates")
			}
		}
	}

	if claudePath, err := exec.LookPath("claude"); err == nil {
		fmt.Printf("Claude Code:  %s\n", claudePath)
	} else {
		fmt.Println("Claude Code:  NOT FOUND")
		fmt.Println("              → Install: npm install -g @anthropic-ai/claude-code")
	}
	if gitPath, err := exec.LookPath("git"); err == nil {
		fmt.Printf("Git:          %s\n", gitPath)
	} else {
		fmt.Println("Git:          NOT FOUND")
		fmt.Println("              → Install: https://git-scm.com/downloads")
	}

	// Recipe health checks
	if btsErr != nil {
		fmt.Println("\nNo .bts/ project found.")
		fmt.Println("  → Run 'bts init' to initialize bts in this project")
		return nil
	}

	strict, _ := cmd.Flags().GetBool("strict")

	var recipes []*state.RecipeState
	var err error
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

	// Check for multiple active recipes
	activeCount := 0
	for _, r := range recipes {
		if r.Phase != "finalize" && r.Phase != "complete" && r.Phase != "cancelled" && r.Phase != "" {
			activeCount++
		}
	}

	totalErrors, totalWarnings := 0, 0
	var quickFixes []string

	if activeCount > 1 {
		fmt.Printf("\n⚠ %d active recipes found (expected 1)\n", activeCount)
		fmt.Println("  → Cancel inactive recipes with 'bts recipe cancel'")
		totalWarnings++
		quickFixes = append(quickFixes, "bts recipe cancel")
	}

	for _, recipe := range recipes {
		recipeDir := state.RecipeDir(btsRoot, recipe.ID)
		fmt.Printf("\n── Recipe: %s (%s) \"%s\" — %s\n",
			recipe.ID, recipe.Type, truncate(recipe.Topic, 35), recipe.Phase)

		var issues []doctorIssue
		issues = append(issues, checkDocuments(recipeDir, recipe)...)

		manifest, _ := state.LoadManifest(btsRoot, recipe.ID)
		issues = append(issues, checkManifestConsistency(recipeDir, recipe.ID, manifest)...)
		issues = append(issues, checkVerifyLog(recipeDir, recipe.ID)...)
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
				if iss.fix != "" {
					fmt.Printf("     → %s\n", iss.fix)
				}
			}
		}

		for _, iss := range issues {
			if iss.level == "error" {
				totalErrors++
				if iss.fix != "" && len(quickFixes) < 3 {
					quickFixes = append(quickFixes, iss.fix)
				}
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
		fmt.Println("     → Run /bts-status to generate, or created during next /bts-recipe-blueprint scoping")
		totalWarnings++
	} else {
		fmt.Println("   ✓ project-map.md exists")
	}

	fmt.Printf("\n%d error(s), %d warning(s)\n", totalErrors, totalWarnings)

	if totalErrors > 0 && len(quickFixes) > 0 {
		fmt.Println("\nQuick fixes:")
		for _, fix := range quickFixes {
			fmt.Printf("  %s\n", fix)
		}
	}

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
		"finalize": 4, "implement": 5, "test": 6, "review": 7,
		"sync": 8, "status": 9, "complete": 10,
	}
	pw := phaseWeight[recipe.Phase]

	switch recipe.Type {
	case "fix":
		if pw >= 2 && !exists("diagnosis.md") {
			issues = append(issues, doctorIssue{"warning", "documents",
				"diagnosis.md — missing",
				"Run /bts-recipe-fix to start diagnosis"})
		}
		if pw >= 3 && !exists("fix-spec.md") {
			issues = append(issues, doctorIssue{"error", "documents",
				"fix-spec.md — missing",
				"Run /bts-recipe-fix to create fix spec"})
		}
		if pw >= 6 {
			issues = append(issues, checkTestFile(recipeDir)...)
		}
		if pw >= 8 && !exists("review.md") {
			issues = append(issues, doctorIssue{"warning", "documents",
				"review.md — missing",
				"Run /bts-review to generate code review"})
		}

	case "debug":
		if pw >= 2 && !exists("perspectives.md") {
			issues = append(issues, doctorIssue{"warning", "documents",
				"perspectives.md — missing",
				"Run /bts-recipe-debug to collect perspectives"})
		}
		if pw >= 3 && !exists("draft.md") {
			issues = append(issues, doctorIssue{"warning", "documents",
				"draft.md — missing",
				"Draft should exist at this phase"})
		}
		if pw >= 4 {
			if !exists("final.md") {
				issues = append(issues, doctorIssue{"error", "documents",
					"final.md — missing",
					"Complete /bts-recipe-debug to produce final.md"})
			}
			// verify-log checked separately with recipe ID
		}
		if pw >= 5 {
			issues = append(issues, checkTasks(recipeDir)...)
		}
		if pw >= 6 {
			issues = append(issues, checkTestFile(recipeDir)...)
		}
		if pw >= 8 && !exists("review.md") {
			issues = append(issues, doctorIssue{"error", "documents",
				"review.md — missing",
				"Run /bts-review to generate code review"})
		}

	default: // blueprint, analyze, design
		if pw >= 1 && recipe.Type == "blueprint" {
			if exists("scope.md") {
				data, _ := os.ReadFile(filepath.Join(recipeDir, "scope.md"))
				if len(data) > 0 {
					content := string(data)
					if strings.Contains(content, "Status: DRAFT") && !strings.Contains(content, "Status: CONFIRMED") {
						issues = append(issues, doctorIssue{"warning", "documents",
							"scope.md — Status: DRAFT (not confirmed)",
							"Confirm scope in /bts-recipe-blueprint"})
					}
				}
			}
		}
		if pw >= 3 && recipe.Type == "blueprint" && !exists("draft.md") {
			issues = append(issues, doctorIssue{"warning", "documents",
				"draft.md — missing",
				"Draft should exist at this phase"})
		}
		if pw >= 4 {
			if !exists("final.md") {
				issues = append(issues, doctorIssue{"error", "documents",
					"final.md — missing",
					fmt.Sprintf("Complete /bts-recipe-%s to produce final.md", recipe.Type)})
			}
			// verify-log checked separately with recipe ID
		}
		if pw >= 5 {
			issues = append(issues, checkTasks(recipeDir)...)
		}
		if pw >= 6 {
			issues = append(issues, checkTestFile(recipeDir)...)
		}
		if pw >= 8 && !exists("review.md") {
			issues = append(issues, doctorIssue{"error", "documents",
				"review.md — missing",
				"Run /bts-review to generate code review"})
		}
		if pw >= 8 && !exists("deviation.md") {
			issues = append(issues, doctorIssue{"warning", "documents",
				"deviation.md — missing",
				"Run /bts-sync to compare spec with code"})
		}
	}

	return issues
}

func checkVerifyLog(recipeDir string, recipeID string) []doctorIssue {
	var issues []doctorIssue
	logPath := filepath.Join(recipeDir, "verify-log.jsonl")
	changelogPath := filepath.Join(recipeDir, "changelog.jsonl")

	verifyCount := countActions(changelogPath, "verify")
	logCount := countLines(logPath)

	if verifyCount > 0 && logCount == 0 {
		issues = append(issues, doctorIssue{"error", "flow",
			fmt.Sprintf("verify-log.jsonl — %d verify in changelog, 0 in verify-log", verifyCount),
			fmt.Sprintf("bts recipe log %s --iteration 1 --critical 0 --major 0", recipeID)})
	}

	// Check verify-log last entry for unresolved issues
	if last, err := readLastVerify(logPath); err == nil {
		if last.Critical > 0 || last.Major > 0 {
			issues = append(issues, doctorIssue{"error", "flow",
				fmt.Sprintf("verify-log: %d critical, %d major unresolved", last.Critical, last.Major),
				"Fix issues and re-run /bts-verify"})
		}
	}

	return issues
}

func checkTasks(recipeDir string) []doctorIssue {
	var issues []doctorIssue
	path := filepath.Join(recipeDir, "tasks.json")
	data, err := os.ReadFile(path)
	if err != nil {
		issues = append(issues, doctorIssue{"error", "documents",
			"tasks.json — missing",
			"Run /bts-implement to decompose tasks"})
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
			fmt.Sprintf("tasks.json — %d task(s) blocked", blocked),
			"Run /bts-implement to retry or skip blocked tasks"})
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
			fmt.Sprintf("tests — %d/%d failed", tr.Failed, tr.Total),
			"Fix failing tests and re-run /bts-test"})
	}
	return issues
}

// checkManifestConsistency compares disk files vs manifest entries.
func checkManifestConsistency(recipeDir string, recipeID string, manifest *state.Manifest) []doctorIssue {
	var issues []doctorIssue

	// Files in manifest but not on disk
	for path := range manifest.Documents {
		fullPath := filepath.Join(recipeDir, path)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			issues = append(issues, doctorIssue{"error", "manifest",
				path + " — in manifest but not on disk",
				"Remove entry from manifest.json or recreate the file"})
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
					name + " — on disk but not in manifest",
					fmt.Sprintf("bts recipe log %s --action [type] --output %s", recipeID, name)})
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

	pw := phaseWeightOf(recipe.Phase)

	// Check: improve before finalize without verify
	for i, action := range actions {
		if action == "finalize" {
			for j := i - 1; j >= 0; j-- {
				if actions[j] == "verify" || actions[j] == "sync-check" {
					break
				}
				if actions[j] == "improve" {
					issues = append(issues, doctorIssue{"warning", "flow",
						"improve before finalize without verify",
						"Run /bts-verify before finalizing"})
					break
				}
			}
		}
	}

	// Check: implement without test
	if pw >= 6 && containsAction(actions, "implement") && !containsAction(actions, "test") {
		issues = append(issues, doctorIssue{"warning", "flow",
			"implement without test",
			"Run /bts-test to generate and execute tests"})
	}

	// Check: test without simulate
	if pw >= 7 && containsAction(actions, "test") && !containsAction(actions, "simulate") {
		issues = append(issues, doctorIssue{"warning", "flow",
			"test without code simulation",
			"Run /bts-simulate code to verify all code paths"})
	}

	// Check: test without review
	if pw >= 8 && containsAction(actions, "test") && !containsAction(actions, "review") {
		issues = append(issues, doctorIssue{"warning", "flow",
			"test without review",
			"Run /bts-review to check code quality"})
	}

	return issues
}

func phaseWeightOf(phase string) int {
	w := map[string]int{
		"scoping": 1, "research": 2, "draft": 3, "assess": 3, "improve": 3,
		"verify": 3, "debate": 3, "simulate": 3, "audit": 3,
		"finalize": 4, "implement": 5, "test": 6, "review": 7,
		"sync": 8, "status": 9, "complete": 10,
	}
	return w[phase]
}

func containsAction(actions []string, target string) bool {
	for _, a := range actions {
		if a == target {
			return true
		}
	}
	return false
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

func readLastVerify(path string) (*state.VerifyLogEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var last state.VerifyLogEntry
	found := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var entry state.VerifyLogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		last = entry
		found = true
	}
	if !found {
		return nil, fmt.Errorf("empty")
	}
	return &last, nil
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
