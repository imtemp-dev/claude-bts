package template

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// UpdateResult says what Update did, for the caller to report.  Update
// itself prints nothing: the SessionStart hook's stdout is the JSON the
// harness parses, so a stray line there would break the hook.
type UpdateResult struct {
	OldVersion string   // the project's recorded template version before the update, "" if none
	Deployed   bool     // templates were deployed because the recorded version or template hash differed
	Files      []string // files the deploy wrote
	Notes      []string // one line per cleanup step that changed something
	Warnings   []string // steps that failed without stopping the update
}

// updateSkipFiles are user-owned files a deploy never overwrites.
// .gitignore is merged through EnsureGitignore instead.
var updateSkipFiles = []string{".bts/config/settings.yaml", ".mcp.json", ".gitignore"}

// Update brings a project's templates to the binary's template version
// and removes what earlier versions left behind.  `bts update` and the
// SessionStart auto-update both call it, so the two paths cannot drift
// apart again: the auto-update used to deploy and record the new version
// without the cleanup, and `bts update` then said "already up to date"
// and never ran it, which left retired agents and skills listed by the
// harness in every auto-updated project.
//
// The deploy runs only when the recorded version or template hash differs.  The cleanup
// steps are idempotent and run every time, so a project an older binary
// left with retired files is cleaned on its next update or session start.
// The version is recorded last, so an update that fails half way is
// retried next time.
func Update(root, current string) (*UpdateResult, error) {
	return update(root, current, true)
}

// CleanupOnly runs Update's idempotent cleanup and leaves the templates and
// their recorded version as they are; OldVersion says which version the
// project is on.  The SessionStart hook calls it while a recipe is active:
// a session start that deployed mid-recipe moved r-003 from 944b3e7 to
// 8bfa2c8 between two of its steps (trial problem #55), so the move waits
// for a `bts update` the user runs between steps.  The cleanup still runs
// because it brings the project in line with the binary already running,
// not with the templates.
func CleanupOnly(root string) *UpdateResult {
	res, _ := update(root, "", false)
	return res
}

func update(root, current string, deploy bool) (*UpdateResult, error) {
	versionFile := filepath.Join(root, ".bts", "config", ".template-version")
	existing, _ := os.ReadFile(versionFile)
	res := &UpdateResult{OldVersion: strings.TrimSpace(string(existing))}

	// The recorded template hash catches templates that changed under the
	// same version (trial problem #15).  A project with none recorded was
	// deployed before hashes were kept: the version alone decides for it.
	hashFile := filepath.Join(root, filepath.FromSlash(TemplateHashFile))
	recordedHash, _ := os.ReadFile(hashFile)
	currentHash := TemplatesHash()
	hashMoved := len(recordedHash) > 0 && currentHash != "" && strings.TrimSpace(string(recordedHash)) != currentHash

	if deploy && (res.OldVersion != current || hashMoved) {
		files, err := DeployForce(root, updateSkipFiles)
		if err != nil {
			return res, fmt.Errorf("update templates: %w", err)
		}
		res.Deployed, res.Files = true, files
		if err := EnsureGitignore(root); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("update .gitignore: %v", err))
		}
	}

	res.note(cleanupLegacyForge(root))
	res.note(removeRetiredTemplates(root, &res.Warnings))
	res.note(migrateHookSettings(root))
	res.note(pruneDeadSettings(root, &res.Warnings))
	res.note(moveSimulateFindingBatch(root, &res.Warnings))

	if res.Deployed {
		if err := MergeHookSettings(root); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("update hook settings: %v", err))
		}
		if err := os.WriteFile(versionFile, []byte(current), 0644); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("save template version: %v", err))
		}
		if currentHash != "" {
			if err := os.WriteFile(hashFile, []byte(currentHash+"\n"), 0644); err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("save template hash: %v", err))
			}
		}
	}
	return res, nil
}

func (r *UpdateResult) note(line string) {
	if line != "" {
		r.Notes = append(r.Notes, line)
	}
}

// migrateHookSettings replaces forge-handle-* with bts-handle-* in settings.local.json.
func migrateHookSettings(root string) string {
	path := filepath.Join(root, ".claude", "settings.local.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := string(data)
	if !strings.Contains(content, "forge-handle-") {
		return ""
	}
	content = strings.ReplaceAll(content, "forge-handle-", "bts-handle-")
	content = strings.ReplaceAll(content, ".forge/status_line.sh", ".bts/status_line.sh")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return ""
	}
	return "Migrated hook settings: forge → bts"
}

// cleanupLegacyForge removes old forge-* template files left from pre-rename versions.
func cleanupLegacyForge(root string) string {
	claudeDir := filepath.Join(root, ".claude")
	removed := 0
	for _, d := range []string{"skills", "agents", "rules", "hooks", "commands"} {
		base := filepath.Join(claudeDir, d)
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "forge-") {
				if err := os.RemoveAll(filepath.Join(base, entry.Name())); err == nil {
					removed++
				}
			}
		}
	}
	// Remove the legacy .forge/ status line.
	oldStatus := filepath.Join(root, ".forge", "status_line.sh")
	if _, err := os.Stat(oldStatus); err == nil {
		if os.Remove(oldStatus) == nil {
			removed++
		}
	}
	if removed == 0 {
		return ""
	}
	return fmt.Sprintf("Cleaned up %d legacy forge files", removed)
}

