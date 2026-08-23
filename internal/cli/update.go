package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/imtemp-dev/claude-jig/internal/state"
	"github.com/imtemp-dev/claude-jig/internal/template"
	"github.com/imtemp-dev/claude-jig/pkg/version"
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
			return fmt.Errorf("not a jig project. Run 'jig init' first")
		}

		current := version.GetTemplateVersion()
		versionFile := filepath.Join(root, ".jig", "config", ".template-version")
		existing, _ := os.ReadFile(versionFile)
		oldVer := strings.TrimSpace(string(existing))

		if oldVer == current {
			fmt.Printf("Templates already up to date (%s)\n", current)
			return nil
		}

		// DeployForce (same skip list as auto-update and init --force).
		// .gitignore is user-owned — merged via EnsureGitignore, never overwritten.
		skipFiles := []string{".jig/config/settings.yaml", ".mcp.json", ".gitignore"}
		updated, err := template.DeployForce(root, skipFiles)
		if err != nil {
			return fmt.Errorf("update templates: %w", err)
		}

		// Ensure .gitignore ignores jig local data without destroying existing rules.
		if err := template.EnsureGitignore(root); err != nil {
			fmt.Fprintf(os.Stderr, "warning: update .gitignore: %v\n", err)
		}

		// Write new version
		if err := os.WriteFile(versionFile, []byte(current), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "warning: save template version: %v\n", err)
		}

		// Clean up template files left under a retired name prefix
		if n := template.CleanupLegacyPrefixes(root); n > 0 {
			fmt.Printf("Cleaned up %d legacy template files\n", n)
		}
		if from := template.MigrateHookSettings(root); from != "" {
			fmt.Printf("Migrated hook settings: %s → jig\n", from)
		}
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



// deadSettingsKeys are settings.yaml keys jig itself once shipped and
// nothing has ever read. `jig update` deletes them.
//
// settings.yaml is user-owned and deliberately preserved across updates
// (it is in skipFiles above), so removing a key from the template only
// affects projects created by a fresh `jig init`. Every existing project
// kept its copy — and `jig doctor` reports unread keys, so upgrading
// turned a healthy project's `--strict` run red with a remedy that said
// "delete them" and no way to do it but by hand.
//
// Only keys that were shipped by jig and read by nothing belong here. A
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
	path := filepath.Join(root, ".jig", "config", "settings.yaml")
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
