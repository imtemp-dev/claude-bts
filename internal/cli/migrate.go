package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/imtemp-dev/claude-jig/internal/engine"
	"github.com/imtemp-dev/claude-jig/internal/state"
	"github.com/spf13/cobra"
)

func init() {
	migrateCmd.AddCommand(migrateVerifyLogCmd)
	migrateVerifyLogCmd.Flags().String("target", "", "Target directory (defaults to CWD project root)")
	migrateVerifyLogCmd.Flags().Bool("dry-run", false, "Show what would change without writing")

	migrateCmd.AddCommand(migrateChangelogCmd)
	migrateChangelogCmd.Flags().String("target", "", "Target directory (defaults to CWD project root)")
	migrateChangelogCmd.Flags().Bool("dry-run", false, "Show what would change without writing")

	migrateCmd.AddCommand(migrateVerificationCmd)
	migrateVerificationCmd.Flags().String("target", "", "Target directory (defaults to CWD project root)")
	migrateVerificationCmd.Flags().Bool("dry-run", false, "Show what would change without writing")

	migrateCmd.AddCommand(migrateSimulationsCmd)
	migrateSimulationsCmd.Flags().String("target", "", "Target directory (defaults to CWD project root)")
	migrateSimulationsCmd.Flags().Bool("dry-run", false, "Show what would change without writing")

	migrateCmd.AddCommand(migrateTaskAnchorsCmd)
	migrateTaskAnchorsCmd.Flags().String("target", "", "Target directory (defaults to CWD project root)")
	migrateTaskAnchorsCmd.Flags().Bool("dry-run", false, "Show what would change without writing")

	migrateCmd.AddCommand(migrateModifyScopeCmd)
	migrateModifyScopeCmd.Flags().String("target", "", "Target directory (defaults to CWD project root)")
	migrateModifyScopeCmd.Flags().Bool("dry-run", false, "Show what would change without writing")

	migrateCmd.AddCommand(migrateDeviationDriverCmd)
	migrateDeviationDriverCmd.Flags().String("target", "", "Target directory (defaults to CWD project root)")
	migrateDeviationDriverCmd.Flags().Bool("dry-run", false, "Show what would change without writing")

	migrateCmd.AddCommand(migrateSimDeviationsCmd)
	migrateSimDeviationsCmd.Flags().String("target", "", "Target directory (defaults to CWD project root)")
	migrateSimDeviationsCmd.Flags().Bool("dry-run", false, "Show what would change without writing")

	migrateCmd.AddCommand(migrateTestScenariosCmd)
	migrateTestScenariosCmd.Flags().String("target", "", "Target directory (defaults to CWD project root)")
	migrateTestScenariosCmd.Flags().Bool("dry-run", false, "Show what would change without writing")

	migrateCmd.AddCommand(migrateSettingsCmd)
	migrateSettingsCmd.Flags().String("target", "", "Target directory (defaults to CWD project root)")
	migrateSettingsCmd.Flags().Bool("dry-run", false, "Show what would change without writing")

	migrateCmd.AddCommand(migrateAllCmd)
	migrateAllCmd.Flags().String("target", "", "Target directory (defaults to CWD project root)")
	migrateAllCmd.Flags().Bool("dry-run", false, "Show what would change without writing")

	rootCmd.AddCommand(migrateCmd)
}

var migrateCmd = &cobra.Command{
	Use:     "migrate",
	Short:   "Migrate jig artifacts to current schema",
	GroupID: "tools",
}

var migrateVerifyLogCmd = &cobra.Command{
	Use:   "verify-log",
	Short: "Rewrite verify-log.jsonl entries to carry split-minor fields",
	Long: `Legacy verify-log entries have a single "minor" field. Phase 2 of the
jig schema split minors into [resolvable] (blocks completion) and
[deferred] (runtime watch-items). This command re-emits each entry with
minor_resolvable = minor (conservative), minor_deferred = 0.

Entries already carrying minor_resolvable or minor_deferred are left
unchanged. Backup files are written alongside as *.jsonl.bak.`,
	RunE: runMigrateVerifyLog,
}

func runMigrateVerifyLog(cmd *cobra.Command, args []string) error {
	targetFlag, _ := cmd.Flags().GetString("target")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	target := targetFlag
	if target == "" {
		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a jig project: %w", err)
		}
		target = root
	}

	recipesDir := filepath.Join(state.SpecsPath(target), "recipes")
	entries, err := os.ReadDir(recipesDir)
	if err != nil {
		return fmt.Errorf("read recipes dir: %w", err)
	}

	var totalRecipes, migratedRecipes, touchedEntries int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		logPath := filepath.Join(recipesDir, e.Name(), "verify-log.jsonl")
		if _, err := os.Stat(logPath); os.IsNotExist(err) {
			continue
		}
		totalRecipes++

		changed, touched, err := migrateOneVerifyLog(logPath, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s: error: %v\n", e.Name(), err)
			continue
		}
		if changed {
			migratedRecipes++
			touchedEntries += touched
			marker := "migrated"
			if dryRun {
				marker = "would migrate"
			}
			fmt.Printf("  %s: %s %d entr(y|ies)\n", e.Name(), marker, touched)
		}
	}

	if dryRun {
		fmt.Printf("\nDry run: %d/%d recipes need migration, %d entries affected.\n",
			migratedRecipes, totalRecipes, touchedEntries)
	} else {
		fmt.Printf("\nMigrated %d/%d recipes (%d entries). Backups: *.jsonl.bak\n",
			migratedRecipes, totalRecipes, touchedEntries)
	}
	return nil
}

// migrateOneVerifyLog rewrites entries that still use the legacy Minor-only
// layout. Entries already carrying minor_resolvable or minor_deferred keys
// are preserved byte-for-byte (to avoid reformatting timestamps, extra
// fields, etc.). Returns whether any entry was changed and the count.
func migrateOneVerifyLog(path string, dryRun bool) (bool, int, error) {
	in, err := os.Open(path)
	if err != nil {
		return false, 0, err
	}
	defer in.Close()

	var lines []string
	touched := 0
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			lines = append(lines, line)
			continue
		}
		newLine, changed, err := upgradeVerifyLogLine(line)
		if err != nil {
			return false, 0, fmt.Errorf("parse line: %w", err)
		}
		if changed {
			touched++
		}
		lines = append(lines, newLine)
	}
	if err := scanner.Err(); err != nil {
		return false, 0, err
	}
	if touched == 0 {
		return false, 0, nil
	}

	if dryRun {
		return true, touched, nil
	}

	// Backup before writing
	bak := path + ".bak"
	if err := copyFile(path, bak); err != nil {
		return false, 0, fmt.Errorf("backup: %w", err)
	}

	out, err := os.Create(path)
	if err != nil {
		return false, 0, err
	}
	defer out.Close()
	w := bufio.NewWriter(out)
	for _, line := range lines {
		if _, err := w.WriteString(line + "\n"); err != nil {
			return false, 0, err
		}
	}
	return true, touched, w.Flush()
}

func upgradeVerifyLogLine(line string) (string, bool, error) {
	// Peek for the split keys first. Raw string check avoids re-marshaling
	// unrelated fields (which would reorder keys and churn the file).
	if strings.Contains(line, `"minor_resolvable"`) || strings.Contains(line, `"minor_deferred"`) {
		return line, false, nil
	}
	var entry state.VerifyLogEntry
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		// Unknown format — leave the line alone.
		return line, false, nil
	}
	if entry.Minor == 0 {
		// Nothing to split. Still emit split keys so downstream validators
		// (which now expect the new format) see a compliant entry.
		entry.MinorResolvable = 0
		entry.MinorDeferred = 0
	} else {
		entry.MinorResolvable = entry.Minor
		entry.MinorDeferred = 0
	}
	// Converge status: strict interpretation aligned with the new CLI handler.
	if entry.Critical == 0 && entry.Major == 0 && entry.MinorResolvable == 0 {
		if entry.Status != "failed" {
			entry.Status = "converged"
		}
	} else if entry.Status == "" {
		entry.Status = "continue"
	}
	data, err := json.Marshal(&entry)
	if err != nil {
		return "", false, err
	}
	return string(data), true, nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

