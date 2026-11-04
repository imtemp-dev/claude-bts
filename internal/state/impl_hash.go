package state

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// ImplHash fingerprints the code a recipe's tasks name, so a verdict can
// record what it judged and a gate can tell whether that code is still
// what is on disk.
//
// It is ContentHash over the sorted lines "path NUL content-hash" of every
// distinct task file, with "absent" for a file that does not exist (a
// delete task, or a create task not yet written). Using the task files
// rather than git keeps it working in repositories with no commits, and
// keeps changes outside the recipe (.bts/local, unrelated work) from
// reading as drift.
//
// Test files are deliberately left out: `bts test run` records the hash
// before the orchestrator fills in test-results.json's test_files, so a
// fingerprint that included them would never match at the gate.
//
// It returns "" when no task names a file.
func ImplHash(root string, tasks []Task) (string, error) {
	seen := make(map[string]bool, len(tasks))
	lines := make([]string, 0, len(tasks))
	for _, t := range tasks {
		if strings.TrimSpace(t.File) == "" {
			continue
		}
		rel := filepath.ToSlash(filepath.Clean(t.File))
		if seen[rel] {
			continue
		}
		seen[rel] = true

		full := t.File
		if !filepath.IsAbs(full) {
			full = filepath.Join(root, full)
		}
		h, ok, err := FileContentHash(full)
		if err != nil {
			return "", fmt.Errorf("hash %s: %w", rel, err)
		}
		if !ok {
			h = "absent"
		}
		lines = append(lines, rel+"\x00"+h)
	}
	if len(lines) == 0 {
		return "", nil
	}
	sort.Strings(lines)
	return ContentHash([]byte(strings.Join(lines, "\n"))), nil
}

// RecipeImplHash is ImplHash over the recipe's tasks.json. It returns ""
// with a nil error when the recipe has no tasks.json, which is normal
// before decomposition and for fix recipes that never wrote one.
func RecipeImplHash(root, recipeID string) (string, error) {
	ts, err := LoadTaskState(root, recipeID)
	if err != nil {
		return "", nil
	}
	return ImplHash(root, ts.Tasks)
}
