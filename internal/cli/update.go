package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/imtemp-dev/claude-bts/internal/template"
	"github.com/imtemp-dev/claude-bts/pkg/version"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(updateCmd)
}

var updateCmd = &cobra.Command{
	Use:     "update",
	Short:   "Update templates to match current binary version",
	GroupID: "project",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a bts project. Run 'bts init' first")
		}

		current := version.GetTemplateVersion()
		versionFile := filepath.Join(root, ".bts", "config", ".template-version")
		existing, _ := os.ReadFile(versionFile)
		oldVer := strings.TrimSpace(string(existing))

		if oldVer == current {
			fmt.Printf("Templates already up to date (%s)\n", current)
			return nil
		}

		// DeployForce (same skip list as auto-update and init --force).
		// .gitignore is user-owned — merged via EnsureGitignore, never overwritten.
		skipFiles := []string{".bts/config/settings.yaml", ".mcp.json", ".gitignore"}
		updated, err := template.DeployForce(root, skipFiles)
		if err != nil {
			return fmt.Errorf("update templates: %w", err)
		}

		// Ensure .gitignore ignores bts local data without destroying existing rules.
		if err := template.EnsureGitignore(root); err != nil {
			fmt.Fprintf(os.Stderr, "warning: update .gitignore: %v\n", err)
		}

		// Write new version
		if err := os.WriteFile(versionFile, []byte(current), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "warning: save template version: %v\n", err)
		}

		// Clean up legacy forge-* template files and settings
		cleanupLegacyForge(root)
		removeRetiredTemplates(root)
		migrateHookSettings(root)
		pruneDeadSettings(root)

		// Merge statusline and hook settings (same as init)
		if err := template.MergeHookSettings(root); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not update hook settings: %v\n", err)
		}

		if oldVer == "" {
			fmt.Printf("Templates initialized: %s\n", current)
		} else {
			fmt.Printf("Templates updated: %s → %s\n", oldVer, current)
		}
		fmt.Printf("Files updated: %d\n", len(updated))
		return nil
	},
}

// migrateHookSettings replaces forge-handle-* with bts-handle-* in settings.local.json.
func migrateHookSettings(root string) {
	path := filepath.Join(root, ".claude", "settings.local.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	content := string(data)
	if !strings.Contains(content, "forge-handle-") {
		return
	}
	content = strings.ReplaceAll(content, "forge-handle-", "bts-handle-")
	content = strings.ReplaceAll(content, ".forge/status_line.sh", ".bts/status_line.sh")
	_ = os.WriteFile(path, []byte(content), 0644)
	fmt.Println("Migrated hook settings: forge → bts")
}

// cleanupLegacyForge removes old forge-* template files left from pre-rename versions.
func cleanupLegacyForge(root string) {
	claudeDir := filepath.Join(root, ".claude")
	dirs := []string{"skills", "agents", "rules", "hooks", "commands"}
	removed := 0

	for _, d := range dirs {
		base := filepath.Join(claudeDir, d)
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "forge-") {
				path := filepath.Join(base, entry.Name())
				if err := os.RemoveAll(path); err == nil {
					removed++
				}
			}
		}
	}

	// Remove legacy .forge/ status_line.sh if .bts/ version exists
	oldStatus := filepath.Join(root, ".forge", "status_line.sh")
	if _, err := os.Stat(oldStatus); err == nil {
		_ = os.Remove(oldStatus)
		removed++
	}

	if removed > 0 {
		fmt.Printf("Cleaned up %d legacy forge files\n", removed)
	}
}

// retiredTemplateFiles are template files bts once shipped and no longer
// does. `bts update` deletes them from the project.
//
// DeployForce writes what the current binary embeds and never looks at
// what an earlier one left behind, so a file removed from the template
// stayed in every project that had it. That was harmless for the command
// stubs removed in bc45f51, which merely duplicated a skill. It is not
// harmless for an agent: a retired agent file keeps describing the role
// it had ("used by /bts-simulate") to a harness that still lists it, and
// an orchestrator reading the list can spawn it in good faith.
//
// Paths are relative to the project root. A test refuses any entry the
// template still ships, so the list cannot delete a live file.
var retiredTemplateFiles = []string{
	// /bts-simulate runs as the simulator agent and no longer
	// spawns a validator/rebuttal pair per finding; /bts-defend argues
	// against the round's open findings on the ledger instead.
	".claude/agents/bts-simulator-validator.md",
	".claude/agents/bts-simulator-rebuttal.md",
}