// ---- changelog verify-result migration --------------------------------

var migrateChangelogCmd = &cobra.Command{
	Use:   "changelog",
	Short: "Upgrade verify entries in changelog.jsonl to the split-minor format",
	Long: `Each verify action in changelog.jsonl carries a "result" string:
  critical=X major=Y minor=Z → status

This command rewrites it to:
  critical=X major=Y minor_resolvable=Z minor_deferred=0 → status

(Legacy minor is conservatively treated as resolvable; operators can
re-classify specific entries by hand afterward.)`,
	RunE: runMigrateChangelog,
}

func runMigrateChangelog(cmd *cobra.Command, args []string) error {
	target, dryRun, err := migrateFlags(cmd)
	if err != nil {
		return err
	}

	recipesDir := filepath.Join(state.SpecsPath(target), "recipes")
	entries, err := os.ReadDir(recipesDir)
	if err != nil {
		return fmt.Errorf("read recipes dir: %w", err)
	}

	var totalRecipes, migratedRecipes, touchedEntries int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		logPath := filepath.Join(recipesDir, e.Name(), "changelog.jsonl")
		if _, err := os.Stat(logPath); os.IsNotExist(err) {
			continue
		}
		totalRecipes++
		changed, touched, err := migrateOneChangelog(logPath, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s: error: %v\n", e.Name(), err)
			continue
		}
		if changed {
			migratedRecipes++
			touchedEntries += touched
			marker := "migrated"
			if dryRun {
				marker = "would migrate"
			}
			fmt.Printf("  %s: %s %d entr(y|ies)\n", e.Name(), marker, touched)
		}
	}

	if dryRun {
		fmt.Printf("\nDry run: %d/%d changelogs need migration, %d entries affected.\n",
			migratedRecipes, totalRecipes, touchedEntries)
	} else {
		fmt.Printf("\nMigrated %d/%d changelogs (%d entries). Backups: *.jsonl.bak\n",
			migratedRecipes, totalRecipes, touchedEntries)
	}
	return nil
}

func migrateOneChangelog(path string, dryRun bool) (bool, int, error) {
	in, err := os.Open(path)
	if err != nil {
		return false, 0, err
	}
	defer in.Close()

	var lines []string
	touched := 0
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			lines = append(lines, line)
			continue
		}
		newLine, changed := upgradeChangelogLine(line)
		if changed {
			touched++
		}
		lines = append(lines, newLine)
	}
	if err := scanner.Err(); err != nil {
		return false, 0, err
	}
	if touched == 0 {
		return false, 0, nil
	}
	if dryRun {
		return true, touched, nil
	}
	bak := path + ".bak"
	if err := copyFile(path, bak); err != nil {
		return false, 0, fmt.Errorf("backup: %w", err)
	}
	out, err := os.Create(path)
	if err != nil {
		return false, 0, err
	}
	defer out.Close()
	w := bufio.NewWriter(out)
	for _, line := range lines {
		if _, err := w.WriteString(line + "\n"); err != nil {
			return false, 0, err
		}
	}
	return true, touched, w.Flush()
}

// upgradeChangelogLine targets verify action entries whose "result" field
// is in the pre-split form "critical=X major=Y minor=Z → status". Everything
// else (including already-migrated verify entries) passes through.
func upgradeChangelogLine(line string) (string, bool) {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return line, false
	}
	if raw["action"] != "verify" {
		return line, false
	}
	result, ok := raw["result"].(string)
	if !ok || result == "" {
		return line, false
	}
	// Already split — skip.
	if strings.Contains(result, "minor_resolvable=") || strings.Contains(result, "minor_deferred=") {
		return line, false
	}
	// Must look like the legacy format.
	if !strings.Contains(result, "minor=") {
		return line, false
	}
	// Parse "critical=X major=Y minor=Z → status"
	parts := strings.SplitN(result, "→", 2)
	head := strings.TrimSpace(parts[0])
	tail := ""
	if len(parts) == 2 {
		tail = strings.TrimSpace(parts[1])
	}

	tokens := strings.Fields(head)
	var critical, major, minor int
	for _, tok := range tokens {
		kv := strings.SplitN(tok, "=", 2)
		if len(kv) != 2 {
			continue
		}
		n, err := strconv.Atoi(kv[1])
		if err != nil {
			continue
		}
		switch kv[0] {
		case "critical":
			critical = n
		case "major":
			major = n
		case "minor":
			minor = n
		}
	}
	newResult := fmt.Sprintf("critical=%d major=%d minor_resolvable=%d minor_deferred=0", critical, major, minor)
	if tail != "" {
		newResult += " → " + tail
	}
	raw["result"] = newResult
	data, err := json.Marshal(raw)
	if err != nil {
		return line, false
	}
	return string(data), true
}

// ---- verification.md migration ----------------------------------------

var migrateVerificationCmd = &cobra.Command{
	Use:   "verification",
	Short: "Inject <jig-findings> block into legacy verification.md files",
	Long: `Reads each recipe's latest verify-log entry, synthesizes a
<jig-findings> JSON block from the counts, and prepends it to
verification.md if missing. Existing blocks are left untouched.

This is a one-time migration. New /jig-verify runs emit the block
natively (per jig-verify/SKILL.md Phase 2.2).`,
	RunE: runMigrateVerification,
}

func runMigrateVerification(cmd *cobra.Command, args []string) error {
	target, dryRun, err := migrateFlags(cmd)
	if err != nil {
		return err
	}

	recipesDir := filepath.Join(state.SpecsPath(target), "recipes")
	entries, err := os.ReadDir(recipesDir)
	if err != nil {
		return fmt.Errorf("read recipes dir: %w", err)
	}

	var migrated, skipped int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		recipeDir := filepath.Join(recipesDir, e.Name())
		vmd := filepath.Join(recipeDir, "verification.md")
		if _, err := os.Stat(vmd); os.IsNotExist(err) {
			continue
		}
		ok, err := injectFindingsBlock(recipeDir, vmd, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s: error: %v\n", e.Name(), err)
			continue
		}
		if ok {
			migrated++
			marker := "injected"
			if dryRun {
				marker = "would inject"
			}
			fmt.Printf("  %s: %s <jig-findings> block\n", e.Name(), marker)
		} else {
			skipped++
		}
	}
	if dryRun {
		fmt.Printf("\nDry run: %d recipes would get blocks, %d already have them or lack verify-log.\n", migrated, skipped)
	} else {
		fmt.Printf("\nInjected blocks into %d recipes. Backups: verification.md.bak\n", migrated)
	}
	return nil
}

func injectFindingsBlock(recipeDir, vmdPath string, dryRun bool) (bool, error) {
	data, err := os.ReadFile(vmdPath)
	if err != nil {
		return false, err
	}
	if strings.Contains(string(data), "<jig-findings>") {
		return false, nil // already present
	}

	logPath := filepath.Join(recipeDir, "verify-log.jsonl")
	last, err := readLastVerifyLogEntry(logPath)
	if err != nil {
		return false, nil // no log — nothing to synthesize from
	}

	// Legacy entries carry a single Minor. Conservative mapping: resolvable.
	resolvable := last.MinorResolvable
	deferred := last.MinorDeferred
	if resolvable == 0 && deferred == 0 && last.Minor > 0 {
		resolvable = last.Minor
	}

	block := fmt.Sprintf(`<jig-findings>
{
  "critical": %d,
  "major": %d,
  "minor_resolvable": %d,
  "minor_deferred": %d,
  "info": %d,
  "source": "migrated-from-verify-log"
}
</jig-findings>

`, last.Critical, last.Major, resolvable, deferred, last.Info)

	if dryRun {
		return true, nil
	}
	bak := vmdPath + ".bak"
	if err := os.WriteFile(bak, data, 0644); err != nil {
		return false, fmt.Errorf("backup: %w", err)
	}
	newContent := block + string(data)
	return true, os.WriteFile(vmdPath, []byte(newContent), 0644)
}