// retiredTemplateFiles are template files bts once shipped and no longer
// does.  Update deletes them from the project.
//
// DeployForce writes what the current binary embeds and never looks at
// what an earlier one left behind, so a file removed from the template
// stayed in every project that had it.  That was harmless for the command
// stubs removed in bc45f51, which merely duplicated a skill.  It is not
// harmless for an agent: a retired agent file keeps describing the role
// it had ("used by /bts-simulate") to a harness that still lists it, and
// an orchestrator reading the list can spawn it in good faith.
//
// Paths are relative to the project root.  A test refuses any entry the
// template still ships, so the list cannot delete a live file.
var retiredTemplateFiles = []string{
	// /bts-simulate runs as the simulator agent and no longer
	// spawns a validator/rebuttal pair per finding; /bts-defend argues
	// against the round's open findings on the ledger instead.
	".claude/agents/bts-simulator-validator.md",
	".claude/agents/bts-simulator-rebuttal.md",
	// Simulation was removed (docs/research/prove-stage-design.md,
	// decision 6); the prove stage proves behaviour by runs instead.
	".claude/agents/bts-simulator.md",
	".claude/skills/bts-simulate/SKILL.md",
}

// removeRetiredTemplates deletes retiredTemplateFiles from the project,
// and a skill directory the deletion left empty.
func removeRetiredTemplates(root string, warnings *[]string) string {
	removed := 0
	for _, rel := range retiredTemplateFiles {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if err := os.Remove(path); err != nil {
			*warnings = append(*warnings, fmt.Sprintf("remove retired template %s: %v", rel, err))
			continue
		}
		removed++
		if strings.HasPrefix(rel, ".claude/skills/") {
			_ = os.Remove(filepath.Dir(path)) // only succeeds when empty
		}
	}
	if removed == 0 {
		return ""
	}
	return fmt.Sprintf("Removed %d retired template file(s)", removed)
}

// deadSettingsKeys are settings.yaml keys bts itself once shipped and
// nothing has ever read.  Update deletes them.
//
// settings.yaml is user-owned and deliberately preserved across updates
// (it is in updateSkipFiles), so removing a key from the template only
// affects projects created by a fresh `bts init`.  Every existing project
// kept its copy, and `bts doctor` reports unread keys, so upgrading
// turned a healthy project's `--strict` run red with a remedy that said
// "delete them" and no way to do it but by hand.
//
// Only keys that were shipped by bts and read by nothing belong here.  A
// key a user added themselves is theirs; doctor reports it and leaves it
// alone.
var deadSettingsKeys = [][]string{
	{"context", "clear_threshold"},
	{"privacy", "strip_private_tags"},
	{"privacy", "detect_secrets"},
	{"resume", "changelog_tail"},
	{"fix", "simulate_scenarios"},
	// simulation was removed; simulate.finding_batch alone is still read,
	// as the fallback for defend.finding_batch.
	{"simulate", "min_scenarios"},
	{"simulate", "max_scenarios"},
	{"simulate", "scenario_batch"},
	{"simulate", "cross_boundary_ratio"},
	{"verify", "convergence"},
}

// pruneDeadSettings removes deadSettingsKeys from the project's
// settings.yaml, and any section left empty by the removal.  Comments and
// key order elsewhere are preserved by editing the text rather than
// re-serialising the tree.
func pruneDeadSettings(root string, warnings *[]string) string {
	path := filepath.Join(root, ".bts", "config", "settings.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	out, removed := stripYAMLKeys(string(data), deadSettingsKeys)
	if len(removed) == 0 {
		return ""
	}
	if err := os.WriteFile(path, []byte(out), 0644); err != nil {
		*warnings = append(*warnings, fmt.Sprintf("prune settings.yaml: %v", err))
		return ""
	}
	return fmt.Sprintf("settings.yaml: removed %d key(s) nothing reads: %s",
		len(removed), strings.Join(removed, ", "))
}

// moveSimulateFindingBatch moves an old `simulate.finding_batch` to
// `defend.finding_batch` and drops the `simulate:` section, which names a
// stage bts no longer has (trial problem #38).  When `defend` already sets
// finding_batch, that value wins as it always did, and the old key just goes.
func moveSimulateFindingBatch(root string, warnings *[]string) string {
	path := filepath.Join(root, ".bts", "config", "settings.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text := string(data)
	value, ok := yamlValue(text, "simulate", "finding_batch")
	if !ok {
		return ""
	}
	out, _ := stripYAMLKeys(text, [][]string{{"simulate", "finding_batch"}})
	note := "settings.yaml: removed simulate.finding_batch (defend.finding_batch is set)"
	if _, has := yamlValue(text, "defend", "finding_batch"); !has {
		out = strings.TrimRight(out, "\n") + "\n\n# Defense\ndefend:\n  finding_batch: " + value + "\n"
		note = "settings.yaml: moved simulate.finding_batch to defend.finding_batch (" + value + ")"
	}
	if err := os.WriteFile(path, []byte(out), 0644); err != nil {
		*warnings = append(*warnings, fmt.Sprintf("move simulate.finding_batch: %v", err))
		return ""
	}
	return note
}

// yamlValue returns the scalar value of `section.key` in a two-level YAML
// document, without its trailing comment.
func yamlValue(text, section, key string) (string, bool) {
	current := ""
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, ":") {
			continue
		}
		k, v, _ := strings.Cut(trimmed, ":")
		if line[0] != ' ' && line[0] != '\t' {
			current = strings.TrimSpace(k)
			continue
		}
		if current == section && strings.TrimSpace(k) == key {
			v, _, _ = strings.Cut(v, "#")
			return strings.TrimSpace(v), true
		}
	}
	return "", false
}
