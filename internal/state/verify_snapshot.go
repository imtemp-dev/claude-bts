package state

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Verify snapshots — the document revision as of the last completed
// verification. `bts recipe log --from-verification --doc <path>`
// saves one; `bts recipe verify-focus <path>` diffs the current doc
// against it to give the next verification round focus hints.
// Snapshots live under local/ (never committed, like tool-trace).

// VerifySnapshotDir returns .bts/local/recipes/<id>/verify-snapshots.
func VerifySnapshotDir(root, recipeID string) string {
	return filepath.Join(LocalPath(root), "recipes", recipeID, "verify-snapshots")
}

// VerifySnapshotPath returns the snapshot path for a doc basename.
func VerifySnapshotPath(root, recipeID, docBase string) string {
	return filepath.Join(VerifySnapshotDir(root, recipeID), docBase)
}

// SaveVerifySnapshot copies the doc's current content as the
// last-verified revision (atomic: temp + rename).
func SaveVerifySnapshot(root, recipeID, docPath string) error {
	data, err := os.ReadFile(docPath)
	if err != nil {
		return fmt.Errorf("read doc: %w", err)
	}
	dir := VerifySnapshotDir(root, recipeID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	dst := filepath.Join(dir, filepath.Base(docPath))
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	return os.Rename(tmp, dst)
}

// LoadVerifySnapshot returns the snapshot content for a doc basename.
// ok=false when no snapshot exists (first verification).
func LoadVerifySnapshot(root, recipeID, docBase string) ([]byte, bool, error) {
	data, err := os.ReadFile(VerifySnapshotPath(root, recipeID, docBase))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return data, true, nil
}

// DirtyVerifiedDocs returns basenames of documents whose current
// content differs from their last-verified snapshot — i.e. documents
// modified AFTER their last verification (rule 3 violations). Returns
// nil when no snapshot dir exists: legacy recipes (pre --doc) have
// nothing enforceable, and blocking them would be a false positive.
// Docs whose snapshot exists but whose current file is gone are
// skipped — missing-document problems belong to other gates.
func DirtyVerifiedDocs(root, recipeID string) ([]string, error) {
	dir := VerifySnapshotDir(root, recipeID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var dirty []string
	for _, e := range entries {
		// .tmp files are leftovers of a crashed atomic write, not snapshots.
		if e.IsDir() || strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		snap, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		cur, err := os.ReadFile(filepath.Join(RecipeDir(root, recipeID), e.Name()))
		if err != nil {
			continue
		}
		if !bytes.Equal(snap, cur) {
			dirty = append(dirty, e.Name())
		}
	}
	sort.Strings(dirty)
	return dirty, nil
}

// RecipeIDFromDocPath extracts the recipe ID from a document path like
// .bts/specs/recipes/<id>/draft.md. Returns "" if the path does not
// contain a recipes/<id>/ segment.
func RecipeIDFromDocPath(path string) string {
	parts := strings.Split(filepath.ToSlash(filepath.Clean(path)), "/")
	for i, p := range parts {
		if p == "recipes" && i+2 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}