func readLastVerifyLogEntry(path string) (*state.VerifyLogEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var last state.VerifyLogEntry
	found := false
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
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
		return nil, fmt.Errorf("empty log")
	}
	return &last, nil
}

// ---- simulations tagging migration ------------------------------------

var migrateSimulationsCmd = &cobra.Command{
	Use:   "simulations",
	Short: "Tag legacy simulation scenarios with [single-axis: legacy]",
	Long: `Scenario headers in pre-Phase-6 simulations carry no cross-boundary
classification. This command adds [single-axis: legacy] to every header
that lacks one of [cross-boundary|single-axis|illegal-cell: ...].

Legacy is a conservative default — it keeps the validator happy without
claiming cross-boundary coverage the author never verified. When the
recipe is re-simulated later, authors re-tag by hand; the checker's
cross-boundary ratio only applies to files whose scenarios are
currently authored.`,
	RunE: runMigrateSimulations,
}

func runMigrateSimulations(cmd *cobra.Command, args []string) error {
	target, dryRun, err := migrateFlags(cmd)
	if err != nil {
		return err
	}
	recipesDir := filepath.Join(state.SpecsPath(target), "recipes")
	entries, err := os.ReadDir(recipesDir)
	if err != nil {
		return fmt.Errorf("read recipes dir: %w", err)
	}

	totalTouched := 0
	totalFiles := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		simsDir := filepath.Join(recipesDir, e.Name(), "simulations")
		sims, err := os.ReadDir(simsDir)
		if err != nil {
			continue
		}
		for _, sim := range sims {
			if sim.IsDir() || !strings.HasSuffix(sim.Name(), ".md") {
				continue
			}
			simPath := filepath.Join(simsDir, sim.Name())
			touched, changed, err := tagLegacyScenarios(simPath, dryRun)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  %s/%s: error: %v\n", e.Name(), sim.Name(), err)
				continue
			}
			if changed {
				totalFiles++
				totalTouched += touched
				marker := "tagged"
				if dryRun {
					marker = "would tag"
				}
				fmt.Printf("  %s/%s: %s %d header(s)\n", e.Name(), sim.Name(), marker, touched)
			}
		}
	}

	if dryRun {
		fmt.Printf("\nDry run: %d files would get tags added (%d headers).\n", totalFiles, totalTouched)
	} else {
		fmt.Printf("\nTagged %d files (%d headers). Backups: *.md.bak\n", totalFiles, totalTouched)
	}
	return nil
}

// existingTagRe detects whether a scenario line already carries one of
// the three canonical tag shapes — used to avoid double-tagging on
// idempotent re-runs of `jig migrate simulations`.
var existingTagRe = regexp.MustCompile(`(?i)\[(cross-boundary|single-axis|illegal-cell)(?::[^\]]*)?\]`)

// tagLegacyScenarios walks a simulation file and injects
// `[single-axis: legacy]` onto any scenario line that lacks a tag.
// Scenario recognition delegates to engine.IsSimulationScenarioLine
// (single source of truth shared with simulation_checker.go), so the
// set of lines this function touches matches exactly the set the
// checker counts.
//
// Table rows receive the tag inside the last cell so markdown
// structure stays valid (`| S01 | foo | bar [single-axis: legacy] |`).
// Heading lines receive it appended at the end.
func tagLegacyScenarios(path string, dryRun bool) (int, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false, err
	}

	lines := strings.Split(string(data), "\n")
	touched := 0
	for i, line := range lines {
		if !engine.IsSimulationScenarioLine(line) {
			continue
		}
		if existingTagRe.MatchString(line) {
			continue // already tagged — leave it alone
		}
		lines[i] = appendLegacyTag(line)
		touched++
	}

	if touched == 0 {
		return 0, false, nil
	}
	if dryRun {
		return touched, true, nil
	}
	if err := os.WriteFile(path+".bak", data, 0644); err != nil {
		return 0, false, fmt.Errorf("backup: %w", err)
	}
	return touched, true, os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}

// appendLegacyTag inserts the legacy tag into a scenario line. The
// placement depends on whether the line is a markdown table row or a
// heading:
//
//   - Table row (`| S01 | foo |`)  → `| S01 | foo [single-axis: legacy] |`
//   - Heading  (`### S01 — Happy`) → `### S01 — Happy [single-axis: legacy]`
//
// Keeping the tag inside the table row's final cell preserves pipe
// balance so downstream markdown renderers still parse the row cleanly.
func appendLegacyTag(line string) string {
	const tag = "[single-axis: legacy]"
	trimmedTrailing := strings.TrimRight(line, " \t")
	leftTrim := strings.TrimLeft(trimmedTrailing, " \t")
	if strings.HasPrefix(leftTrim, "|") && strings.HasSuffix(trimmedTrailing, "|") {
		// Inject inside the last cell, before the closing `|`.
		return trimmedTrailing[:len(trimmedTrailing)-1] + " " + tag + " |"
	}
	return trimmedTrailing + " " + tag
}

// ---- task-anchor migration (Phase 9) ---------------------------------

var migrateTaskAnchorsCmd = &cobra.Command{
	Use:   "task-anchors",
	Short: "Inject <!-- task-anchor: path action --> into final.md and populate Task.anchor",
	Long: `Phase 9 makes tasks.json ↔ final.md a machine-checked 1:1 contract.
Legacy recipes have neither the anchor comment in final.md nor the
Task.anchor field in tasks.json. This command walks each recipe, looks
up each Task's (file, action), inserts the anchor above the spec block
most likely describing that file (heuristics: " ` + "`" + `{file}` + "`" + ` "
mention, " ### ` + "`" + `{file}` + "`" + ` " heading, " ## {file} " heading,
first code block citing the path), and backfills Task.anchor.

Ambiguous files (no match in final.md or multiple plausible locations)
are reported; the anchor is inserted at the top of the Tasks/Components
section as a fallback. Manual review is recommended afterward.

Backups: final.md.bak + tasks.json.bak.`,
	RunE: runMigrateTaskAnchors,
}

func runMigrateTaskAnchors(cmd *cobra.Command, args []string) error {
	target, dryRun, err := migrateFlags(cmd)
	if err != nil {
		return err
	}
	recipesDir := filepath.Join(state.SpecsPath(target), "recipes")
	entries, err := os.ReadDir(recipesDir)
	if err != nil {
		return fmt.Errorf("read recipes dir: %w", err)
	}

	var totalRecipes, migratedRecipes, insertedAnchors, backfilledTasks int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		recipeDir := filepath.Join(recipesDir, e.Name())
		finalPath := filepath.Join(recipeDir, "final.md")
		tasksPath := filepath.Join(recipeDir, "tasks.json")
		if _, err := os.Stat(finalPath); os.IsNotExist(err) {
			continue
		}
		if _, err := os.Stat(tasksPath); os.IsNotExist(err) {
			continue
		}
		totalRecipes++

		inserted, backfilled, err := migrateOneTaskAnchors(finalPath, tasksPath, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s: error: %v\n", e.Name(), err)
			continue
		}
		if inserted > 0 || backfilled > 0 {
			migratedRecipes++
			insertedAnchors += inserted
			backfilledTasks += backfilled
			marker := "migrated"
			if dryRun {
				marker = "would migrate"
			}
			fmt.Printf("  %s: %s (%d anchors inserted, %d Task.anchor backfilled)\n", e.Name(), marker, inserted, backfilled)
		}
	}
	if dryRun {
		fmt.Printf("\nDry run: %d/%d recipes need migration (%d anchors, %d task fields).\n", migratedRecipes, totalRecipes, insertedAnchors, backfilledTasks)
	} else {
		fmt.Printf("\nMigrated %d/%d recipes (%d anchors, %d task fields). Backups: final.md.bak + tasks.json.bak\n", migratedRecipes, totalRecipes, insertedAnchors, backfilledTasks)
	}
	return nil
}

