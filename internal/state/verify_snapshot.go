package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

// LastFullPassPaths folds a verify-log into the last recorded
// root-relative path per document, considering only full passes. Only
// documents that recorded one appear; the caller falls back to the
// recipe directory for the rest.
func LastFullPassPaths(entries []VerifyLogEntry) map[string]string {
	paths := map[string]string{}
	for i := range entries {
		e := entries[i]
		if e.Doc == "" || e.DocPath == "" || !e.FullPass {
			continue
		}
		paths[e.Doc] = e.DocPath
	}
	return paths
}

// DeltaChain is one document's verify history split at its last full
// pass: that pass, and the delta rounds recorded after it.
type DeltaChain struct {
	Full   *VerifyLogEntry  // the last full pass; nil when there is none
	Deltas []VerifyLogEntry // delta rounds after Full, in log order
}

// LastDeltaChain splits one document's history (VerifyEntriesForDoc) at
// its last full pass. With no full pass, Full is nil and Deltas holds
// nothing: delta rounds with no full pass before them follow nothing.
func LastDeltaChain(docEntries []VerifyLogEntry) DeltaChain {
	for i := len(docEntries) - 1; i >= 0; i-- {
		if docEntries[i].FullPass {
			full := docEntries[i]
			return DeltaChain{Full: &full, Deltas: append([]VerifyLogEntry(nil), docEntries[i+1:]...)}
		}
	}
	return DeltaChain{}
}

// Breach returns the first delta round after the full pass that found a
// critical or a major, or nil when none did.
func (c DeltaChain) Breach() *VerifyLogEntry {
	for i := range c.Deltas {
		if c.Deltas[i].Critical > 0 || c.Deltas[i].Major > 0 {
			d := c.Deltas[i]
			return &d
		}
	}
	return nil
}

// AbsorbedHash is the revision a clean chain of delta rounds reviewed:
// the last delta's DocHash. ok=false when there is no full pass, no
// delta after it, a delta that found a critical or major, or no recorded
// revision on the last delta.
//
// This is how the build recipe's one review absorbs a fix (trial problem
// #45). Before it, any edit to final.md after the review, a minor's fix
// included, made the document dirty until another full pass, and the fix
// text then bred new minors in that pass (r-003: one major, then three
// minors, two of them in the fix). A delta round reads the sections
// changed since the full pass (verify-focus always diffs against the
// full pass's snapshot) plus their references, which is what a fix needs;
// a delta that finds a new critical or major says the fix reached further
// than the sections it changed, and only a full pass answers that.
func (c DeltaChain) AbsorbedHash() (string, bool) {
	if c.Full == nil || len(c.Deltas) == 0 || c.Breach() != nil {
		return "", false
	}
	last := c.Deltas[len(c.Deltas)-1]
	if last.DocHash == "" {
		return "", false
	}
	return last.DocHash, true
}

// deltaAbsorbs reports whether a document's clean delta rounds count as
// its reviewed revision: only the build recipe's final.md (#45). Every
// other document keeps rule 3 as written: only a full pass advances the
// verified revision (bts-verification-protocol.md, Verification Scope).
func deltaAbsorbs(root, recipeID, doc string) bool {
	if !strings.EqualFold(doc, "final.md") {
		return false
	}
	r, err := LoadRecipeState(root, recipeID)
	return err == nil && r.Type == "build"
}

// DirtyVerifiedDocs returns basenames of documents whose current content
// differs from the revision that was last verified — i.e. documents
// modified AFTER their last verification (rule 3 violations).
//
// For a build recipe's final.md, a clean chain of delta rounds after the
// last full pass moves the reviewed revision to the last delta's
// (DeltaChain.AbsorbedHash, trial problem #45), so the blind-stop and
// session-start rule-3 checks agree with build_review_clean.
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
// CitedDocsChanged returns the documents doc's latest round read besides
// doc itself (VerifyLogEntry.CitedHashes) whose content has changed since,
// by basename and sorted.  A cited document that is gone counts as
// changed.  Nil when that round recorded none.
func CitedDocsChanged(root, recipeID, doc string) []string {
	entries, err := ReadVerifyLog(root, recipeID)
	if err != nil {
		return nil
	}
	return citedDocsChanged(root, recipeID, entries, doc)
}

