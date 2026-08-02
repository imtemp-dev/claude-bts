package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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

// ContentHash returns the identity of a document's content for
// verification bookkeeping.
//
// Line endings are normalised first: whether a checkout materialises a
// markdown file with LF or CRLF is a property of the checkout, not of
// the content that was verified, and letting it change the hash would
// reintroduce exactly the cross-checkout false positive these hashes
// exist to remove.
func ContentHash(data []byte) string {
	sum := sha256.Sum256(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// FileContentHash hashes a file's content. ok=false when the file does
// not exist, which is a normal state (no verification.md yet), not an
// error.
func FileContentHash(path string) (hash string, ok bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return ContentHash(data), true, nil
}

// LastFullPassHashes folds a verify-log into the last recorded content
// hash per document, considering only full passes.
//
// Full passes only, because that mirrors what SaveVerifySnapshot does: a
// delta round verified part of the document, so it is not evidence that
// the whole document as it now stands has been verified.
func LastFullPassHashes(entries []VerifyLogEntry) map[string]string {
	hashes := map[string]string{}
	for i := range entries {
		e := entries[i]
		if e.Doc == "" || e.DocHash == "" || !e.FullPass {
			continue
		}
		hashes[e.Doc] = e.DocHash
	}
	return hashes
}

// DirtyVerifiedDocs returns basenames of documents whose current content
// differs from the revision that was last verified — i.e. documents
// modified AFTER their last verification (rule 3 violations).
//
// The recorded hash in verify-log.jsonl is authoritative, because it is
// tracked and therefore present in every worktree and fresh clone. The
// local snapshot is consulted only for documents the log cannot speak
// for: rounds logged before DocHash existed. Without that fallback,
// upgrading would silently switch the gate off for in-flight recipes
// until their next full verification.
//
// Returns nil when neither source has anything to say — legacy recipes
// have nothing enforceable and blocking them would be a false positive.
// Docs whose recorded revision exists but whose current file is gone are
// skipped; missing-document problems belong to other gates.
func DirtyVerifiedDocs(root, recipeID string) ([]string, error) {
	recipeDir := RecipeDir(root, recipeID)

	entries, err := ReadVerifyLog(root, recipeID)
	if err != nil {
		return nil, err
	}
	verified := LastFullPassHashes(entries)

	var dirty []string
	for doc, want := range verified {
		got, ok, herr := FileContentHash(filepath.Join(recipeDir, doc))
		if herr != nil || !ok {
			continue
		}
		if got != want {
			dirty = append(dirty, doc)
		}
	}

	// Snapshot fallback, for documents with no recorded hash.
	snapDir := VerifySnapshotDir(root, recipeID)
	snaps, serr := os.ReadDir(snapDir)
	if serr != nil && !os.IsNotExist(serr) {
		return nil, serr
	}
	for _, e := range snaps {
		// .tmp files are leftovers of a crashed atomic write, not snapshots.
		if e.IsDir() || strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		if _, covered := verified[e.Name()]; covered {
			continue
		}
		snap, rerr := os.ReadFile(filepath.Join(snapDir, e.Name()))
		if rerr != nil {
			continue
		}
		cur, rerr := os.ReadFile(filepath.Join(recipeDir, e.Name()))
		if rerr != nil {
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