// taskAnchorJSON is the minimal struct we deserialize tasks.json into
// for migration purposes. Keeps field order stable on rewrite so diffs
// stay reviewable.
type taskAnchorJSON struct {
	RecipeID  string                   `json:"recipe_id,omitempty"`
	StartedAt string                   `json:"started_at,omitempty"`
	UpdatedAt string                   `json:"updated_at,omitempty"`
	Tasks     []map[string]interface{} `json:"tasks"`
}

func migrateOneTaskAnchors(finalPath, tasksPath string, dryRun bool) (int, int, error) {
	finalData, err := os.ReadFile(finalPath)
	if err != nil {
		return 0, 0, err
	}
	tasksData, err := os.ReadFile(tasksPath)
	if err != nil {
		return 0, 0, err
	}

	var tasks taskAnchorJSON
	if err := json.Unmarshal(tasksData, &tasks); err != nil {
		return 0, 0, fmt.Errorf("parse tasks.json: %w", err)
	}

	finalStr := string(finalData)
	insertedAnchors := 0
	backfilledTasks := 0

	for i, task := range tasks.Tasks {
		file, _ := task["file"].(string)
		action, _ := task["action"].(string)
		if file == "" || action == "" {
			continue
		}
		anchorKey := file + " " + action
		existing, _ := task["anchor"].(string)

		// Backfill Task.anchor if missing.
		if existing == "" {
			tasks.Tasks[i]["anchor"] = anchorKey
			backfilledTasks++
		}

		// Insert anchor into final.md if not present.
		anchorComment := "<!-- task-anchor: " + anchorKey + " -->"
		if strings.Contains(finalStr, anchorComment) {
			continue
		}
		insertion, ok := placeTaskAnchor(finalStr, file, anchorComment)
		if !ok {
			// Fallback: append at top of file below first H1 or at line 0.
			insertion = fallbackInsertAnchor(finalStr, anchorComment)
		}
		finalStr = insertion
		insertedAnchors++
	}

	if insertedAnchors == 0 && backfilledTasks == 0 {
		return 0, 0, nil
	}
	if dryRun {
		return insertedAnchors, backfilledTasks, nil
	}

	// Write backups before overwriting.
	if err := os.WriteFile(finalPath+".bak", finalData, 0644); err != nil {
		return 0, 0, err
	}
	if err := os.WriteFile(tasksPath+".bak", tasksData, 0644); err != nil {
		return 0, 0, err
	}

	if err := os.WriteFile(finalPath, []byte(finalStr), 0644); err != nil {
		return 0, 0, err
	}
	out, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return 0, 0, err
	}
	// Preserve trailing newline if input had one.
	if strings.HasSuffix(string(tasksData), "\n") {
		out = append(out, '\n')
	}
	if err := os.WriteFile(tasksPath, out, 0644); err != nil {
		return 0, 0, err
	}
	return insertedAnchors, backfilledTasks, nil
}

// placeTaskAnchor tries to insert the anchor immediately above a
// plausible heading describing the file. Heuristics, in order:
//  1. "### `{file}`" — canonical Level-3-draft heading
//  2. "## {file}"    — alternate heading style
//  3. "### {basename}" — heading by file basename only
//  4. first line mentioning the file path inside a code span
//
// On success returns the updated final.md content and true.
func placeTaskAnchor(finalStr, file, anchorComment string) (string, bool) {
	base := filepath.Base(file)
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?m)^###\s+` + "`" + regexp.QuoteMeta(file) + "`" + `.*$`),
		regexp.MustCompile(`(?m)^##\s+` + regexp.QuoteMeta(file) + `.*$`),
		regexp.MustCompile(`(?m)^###\s+` + "`" + regexp.QuoteMeta(base) + "`" + `.*$`),
		regexp.MustCompile(`(?m)^###\s+` + regexp.QuoteMeta(base) + `.*$`),
	}
	for _, re := range patterns {
		if loc := re.FindStringIndex(finalStr); loc != nil {
			return finalStr[:loc[0]] + anchorComment + "\n" + finalStr[loc[0]:], true
		}
	}
	return finalStr, false
}

// fallbackInsertAnchor places the anchor after the first H1 heading or,
// lacking one, at the very start of final.md. Keeps the document
// parseable either way.
func fallbackInsertAnchor(finalStr, anchorComment string) string {
	h1 := regexp.MustCompile(`(?m)^#\s+.+$`).FindStringIndex(finalStr)
	if h1 == nil {
		return anchorComment + "\n" + finalStr
	}
	// insert after the heading line
	end := h1[1]
	if end < len(finalStr) && finalStr[end] == '\n' {
		end++
	}
	return finalStr[:end] + "\n" + anchorComment + "\n" + finalStr[end:]
}

// ---- modify-scope migration (Phase 14) -------------------------------

var migrateModifyScopeCmd = &cobra.Command{
	Use:   "modify-scope",
	Short: "Declare modify_scope + anchor scope= for legacy modify tasks",
	Long: `Phase 14 requires every action=modify task to carry both a
Task.modify_scope list and a matching scope= suffix in its final.md
anchor. This command walks recipes and infers a scope from the task's
Description using a heuristic: scan the target source file for
exported/top-level symbols (functions, classes, methods, const decls);
keep the ones whose names appear in the task description; set that
list as both modify_scope and the anchor's scope= suffix.

When the heuristic yields zero symbols, the task is skipped and
reported so the user can fill the list manually.

Backups: final.md.bak.scope + tasks.json.bak.scope.`,
	RunE: runMigrateModifyScope,
}