// removeRetiredTemplates deletes retiredTemplateFiles from the project.
func removeRetiredTemplates(root string) {
	removed := 0
	for _, rel := range retiredTemplateFiles {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if err := os.Remove(path); err != nil {
			fmt.Fprintf(os.Stderr, "warning: remove retired template %s: %v\n", rel, err)
			continue
		}
		removed++
	}
	if removed > 0 {
		fmt.Printf("Removed %d retired template file(s)\n", removed)
	}
}

// deadSettingsKeys are settings.yaml keys bts itself once shipped and
// nothing has ever read. `bts update` deletes them.
//
// settings.yaml is user-owned and deliberately preserved across updates
// (it is in skipFiles above), so removing a key from the template only
// affects projects created by a fresh `bts init`. Every existing project
// kept its copy — and `bts doctor` reports unread keys, so upgrading
// turned a healthy project's `--strict` run red with a remedy that said
// "delete them" and no way to do it but by hand.
//
// Only keys that were shipped by bts and read by nothing belong here. A
// key a user added themselves is theirs; doctor reports it and leaves it
// alone.
var deadSettingsKeys = [][]string{
	{"context", "clear_threshold"},
	{"privacy", "strip_private_tags"},
	{"privacy", "detect_secrets"},
	{"resume", "changelog_tail"},
	{"fix", "simulate_scenarios"},
	{"verify", "convergence"},
}

// pruneDeadSettings removes deadSettingsKeys from the project's
// settings.yaml, and any section left empty by the removal. Comments and
// key order elsewhere are preserved by editing the text rather than
// re-serialising the tree.
func pruneDeadSettings(root string) {
	path := filepath.Join(root, ".bts", "config", "settings.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	out, removed := stripYAMLKeys(string(data), deadSettingsKeys)
	if len(removed) == 0 {
		return
	}
	if err := os.WriteFile(path, []byte(out), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "warning: prune settings.yaml: %v\n", err)
		return
	}
	fmt.Printf("settings.yaml: removed %d key(s) nothing reads: %s\n",
		len(removed), strings.Join(removed, ", "))
}

// stripYAMLKeys deletes `section.key` paths (and a bare `section` path)
// from a YAML document, together with everything nested under them,
// returning the new text and the dotted paths actually removed.
//
// It works on lines because settings.yaml is a commented file a person
// reads and edits; round-tripping it through a YAML marshaller would
// silently strip every comment in it, which is most of its value.
func stripYAMLKeys(text string, paths [][]string) (string, []string) {
	drop := map[string]bool{}
	for _, p := range paths {
		drop[strings.Join(p, ".")] = true
	}

	lines := strings.Split(text, "\n")
	keep := make([]string, 0, len(lines))
	var removed []string
	section := ""
	skipDeeperThan := -1 // indent of the key being dropped; -1 when not dropping

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " \t"))

		// Inside a dropped subtree: everything more indented goes with it,
		// as do the blank lines between its entries.
		if skipDeeperThan >= 0 {
			if trimmed == "" || indent > skipDeeperThan {
				continue
			}
			skipDeeperThan = -1
		}

		if trimmed == "" || strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, ":") {
			keep = append(keep, line)
			continue
		}

		key := strings.TrimSpace(strings.SplitN(trimmed, ":", 2)[0])
		dotted := key
		if indent == 0 {
			section = key
		} else if section != "" {
			dotted = section + "." + key
		}
		if drop[dotted] {
			removed = append(removed, dotted)
			skipDeeperThan = indent
			continue
		}
		keep = append(keep, line)
	}

	// Drop sections the removal emptied, and collapse the blank runs it left.
	return collapseEmptySections(strings.Join(keep, "\n")), removed
}

// collapseEmptySections removes a top-level `name:` whose body is now
// empty, along with any comment block immediately above it, and squeezes
// runs of blank lines down to one.
func collapseEmptySections(text string) string {
	lines := strings.Split(text, "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		isTopKey := line != "" && line[0] != ' ' && line[0] != '\t' &&
			strings.HasSuffix(trimmed, ":") && !strings.HasPrefix(trimmed, "#")
		if isTopKey {
			// Look ahead: is there an indented line before the next
			// top-level key?
			empty := true
			for j := i + 1; j < len(lines); j++ {
				n := lines[j]
				if strings.TrimSpace(n) == "" {
					continue
				}
				if n[0] == ' ' || n[0] == '\t' {
					if !strings.HasPrefix(strings.TrimSpace(n), "#") {
						empty = false
					}
					continue
				}
				break
			}
			if empty {
				// Drop the comment block directly above it too.
				for len(out) > 0 {
					last := strings.TrimSpace(out[len(out)-1])
					if strings.HasPrefix(last, "#") {
						out = out[:len(out)-1]
						continue
					}
					break
				}
				continue
			}
		}
		if trimmed == "" && len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
