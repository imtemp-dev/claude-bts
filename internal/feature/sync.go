package feature

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/imtemp-dev/claude-bts/pkg/version"
)

// terminalPhase is the phase in which an item's recipe is done.
func terminalPhase(recipeType string) string {
	switch recipeType {
	case "analyze", "design":
		return "finalize"
	}
	return "complete"
}

// copyDir copies the regular files under src to dst.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
}

// contentHash hashes the files under dir, by relative path and content.
func contentHash(dir string) (string, error) {
	var files []string
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	h := sha256.New()
	for _, p := range files {
		rel, _ := filepath.Rel(dir, p)
		data, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", filepath.ToSlash(rel), len(data))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// lastPhaseVia returns the source of the change into phase, from the
// recipe's changelog.
func lastPhaseVia(root, recipeID, phase string) string {
	entries, err := state.ReadChangelog(root, recipeID)
	if err != nil {
		return ""
	}
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Action == "phase" && strings.HasSuffix(e.Result, "→ "+phase) {
			return e.Via
		}
	}
	return ""
}

// attemptDir is the store directory of the recipe's attempt, and whether it
// exists.  When the recorded feature directory is gone (the store moved),
// the feature is looked up by id.
func attemptDir(r *state.RecipeState) (string, bool) {
	rel := filepath.Join(ItemsDir, r.FeatureItem, fmt.Sprintf("a%d", r.FeatureAttempt))
	ad := filepath.Join(r.FeatureDir, rel)
	if fileExists(ad) {
		return ad, true
	}
	if dir, err := Find(r.Feature); err == nil && fileExists(filepath.Join(dir, rel)) {
		return filepath.Join(dir, rel), true
	}
	return ad, false
}

// isTerminal reports whether a recipe's phase ends its attempt.
func isTerminal(r *state.RecipeState) bool {
	return r.Phase == terminalPhase(r.Type) || r.Phase == "cancelled"
}

// frozen reports whether the attempt's snapshot recorded the recipe ending
// in the phase it is still in.  A finished attempt is a record: a later
// write to the recipe (a fix-forward's "amended" entry) must not replace its
// head, branch or metrics with whatever worktree it is synced from.
func frozen(old *Manifest, r *state.RecipeState) bool {
	return old != nil && old.Outcome != "" && old.Phase == r.Phase
}

// staleAge is how old a .staging-* or .prev-* directory must be before a
// sync removes it as left by a killed sync.
const staleAge = 10 * time.Minute

// recoverSnapshot puts back a snapshot a sync set aside and then died, and
// removes old staging and set-aside directories.
func recoverSnapshot(ad string) {
	snap := filepath.Join(ad, SnapshotDir)
	if !fileExists(snap) {
		if cur := currentSnapshot(ad); cur != "" && cur != snap {
			_ = os.Rename(cur, snap)
		}
	}
	for _, pat := range []string{".staging-*", ".prev-*"} {
		olds, _ := filepath.Glob(filepath.Join(ad, pat))
		for _, p := range olds {
			if fi, err := os.Stat(p); err == nil && time.Since(fi.ModTime()) > staleAge {
				_ = os.RemoveAll(p)
			}
		}
	}
}

// Sync copies a feature-bound recipe into its attempt in the store: the
// whole recipe directory and its metrics, staged and swapped in as
// snapshot/, with manifest.json.  It is idempotent, does nothing for a
// recipe bound to no feature, and leaves a finished attempt's snapshot as
// it was.  It runs on every phase change, at session start for a snapshot
// that is behind, and by hand (bts feature harvest).
func Sync(root, recipeID string) error {
	r, err := state.LoadRecipeState(root, recipeID)
	if err != nil {
		return err
	}
	if r.Feature == "" || r.FeatureDir == "" {
		return nil
	}
	ad, ok := attemptDir(r)
	if !ok {
		if isTerminal(r) {
			return nil // nothing left to report to
		}
		return fmt.Errorf("attempt a%d of item %s is not in the store (%s): the feature was deleted or moved", r.FeatureAttempt, r.FeatureItem, ad)
	}
	var c Claim
	if err := state.ReadJSON(filepath.Join(ad, ClaimFile), &c); err != nil {
		return fmt.Errorf("attempt %s has no claim: %w", ad, err)
	}
	if c.RecipeID != recipeID {
		return fmt.Errorf("attempt %s belongs to recipe %s, not %s", ad, c.RecipeID, recipeID)
	}
	if c.Schema > Schema {
		return fmt.Errorf("attempt %s was written by a newer bts (schema %d); upgrade bts", ad, c.Schema)
	}
	recoverSnapshot(ad)
	var old *Manifest
	var om Manifest
	if err := state.ReadJSON(filepath.Join(ad, SnapshotDir, ManifestFile), &om); err == nil {
		old = &om
		if old.Schema > Schema {
			return fmt.Errorf("attempt %s was synced by a newer bts (schema %d); upgrade bts", ad, old.Schema)
		}
	}
	if frozen(old, r) {
		return nil
	}

	stage := filepath.Join(ad, fmt.Sprintf(".staging-%d-%d", os.Getpid(), time.Now().UnixNano()))
	defer os.RemoveAll(stage)
	if err := copyDir(state.RecipeDir(root, recipeID), filepath.Join(stage, "recipe")); err != nil {
		return err
	}
	if m := filepath.Join(state.LocalPath(root), "recipes", recipeID, "metrics.jsonl"); fileExists(m) {
		if err := copyFile(m, filepath.Join(stage, "metrics.jsonl")); err != nil {
			return err
		}
	}
	hash, err := contentHash(filepath.Join(stage, "recipe"))
	if err != nil {
		return err
	}
	m := &Manifest{
		Schema: Schema, RecipeID: recipeID, Phase: r.Phase, Type: r.Type, Mode: r.Mode,
		Branch: branch(root), HeadSHA: headSHA(root), Dirty: dirty(root),
		BtsVersion: version.GetVersion(), ContentHash: hash, SyncedAt: now(),
	}
	if isTerminal(r) {
		m.CompletedAt = m.SyncedAt
	}
	switch r.Phase {
	case terminalPhase(r.Type):
		m.Outcome = "done"
		switch lastPhaseVia(root, recipeID, r.Phase) {
		case state.PhaseViaGate:
			m.CompletedVia = "gate"
		case state.PhaseViaCLI:
			m.CompletedVia = "forced"
		default:
			m.CompletedVia = "unknown"
		}
	case "cancelled":
		m.Outcome = "cancelled"
	}
	if recs, err := state.ReadOverrides(root, recipeID); err == nil && len(recs) > 0 {
		m.ActiveOverrides = len(state.LiveOverrides(recs, state.CurrentDocHashes(root, recipeID, recs)))
	}
	if err := state.WriteJSON(filepath.Join(stage, ManifestFile), m); err != nil {
		return err
	}

	snap := filepath.Join(ad, SnapshotDir)
	prev := filepath.Join(ad, fmt.Sprintf(".prev-%d", time.Now().UnixNano()))
	if fileExists(snap) {
		if err := os.Rename(snap, prev); err != nil {
			return err
		}
	}
	if err := os.Rename(stage, snap); err != nil {
		_ = os.Rename(prev, snap)
		return err
	}
	_ = os.RemoveAll(prev)
	return nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

// Behind reports whether a feature-bound recipe's snapshot in the store is
// missing or older than the recipe in the worktree.  A finished attempt is
// never behind, and neither is a finished recipe whose attempt is gone.
func Behind(root string, r *state.RecipeState) bool {
	if r.Feature == "" || r.FeatureDir == "" {
		return false
	}
	ad, ok := attemptDir(r)
	if !ok {
		return !isTerminal(r)
	}
	snap := currentSnapshot(ad)
	if snap == "" {
		return true
	}
	var m Manifest
	if err := state.ReadJSON(filepath.Join(snap, ManifestFile), &m); err != nil {
		return true
	}
	if frozen(&m, r) {
		return false
	}
	h, err := contentHash(state.RecipeDir(root, r.ID))
	return err != nil || h != m.ContentHash
}

// SyncPending syncs every feature-bound recipe in the worktree whose
// snapshot is behind.  It returns the recipes it synced and the first error.
func SyncPending(root string) ([]string, error) {
	recipes, err := state.ListRecipes(root)
	if err != nil {
		return nil, err
	}
	var synced []string
	var first error
	for _, r := range recipes {
		if !Behind(root, r) {
			continue
		}
		if err := Sync(root, r.ID); err != nil {
			if first == nil {
				first = fmt.Errorf("%s: %w", r.ID, err)
			}
			continue
		}
		synced = append(synced, r.ID)
	}
	return synced, first
}

// OnPhaseChange is the phase observer that keeps the store in step.  A
// failure is a warning: the next session start retries it.
func OnPhaseChange(root, recipeID, from, to string) {
	r, err := state.LoadRecipeState(root, recipeID)
	if err != nil || r == nil || r.Feature == "" {
		return
	}
	if err := Sync(root, recipeID); err != nil {
		fmt.Fprintf(os.Stderr, "[bts] warning: feature store not updated for %s (%s → %s): %v; the next session start retries it, or run bts feature harvest\n", recipeID, from, to, err)
	}
}