var (
	goTopLevelSymbolRe = regexp.MustCompile(`(?m)^\s*(?:func|type)\s+(?:\([^)]+\)\s*)?([A-Za-z_]\w*)`)
	tsTopLevelSymbolRe = regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:async\s+)?(?:function|class|const|let|var|type|interface)\s+([A-Za-z_]\w*)`)
	swiftTopLevelRe    = regexp.MustCompile(`(?m)^\s*(?:public\s+|private\s+|internal\s+|fileprivate\s+)?(?:func|class|struct|enum|var|let)\s+([A-Za-z_]\w*)`)
	pyTopLevelRe       = regexp.MustCompile(`(?m)^\s*(?:def|class)\s+([A-Za-z_]\w*)`)
)

func runMigrateModifyScope(cmd *cobra.Command, args []string) error {
	target, dryRun, err := migrateFlags(cmd)
	if err != nil {
		return err
	}
	recipesDir := filepath.Join(state.SpecsPath(target), "recipes")
	entries, err := os.ReadDir(recipesDir)
	if err != nil {
		return fmt.Errorf("read recipes dir: %w", err)
	}

	var totalRecipes, migratedRecipes, filledTasks, skippedTasks int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		recipeDir := filepath.Join(recipesDir, e.Name())
		finalPath := filepath.Join(recipeDir, "final.md")
		tasksPath := filepath.Join(recipeDir, "tasks.json")
		if _, err := os.Stat(tasksPath); os.IsNotExist(err) {
			continue
		}
		if _, err := os.Stat(finalPath); os.IsNotExist(err) {
			continue
		}
		totalRecipes++

		filled, skipped, err := migrateOneModifyScope(target, finalPath, tasksPath, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s: error: %v\n", e.Name(), err)
			continue
		}
		if filled > 0 || skipped > 0 {
			migratedRecipes++
			filledTasks += filled
			skippedTasks += skipped
			marker := "migrated"
			if dryRun {
				marker = "would migrate"
			}
			fmt.Printf("  %s: %s (%d filled, %d skipped-for-manual)\n", e.Name(), marker, filled, skipped)
		}
	}
	if dryRun {
		fmt.Printf("\nDry run: %d/%d recipes need migration (%d tasks filled, %d skipped).\n", migratedRecipes, totalRecipes, filledTasks, skippedTasks)
	} else {
		fmt.Printf("\nMigrated %d/%d recipes (%d filled, %d skipped). Backups: .bak.scope\n", migratedRecipes, totalRecipes, filledTasks, skippedTasks)
	}
	return nil
}

func migrateOneModifyScope(projectRoot, finalPath, tasksPath string, dryRun bool) (int, int, error) {
	finalData, err := os.ReadFile(finalPath)
	if err != nil {
		return 0, 0, err
	}
	tasksData, err := os.ReadFile(tasksPath)
	if err != nil {
		return 0, 0, err
	}

	var tasks taskAnchorJSON
	if err := json.Unmarshal(tasksData, &tasks); err != nil {
		return 0, 0, fmt.Errorf("parse tasks.json: %w", err)
	}

	finalStr := string(finalData)
	filled := 0
	skipped := 0

	for i, task := range tasks.Tasks {
		action, _ := task["action"].(string)
		if action != "modify" {
			continue
		}
		// Skip if already set.
		if scope, ok := task["modify_scope"].([]interface{}); ok && len(scope) > 0 {
			continue
		}
		file, _ := task["file"].(string)
		desc, _ := task["description"].(string)

		symbols := inferScopeSymbols(projectRoot, file, desc)
		if len(symbols) == 0 {
			// Heuristic failed — tag `legacy` as a migration placeholder.
			// CheckModifyScope treats this token as unchecked so validation
			// passes; the user is expected to replace it with real symbols
			// when they next work on the task. Counted as skipped-for-manual
			// so the output makes the gap visible.
			symbols = []string{"legacy"}
			skipped++
		} else {
			filled++
		}

		// Set Task.modify_scope.
		scopeIface := make([]interface{}, len(symbols))
		for j, s := range symbols {
			scopeIface[j] = s
		}
		tasks.Tasks[i]["modify_scope"] = scopeIface

		// Rewrite the anchor in final.md to append `scope=...`.
		oldAnchor := "<!-- task-anchor: " + file + " modify -->"
		newAnchor := "<!-- task-anchor: " + file + " modify scope=" + strings.Join(symbols, ",") + " -->"
		if strings.Contains(finalStr, oldAnchor) {
			finalStr = strings.Replace(finalStr, oldAnchor, newAnchor, 1)
		} else {
			// Anchor already has some suffix — do not clobber; add scope if
			// the existing suffix lacks it.
			re := regexp.MustCompile(`<!--\s*task-anchor:\s*` + regexp.QuoteMeta(file) + `\s+modify\b([^>]*)-->`)
			match := re.FindStringSubmatch(finalStr)
			if len(match) >= 2 && !strings.Contains(match[1], "scope=") {
				replacement := "<!-- task-anchor: " + file + " modify" + match[1] + " scope=" + strings.Join(symbols, ",") + " -->"
				finalStr = re.ReplaceAllString(finalStr, replacement)
			}
		}
	}

	if filled == 0 && skipped == 0 {
		return 0, 0, nil
	}
	if dryRun {
		return filled, skipped, nil
	}
	if err := os.WriteFile(finalPath+".bak.scope", finalData, 0644); err != nil {
		return 0, 0, err
	}
	if err := os.WriteFile(tasksPath+".bak.scope", tasksData, 0644); err != nil {
		return 0, 0, err
	}
	if err := os.WriteFile(finalPath, []byte(finalStr), 0644); err != nil {
		return 0, 0, err
	}
	out, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return 0, 0, err
	}
	if strings.HasSuffix(string(tasksData), "\n") {
		out = append(out, '\n')
	}
	if err := os.WriteFile(tasksPath, out, 0644); err != nil {
		return 0, 0, err
	}
	return filled, skipped, nil
}

// inferScopeSymbols reads the target file and the task description,
// returns the set of top-level symbol names that appear in both.
// Language detected by file extension; unknown extensions fall back
// to the TS pattern (broadest).
func inferScopeSymbols(projectRoot, file, description string) []string {
	path := file
	if !filepath.IsAbs(path) {
		path = filepath.Join(projectRoot, file)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	content := string(data)

	re := tsTopLevelSymbolRe
	switch strings.ToLower(filepath.Ext(file)) {
	case ".go":
		re = goTopLevelSymbolRe
	case ".swift":
		re = swiftTopLevelRe
	case ".py":
		re = pyTopLevelRe
	}

	allSymbols := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(content, -1) {
		if len(m) >= 2 {
			allSymbols[m[1]] = true
		}
	}

	// Retain symbols whose name is referenced in the description.
	descLower := strings.ToLower(description)
	var matched []string
	seen := map[string]bool{}
	for sym := range allSymbols {
		if seen[sym] {
			continue
		}
		// Word-boundary match on case-insensitive description.
		if strings.Contains(descLower, strings.ToLower(sym)) {
			matched = append(matched, sym)
			seen[sym] = true
		}
	}
	sort.Strings(matched)
	return matched
}

// ---- deviation-driver migration (Phase 16) ----------------------------

var migrateDeviationDriverCmd = &cobra.Command{
	Use:   "deviation-driver",
	Short: "Upgrade deviation.md tables to the 7-column Driver schema",
	Long: `Phase 16 requires every row in deviation.md to carry an ID, a
Driver from the vocabulary (code-diff | sync-check | simulate:<id> |
review:<id> | test:<name> | midrun:<window>), and a Severity from
{critical, major, minor, info}. Legacy tables lack these columns.

This command rewrites each recipe's deviation.md so that:
  - Every row receives a unique monotonic ID (D-NNN) preserving
    document order.
  - The Driver column is added with a default value of "code-diff"
    (the conservative choice for rows produced by /jig-sync's own
    diff pass).
  - The Severity column is added with a default of "major".

Rows already matching the new schema are left untouched. Backup:
deviation.md.bak.driver.`,
	RunE: runMigrateDeviationDriver,
}

var (
	// Old "Not Implemented" header. Some recipes added a leading "#"
	// or "No." column — accept either shape.
	oldNotImplHeaderRe   = regexp.MustCompile(`(?mi)^\|\s*(?:#|No\.?)?\s*\|?\s*Item\s*\|\s*File\s*\|\s*Reason\s*\|\s*$`)
	oldSpecAddHeaderRe   = regexp.MustCompile(`(?mi)^\|\s*(?:#|No\.?)?\s*\|?\s*Item\s*\|\s*File\s*\|\s*Description\s*\|\s*$`)
	oldDeviationHeaderRe = regexp.MustCompile(`(?mi)^\|\s*(?:#|No\.?)?\s*\|?\s*Item\s*\|\s*Spec Says\s*\|\s*Code Has\s*\|\s*Resolution\s*\|\s*$`)
)

// hasNumberingColumn reports whether the matched header included a "#"
// or "No." leading column. Used to strip that first cell from data rows
// during rewrite so it does not leak into the new ID column.
func hasNumberingColumn(headerLine string) bool {
	trimmed := strings.TrimSpace(headerLine)
	// Look at the first cell (between the first and second |).
	first := strings.SplitN(strings.TrimPrefix(trimmed, "|"), "|", 2)
	if len(first) == 0 {
		return false
	}
	cell := strings.TrimSpace(first[0])
	return cell == "#" || strings.EqualFold(cell, "No") || strings.EqualFold(cell, "No.")
}

func runMigrateDeviationDriver(cmd *cobra.Command, args []string) error {
	target, dryRun, err := migrateFlags(cmd)
	if err != nil {
		return err
	}
	recipesDir := filepath.Join(state.SpecsPath(target), "recipes")
	entries, err := os.ReadDir(recipesDir)
	if err != nil {
		return fmt.Errorf("read recipes dir: %w", err)
	}

	var totalRecipes, migratedRecipes, rewrittenRows int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		deviationPath := filepath.Join(recipesDir, e.Name(), "deviation.md")
		if _, err := os.Stat(deviationPath); os.IsNotExist(err) {
			continue
		}
		totalRecipes++

		rewritten, err := migrateOneDeviationDriver(deviationPath, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s: error: %v\n", e.Name(), err)
			continue
		}
		if rewritten > 0 {
			migratedRecipes++
			rewrittenRows += rewritten
			marker := "migrated"
			if dryRun {
				marker = "would migrate"
			}
			fmt.Printf("  %s: %s %d row(s)\n", e.Name(), marker, rewritten)
		}
	}
	if dryRun {
		fmt.Printf("\nDry run: %d/%d deviation.md files need migration (%d rows).\n", migratedRecipes, totalRecipes, rewrittenRows)
	} else {
		fmt.Printf("\nMigrated %d/%d deviation.md files (%d rows). Backups: deviation.md.bak.driver\n", migratedRecipes, totalRecipes, rewrittenRows)
	}
	return nil
}

