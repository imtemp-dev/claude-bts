package template

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// legacyPrefixes are name prefixes this tool has shipped templates under
// before. Ordered newest-first so the message names the one actually found.
var legacyPrefixes = []string{"bts", "forge"}

// CleanupLegacyPrefixes removes deployed template files carrying a retired
// name prefix. Deployment only writes the current jig-* set and never deletes,
// so without this the old files stay and Claude Code loads both: every skill,
// agent and rule appears twice, and the stale copy references a binary and
// recipe types that no longer exist.
//
// Safe to call when there is nothing to clean — it reports 0 and returns.
func CleanupLegacyPrefixes(root string) int {
	claudeDir := filepath.Join(root, ".claude")
	removed := 0

	for _, d := range []string{"skills", "agents", "rules", "hooks", "commands"} {
		base := filepath.Join(claudeDir, d)
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !hasLegacyPrefix(entry.Name()) {
				continue
			}
			if err := os.RemoveAll(filepath.Join(base, entry.Name())); err == nil {
				removed++
			}
		}
	}

	// A status_line.sh left under a retired state directory. FindRoot renames
	// the directory itself, so this only matters if both somehow survived.
	for _, old := range legacyPrefixes {
		oldStatus := filepath.Join(root, "."+old, "status_line.sh")
		if _, err := os.Stat(oldStatus); err == nil {
			_ = os.Remove(oldStatus)
			removed++
		}
	}

	return removed
}

func hasLegacyPrefix(name string) bool {
	for _, p := range legacyPrefixes {
		if strings.HasPrefix(name, p+"-") {
			return true
		}
	}
	return false
}

// MigrateHookSettings repoints hook commands left by an earlier name at the
// jig-handle-* scripts, returning the retired name it migrated from ("" if
// there was nothing to do).
//
// settings.local.json stores absolute paths to the hook scripts. A rebrand
// renames those scripts, so without this rewrite every hook points at a file
// that no longer exists — and a hook that cannot run is a gate that fails
// open, which is the one failure mode this tool must never have.
func MigrateHookSettings(root string) string {
	path := filepath.Join(root, ".claude", "settings.local.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := string(data)
	migrated := ""
	for _, old := range legacyPrefixes {
		if !strings.Contains(content, old+"-handle-") {
			continue
		}
		content = strings.ReplaceAll(content, old+"-handle-", "jig-handle-")
		content = strings.ReplaceAll(content, "."+old+"/status_line.sh", ".jig/status_line.sh")
		migrated = old
	}
	if migrated == "" {
		return ""
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "warning: rewrite hook settings: %v\n", err)
		return ""
	}
	return migrated
}