func citedDocsChanged(root, recipeID string, entries []VerifyLogEntry, doc string) []string {
	scoped := VerifyEntriesForDoc(entries, doc)
	if len(scoped) == 0 {
		return nil
	}
	var changed []string
	for name, want := range scoped[len(scoped)-1].CitedHashes {
		got, ok, err := FileContentHash(filepath.Join(RecipeDir(root, recipeID), name))
		if err != nil {
			continue
		}
		if !ok || got != want {
			changed = append(changed, name)
		}
	}
	sort.Strings(changed)
	return changed
}

// DescribeDirty says what is stale about DirtyVerifiedDocs' documents, as
// the clause a message starts with: "final.md changed after its last
// verification", or, where only a document its review read changed,
// "final.md's review is out of date: domain.md changed after the review
// read it".  An agent told only "final.md changed" looks at an unchanged
// final.md (trial problem #106).  The names this gave before, with the
// cited document in parentheses, were followed in every message by
// "changed after its last verification", which said final.md changed when
// only domain.md had (trial problem #122).
func DescribeDirty(root, recipeID string, dirty []string) string {
	entries, _ := ReadVerifyLog(root, recipeID)
	verified := LastFullPassHashes(entries)
	paths := LastFullPassPaths(entries)
	var own, parts []string
	for _, d := range dirty {
		if want, recorded := verified[d]; recorded && !ownTextChanged(root, recipeID, entries, paths, d, want) {
			if cited := citedDocsChanged(root, recipeID, entries, d); len(cited) > 0 {
				parts = append(parts, fmt.Sprintf("%s's review is out of date: %s changed after the review read it", d, strings.Join(cited, ", ")))
				continue
			}
		}
		own = append(own, d)
	}
	switch len(own) {
	case 0:
	case 1:
		parts = append([]string{own[0] + " changed after its last verification"}, parts...)
	default:
		parts = append([]string{strings.Join(own, ", ") + " changed after their last verification"}, parts...)
	}
	return strings.Join(parts, "; ")
}

// ownTextChanged reports whether doc's own text differs from the text its
// rounds verified: want, or what a delta chain absorbed since.
func ownTextChanged(root, recipeID string, entries []VerifyLogEntry, paths map[string]string, doc, want string) bool {
	if deltaAbsorbs(root, recipeID, doc) {
		if h, ok := LastDeltaChain(VerifyEntriesForDoc(entries, doc)).AbsorbedHash(); ok {
			want = h
		}
	}
	// Where the round said it read the document, if it said. A
	// basename joined to the recipe directory is the fallback, and
	// was the only rule — which silently exempted every verified
	// document living anywhere else from rule 3.
	at := filepath.Join(RecipeDir(root, recipeID), doc)
	if p := paths[doc]; p != "" {
		at = filepath.Join(root, p)
	}
	got, ok, err := FileContentHash(at)
	return err == nil && ok && got != want
}

func DirtyVerifiedDocs(root, recipeID string) ([]string, error) {
	recipeDir := RecipeDir(root, recipeID)

	entries, err := ReadVerifyLog(root, recipeID)
	if err != nil {
		return nil, err
	}
	verified := LastFullPassHashes(entries)
	paths := LastFullPassPaths(entries)

	var dirty []string
	for doc, want := range verified {
		if ownTextChanged(root, recipeID, entries, paths, doc, want) {
			dirty = append(dirty, doc)
		}
	}

	// A document whose own text is unchanged is still stale when its latest
	// round read another document that changed since (trial problem #106):
	// a build recipe's final.md review reads domain.md and the wireframe.
	for doc := range verified {
		if slices.Contains(dirty, doc) {
			continue
		}
		if len(citedDocsChanged(root, recipeID, entries, doc)) > 0 {
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