func migrateOneDeviationDriver(path string, dryRun bool) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	content := string(data)

	// Short-circuit if the file already uses the new schema.
	if strings.Contains(content, "| ID | Item ") {
		return 0, nil
	}

	lines := strings.Split(content, "\n")
	idCounter := 0
	nextID := func() string {
		idCounter++
		return fmt.Sprintf("D-%03d", idCounter)
	}

	var out []string
	tableMode := "" // "not_implemented" | "spec_additions" | "deviations"
	expectSeparator := false
	skipLeadingCell := false
	rowsRewritten := 0

	for _, line := range lines {
		switch {
		case oldNotImplHeaderRe.MatchString(line):
			out = append(out, "| ID | Item | File | Driver | Severity | Reason |")
			expectSeparator = true
			tableMode = "not_implemented"
			skipLeadingCell = hasNumberingColumn(line)
			continue
		case oldSpecAddHeaderRe.MatchString(line):
			out = append(out, "| ID | Item | File | Driver | Severity | Description |")
			expectSeparator = true
			tableMode = "spec_additions"
			skipLeadingCell = hasNumberingColumn(line)
			continue
		case oldDeviationHeaderRe.MatchString(line):
			out = append(out, "| ID | Item | Spec Says | Code Has | Driver | Severity | Resolution |")
			expectSeparator = true
			tableMode = "deviations"
			skipLeadingCell = hasNumberingColumn(line)
			continue
		}

		if expectSeparator && strings.Contains(line, "---") {
			// Replace the old separator with one that matches the new
			// column count.
			switch tableMode {
			case "deviations":
				out = append(out, "|----|------|-----------|----------|--------|----------|------------|")
			default:
				out = append(out, "|----|------|------|--------|----------|--------|")
			}
			expectSeparator = false
			continue
		}

		if tableMode != "" {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "|") {
				tableMode = ""
				out = append(out, line)
				continue
			}
			cells := parseTableRow(trimmed)
			for i := range cells {
				cells[i] = strings.TrimSpace(cells[i])
			}
			// If the old header carried a leading "#"/"No." numbering
			// column, drop that first cell so downstream indices line up
			// with the canonical Item-first layout.
			if skipLeadingCell && len(cells) > 0 {
				cells = cells[1:]
			}
			// Placeholder row → preserve but append empty cells to match width.
			if isPlaceholderCells(cells) {
				switch tableMode {
				case "deviations":
					out = append(out, "| — | — | — | — | — | — | — |")
				default:
					out = append(out, "| — | — | — | — | — | — |")
				}
				continue
			}
			id := nextID()
			var rewritten string
			switch tableMode {
			case "not_implemented", "spec_additions":
				// old: Item | File | Reason(or Description)
				item := firstOr(cells, 0, "")
				file := firstOr(cells, 1, "")
				tail := firstOr(cells, 2, "")
				rewritten = fmt.Sprintf("| %s | %s | %s | code-diff | major | %s |",
					id, item, file, tail)
			case "deviations":
				// old: Item | Spec Says | Code Has | Resolution
				item := firstOr(cells, 0, "")
				spec := firstOr(cells, 1, "")
				code := firstOr(cells, 2, "")
				res := firstOr(cells, 3, "")
				rewritten = fmt.Sprintf("| %s | %s | %s | %s | code-diff | major | %s |",
					id, item, spec, code, res)
			}
			out = append(out, rewritten)
			rowsRewritten++
			continue
		}
		out = append(out, line)
	}

	if rowsRewritten == 0 {
		return 0, nil
	}
	if dryRun {
		return rowsRewritten, nil
	}
	if err := os.WriteFile(path+".bak.driver", data, 0644); err != nil {
		return 0, err
	}
	return rowsRewritten, os.WriteFile(path, []byte(strings.Join(out, "\n")), 0644)
}

func firstOr(xs []string, i int, def string) string {
	if i < len(xs) {
		return xs[i]
	}
	return def
}

func isPlaceholderCells(cells []string) bool {
	for _, c := range cells {
		if c != "" && c != "—" && c != "-" {
			return false
		}
	}
	return true
}

// parseTableRow is a thin wrapper so migrate doesn't need to import
// engine (which would create a cycle). Duplicates the logic in
// engine/domain_checker.go — kept short enough to inline.
func parseTableRow(line string) []string {
	line = strings.TrimSpace(line)
	parts := strings.Split(line, "|")
	if len(parts) > 0 && strings.TrimSpace(parts[0]) == "" && strings.HasPrefix(line, "|") {
		parts = parts[1:]
	}
	if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" && strings.HasSuffix(line, "|") {
		parts = parts[:len(parts)-1]
	}
	return parts
}

// ---- sim-deviations migration (Phase 12) ------------------------------

var migrateSimDeviationsCmd = &cobra.Command{
	Use:   "sim-deviations",
	Short: "Append legacy simulation DEVIATIONs to deviation.md (Phase 12)",
	Long: `Phase 12 requires every simulation DEVIATION to land in
deviation.md with a matching simulate:{id} Driver. Legacy recipes have
DEVIATIONs listed in simulations/*.md (bullet or bare-line form) but
never synced into deviation.md. This command appends a row for each
unconsumed DEVIATION with:

  - ID:         next available D-NNN
  - Item:       first sentence of the detail
  - Spec Says:  "(see simulation {file})"
  - Code Has:   "(see simulation {file})"
  - Driver:     simulate:{sim-id}
  - Severity:   major (legacy default) unless grammar declared otherwise
  - Resolution: pending

Existing rows whose Driver already cites simulate:{id} are preserved
untouched. Backup: deviation.md.bak.sim.`,
	RunE: runMigrateSimDeviations,
}

func runMigrateSimDeviations(cmd *cobra.Command, args []string) error {
	target, dryRun, err := migrateFlags(cmd)
	if err != nil {
		return err
	}
	recipesDir := filepath.Join(state.SpecsPath(target), "recipes")
	entries, err := os.ReadDir(recipesDir)
	if err != nil {
		return fmt.Errorf("read recipes dir: %w", err)
	}

	var totalRecipes, migratedRecipes, appendedRows int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		recipeDir := filepath.Join(recipesDir, e.Name())
		deviationPath := filepath.Join(recipeDir, "deviation.md")
		if _, err := os.Stat(deviationPath); os.IsNotExist(err) {
			continue
		}
		simsDir := filepath.Join(recipeDir, "simulations")
		if _, err := os.Stat(simsDir); os.IsNotExist(err) {
			continue
		}
		totalRecipes++

		appended, err := migrateOneSimDeviations(recipeDir, deviationPath, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s: error: %v\n", e.Name(), err)
			continue
		}
		if appended > 0 {
			migratedRecipes++
			appendedRows += appended
			marker := "appended"
			if dryRun {
				marker = "would append"
			}
			fmt.Printf("  %s: %s %d sim-DEVIATION row(s)\n", e.Name(), marker, appended)
		}
	}

	if dryRun {
		fmt.Printf("\nDry run: %d/%d recipes need sim-DEVIATION migration (%d rows).\n", migratedRecipes, totalRecipes, appendedRows)
	} else {
		fmt.Printf("\nMigrated %d/%d recipes (%d rows). Backups: deviation.md.bak.sim\n", migratedRecipes, totalRecipes, appendedRows)
	}
	return nil
}

func migrateOneSimDeviations(recipeDir, deviationPath string, dryRun bool) (int, error) {
	// Use the engine to get the same list the gate checks against.
	sims, err := engineExtractSims(recipeDir)
	if err != nil || len(sims) == 0 {
		return 0, err
	}

	// Find which sim ids are already covered in deviation.md so we
	// skip them — idempotent re-run should be a no-op.
	data, err := os.ReadFile(deviationPath)
	if err != nil {
		return 0, err
	}
	content := string(data)

	var unconsumed []simMigrateEntry
	for _, s := range sims {
		needle := "simulate:" + s.ID
		if strings.Contains(content, needle) {
			continue
		}
		unconsumed = append(unconsumed, s)
	}
	if len(unconsumed) == 0 {
		return 0, nil
	}

	// Compute the next D-NNN id. Scan existing rows for the highest.
	maxID := 0
	idRe := regexp.MustCompile(`\|\s*D-(\d+)\s*\|`)
	for _, m := range idRe.FindAllStringSubmatch(content, -1) {
		if len(m) >= 2 {
			n, _ := strconv.Atoi(m[1])
			if n > maxID {
				maxID = n
			}
		}
	}

	// Build the appended rows. If a "## Deviations" section exists, we
	// inject just after its table; otherwise append a new section.
	var appended strings.Builder
	for _, s := range unconsumed {
		maxID++
		id := fmt.Sprintf("D-%03d", maxID)
		item := firstSentence(s.Detail)
		fmt.Fprintf(&appended, "| %s | %s | (see simulations/%s) | (see simulations/%s) | simulate:%s | %s | pending |\n",
			id, item, s.File, s.File, s.ID, s.Severity)
	}

	newContent := appendToDeviationSection(content, appended.String())

	if dryRun {
		return len(unconsumed), nil
	}
	if err := os.WriteFile(deviationPath+".bak.sim", data, 0644); err != nil {
		return 0, err
	}
	if err := os.WriteFile(deviationPath, []byte(newContent), 0644); err != nil {
		return 0, err
	}
	return len(unconsumed), nil
}

// simMigrateEntry mirrors engine.SimDeviation without pulling the
// engine package into this file's signature surface. migrate.go
// already imports engine indirectly via cli; a thin aliasing call
// keeps the two layers loosely coupled.
type simMigrateEntry struct {
	ID, File, Driver, Severity, Detail string
}

// engineExtractSims forwards to the engine extractor. Kept in a tiny
// wrapper so testing the migrator can stub this out if needed later.
func engineExtractSims(recipeDir string) ([]simMigrateEntry, error) {
	raws, err := engine.ExtractSimulationDeviations(recipeDir)
	if err != nil {
		return nil, err
	}
	out := make([]simMigrateEntry, 0, len(raws))
	for _, r := range raws {
		out = append(out, simMigrateEntry{
			ID: r.ID, File: r.File, Driver: r.Driver,
			Severity: r.Severity, Detail: r.Detail,
		})
	}
	return out, nil
}

// firstSentence returns the first ". " or "\n" delimited chunk of s,
// capped at 100 characters. Used when synthesizing the Item column.
func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.Index(s, ". "); idx > 0 && idx < 100 {
		return s[:idx]
	}
	if idx := strings.IndexByte(s, '\n'); idx > 0 && idx < 100 {
		return strings.TrimSpace(s[:idx])
	}
	if len(s) > 100 {
		return s[:97] + "..."
	}
	return s
}

// appendToDeviationSection inserts rows into the Deviations table.
// If no Deviations section exists, a fresh one is appended at EOF
// along with the canonical 7-column header.
func appendToDeviationSection(content, rows string) string {
	const header = "## Deviations"
	idx := strings.Index(content, header)
	if idx < 0 {
		// No section → append new one.
		return strings.TrimRight(content, "\n") + "\n\n" +
			"## Deviations\n" +
			"| ID | Item | Spec Says | Code Has | Driver | Severity | Resolution |\n" +
			"|----|------|-----------|----------|--------|----------|------------|\n" +
			rows
	}
	// Find the end of the existing table by walking until a line that
	// is NOT part of the table (doesn't start with "|").
	tail := content[idx:]
	lines := strings.Split(tail, "\n")
	cut := len(lines)
	tableStarted := false
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "|") {
			tableStarted = true
			continue
		}
		if tableStarted && trimmed == "" {
			cut = i
			break
		}
		if tableStarted && !strings.HasPrefix(trimmed, "|") {
			cut = i
			break
		}
	}
	before := idx + len(strings.Join(lines[:cut], "\n"))
	after := content[before:]
	return content[:before] + "\n" + rows + after
}

// ---- test-scenarios migration (Phase 13) ------------------------------

var migrateTestScenariosCmd = &cobra.Command{
	Use:   "test-scenarios",
	Short: "Seed test-results.json scenario_coverage from simulations (Phase 13)",
	Long: `Phase 13 requires every simulation scenario to be linked by a
jig:scenario test tag. Legacy recipes have scenarios but no tagged
tests. This migration populates test-results.json's scenario_coverage
map with an entry for every simulation scenario id:

  - Value ["legacy"] if no matching test name can be guessed from the
    scenario id / description. The checker treats these as
    acknowledged-but-unverified; monitoring (Phase 17) tracks the
    percentage still in legacy state.
  - Value ["<guess>"] when the scenario id appears in a test file name
    or description. The author is expected to verify and replace the
    guess with actual test names in a follow-up pass.

Backup: test-results.json.bak.scenarios.`,
	RunE: runMigrateTestScenarios,
}

func runMigrateTestScenarios(cmd *cobra.Command, args []string) error {
	target, dryRun, err := migrateFlags(cmd)
	if err != nil {
		return err
	}
	recipesDir := filepath.Join(state.SpecsPath(target), "recipes")
	entries, err := os.ReadDir(recipesDir)
	if err != nil {
		return fmt.Errorf("read recipes dir: %w", err)
	}

	var totalRecipes, migratedRecipes, addedEntries int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		recipeDir := filepath.Join(recipesDir, e.Name())
		resultsPath := filepath.Join(recipeDir, "test-results.json")
		if _, err := os.Stat(resultsPath); os.IsNotExist(err) {
			continue
		}
		simsDir := filepath.Join(recipeDir, "simulations")
		if _, err := os.Stat(simsDir); os.IsNotExist(err) {
			continue
		}
		totalRecipes++

		added, err := migrateOneTestScenarios(recipeDir, resultsPath, dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s: error: %v\n", e.Name(), err)
			continue
		}
		if added > 0 {
			migratedRecipes++
			addedEntries += added
			marker := "added"
			if dryRun {
				marker = "would add"
			}
			fmt.Printf("  %s: %s %d scenario_coverage entr(y|ies)\n", e.Name(), marker, added)
		}
	}
	if dryRun {
		fmt.Printf("\nDry run: %d/%d recipes need migration (%d entries).\n", migratedRecipes, totalRecipes, addedEntries)
	} else {
		fmt.Printf("\nMigrated %d/%d recipes (%d entries). Backups: test-results.json.bak.scenarios\n", migratedRecipes, totalRecipes, addedEntries)
	}
	return nil
}

func migrateOneTestScenarios(recipeDir, resultsPath string, dryRun bool) (int, error) {
	// Collect known simulation scenario ids via the engine's own
	// collector. Stay honest about what the checker counts.
	scenarios := engine.CollectSimulationScenarioIDsForMigration(recipeDir)
	if len(scenarios) == 0 {
		return 0, nil
	}

	data, err := os.ReadFile(resultsPath)
	if err != nil {
		return 0, err
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		return 0, fmt.Errorf("parse test-results.json: %w", err)
	}

	coverage, _ := payload["scenario_coverage"].(map[string]interface{})
	if coverage == nil {
		coverage = map[string]interface{}{}
	}

	added := 0
	for _, id := range scenarios {
		if _, ok := coverage[id]; ok {
			continue
		}
		coverage[id] = []interface{}{"legacy"}
		added++
	}
	if added == 0 {
		return 0, nil
	}
	payload["scenario_coverage"] = coverage

	if dryRun {
		return added, nil
	}
	if err := os.WriteFile(resultsPath+".bak.scenarios", data, 0644); err != nil {
		return 0, err
	}
	out, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return 0, err
	}
	if strings.HasSuffix(string(data), "\n") {
		out = append(out, '\n')
	}
	return added, os.WriteFile(resultsPath, out, 0644)
}

// ---- all-in-one convenience -------------------------------------------

var migrateAllCmd = &cobra.Command{
	Use:   "all",
	Short: "Run all migrations in sequence (Sprint 1..7 in order)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := runMigrateVerifyLog(cmd, args); err != nil {
			return err
		}
		fmt.Println()
		if err := runMigrateChangelog(cmd, args); err != nil {
			return err
		}
		fmt.Println()
		if err := runMigrateVerification(cmd, args); err != nil {
			return err
		}
		fmt.Println()
		if err := runMigrateSimulations(cmd, args); err != nil {
			return err
		}
		fmt.Println()
		if err := runMigrateTaskAnchors(cmd, args); err != nil {
			return err
		}
		fmt.Println()
		if err := runMigrateModifyScope(cmd, args); err != nil {
			return err
		}
		fmt.Println()
		if err := runMigrateDeviationDriver(cmd, args); err != nil {
			return err
		}
		fmt.Println()
		if err := runMigrateSimDeviations(cmd, args); err != nil {
			return err
		}
		fmt.Println()
		if err := runMigrateTestScenarios(cmd, args); err != nil {
			return err
		}
		fmt.Println()
		return runMigrateSettings(cmd, args)
	},
}

// ---- settings migration (v0.5.0+ added keys) --------------------------

var migrateSettingsCmd = &cobra.Command{
	Use:   "settings",
	Short: "Append missing default keys to .jig/config/settings.yaml",
	Long: `Each jig release may add new settings keys. Existing projects that
init'd under an older release end up with a settings.yaml that lacks
those keys — so LoadSettings quietly returns the default, and any
derived logic (e.g. the midrun_review_every denominator in
ComputeRecipeStats) reads 0 from the file while the code assumes the
default. This command reconciles by appending every known-added key
that is missing from the file, with a preceding comment naming the
release it was introduced in. Existing keys are never overwritten.

Backup: settings.yaml.bak.`,
	RunE: runMigrateSettings,
}

// settingsInsertion records one default key that shipped after the
// initial settings.yaml template. Adding a new key to a future release
// means appending one entry here; the migrator handles the rest.
type settingsInsertion struct {
	Parent  string // second-level parent key, e.g. "implement"
	Key     string // the key being added
	Value   string // YAML-literal form (int, string, bool)
	Comment string // one-line explanation
	Since   string // release tag for the trailing "Added in vX.Y.Z" hint
}

var settingsInsertions = []settingsInsertion{
	{
		Parent:  "implement",
		Key:     "midrun_review_every",
		Value:   "5",
		Comment: "Emit reviews/midrun-*.md every N completed tasks (0 disables).",
		Since:   "v0.5.0",
	},
}

func runMigrateSettings(cmd *cobra.Command, args []string) error {
	target, dryRun, err := migrateFlags(cmd)
	if err != nil {
		return err
	}
	path := filepath.Join(target, ".jig", "config", "settings.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("No settings.yaml at %s — nothing to migrate.\n", path)
			return nil
		}
		return err
	}

	updated := string(data)
	var added []string
	var skippedCommented []string
	for _, ins := range settingsInsertions {
		if hasNestedKey(updated, ins.Parent, ins.Key) {
			continue
		}
		if hasCommentedKey(updated, ins.Key) {
			skippedCommented = append(skippedCommented, ins.Parent+"."+ins.Key)
			continue
		}
		next, ok := injectSettingKey(updated, ins)
		if !ok {
			fmt.Fprintf(os.Stderr, "  skipped %s.%s (could not locate a safe insertion point)\n", ins.Parent, ins.Key)
			continue
		}
		updated = next
		added = append(added, ins.Parent+"."+ins.Key)
	}

	for _, k := range skippedCommented {
		fmt.Printf("  %s: skipped (appears commented-out in the file — treat as explicit opt-out)\n", k)
	}

	if len(added) == 0 {
		fmt.Println("settings.yaml already has all known defaults.")
		return nil
	}

	if dryRun {
		fmt.Printf("Dry run: would add %d key(s): %s\n", len(added), strings.Join(added, ", "))
		return nil
	}

	if err := os.WriteFile(path+".bak", data, 0644); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
		return err
	}
	fmt.Printf("Added %d key(s): %s. Backup: settings.yaml.bak\n", len(added), strings.Join(added, ", "))
	return nil
}

// hasNestedKey returns true if `content` already has `key:` somewhere
// inside the top-level `parent:` block. Comments and commented-out
// forms do NOT count — see hasCommentedKey for that.
func hasNestedKey(content, parent, key string) bool {
	parentRe := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(parent) + `:\s*$`)
	loc := parentRe.FindStringIndex(content)
	if loc == nil {
		return false
	}
	after := content[loc[1]:]
	end := nextTopLevelOrEOF(after)
	keyRe := regexp.MustCompile(`(?m)^\s+` + regexp.QuoteMeta(key) + `:`)
	return keyRe.MatchString(after[:end])
}

// hasCommentedKey detects `# key:` (optionally indented) anywhere in
// the file. If present we leave the file alone — the user may have
// deliberately disabled the key by commenting it out, and silently
// adding it back would surprise them.
func hasCommentedKey(content, key string) bool {
	re := regexp.MustCompile(`(?m)^\s*#\s*` + regexp.QuoteMeta(key) + `:`)
	return re.MatchString(content)
}

// nextTopLevelOrEOF scans `s` and returns the byte offset where the
// next top-level (column-0, non-whitespace) line starts — i.e. where
// the current nested block ends. Returns len(s) if no such line.
func nextTopLevelOrEOF(s string) int {
	lines := strings.Split(s, "\n")
	offset := 0
	for _, ln := range lines {
		if len(ln) > 0 && ln[0] != ' ' && ln[0] != '\t' {
			return offset
		}
		offset += len(ln) + 1
	}
	if offset > len(s) {
		return len(s)
	}
	return offset
}

// injectSettingKey inserts a new key+comment into the parent block.
// If the parent section is missing entirely, it appends a fresh
// section at EOF.
func injectSettingKey(content string, ins settingsInsertion) (string, bool) {
	parentRe := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(ins.Parent) + `:\s*$`)
	loc := parentRe.FindStringIndex(content)
	if loc == nil {
		appended := fmt.Sprintf("\n%s:\n  # %s (added in %s)\n  %s: %s\n",
			ins.Parent, ins.Comment, ins.Since, ins.Key, ins.Value)
		return strings.TrimRight(content, "\n") + "\n" + appended, true
	}

	parentEnd := loc[1]
	after := content[parentEnd:]
	blockEnd := nextTopLevelOrEOF(after)
	blockText := after[:blockEnd]
	rest := after[blockEnd:]

	indent := detectBlockIndent(blockText)
	trimmed := strings.TrimRight(blockText, "\n ")
	trailing := blockText[len(trimmed):]

	insertion := fmt.Sprintf("\n%s# %s (added in %s)\n%s%s: %s",
		indent, ins.Comment, ins.Since, indent, ins.Key, ins.Value)

	return content[:parentEnd] + trimmed + insertion + trailing + rest, true
}

// detectBlockIndent picks the first non-empty line in `block` whose
// first character is whitespace and returns the leading run of
// whitespace. Falls back to two spaces when the block is empty.
func detectBlockIndent(block string) string {
	for _, ln := range strings.Split(block, "\n") {
		if len(ln) == 0 {
			continue
		}
		if ln[0] != ' ' && ln[0] != '\t' {
			continue
		}
		i := 0
		for i < len(ln) && (ln[i] == ' ' || ln[i] == '\t') {
			i++
		}
		if i < len(ln) {
			return ln[:i]
		}
	}
	return "  "
}

func migrateFlags(cmd *cobra.Command) (string, bool, error) {
	targetFlag, _ := cmd.Flags().GetString("target")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	target := targetFlag
	if target == "" {
		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return "", false, fmt.Errorf("not a jig project: %w", err)
		}
		target = root
	}
	return target, dryRun, nil
}
