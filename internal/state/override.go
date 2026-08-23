package state

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Gate overrides — the bypass that already existed, made explicit.
//
// A hard gate the operator disagrees with does not stop them; it stops
// the recorded path and leaves the unrecorded one open. In a measured
// recipe the completion gate refused `<bts>DONE</bts>` for fourteen
// rounds, and the recipe finalized anyway: final.md was written directly
// from draft.md seventeen hours after a verify round marked `failed`,
// with seven majors open. The two overrides behind that decision were
// real, considered, and written down — as four thousand characters of
// prose in changelog.jsonl and decisions.jsonl. Nothing machine-readable
// carried them, so `bts recipe status`, `bts doctor` and `bts stats` all
// went on reporting an ordinary finalized recipe, and the README's
// promise that "specs can't finalize without passing verification" was
// true of the gate and false of the artifact.
//
// So the bypass becomes a first-class, narrow, durable record:
//
//   - It names ONE gate. There is no blanket override.
//   - It enumerates the finding IDs it excuses, so an override cannot
//     silently widen to defects nobody weighed.
//   - It pins the revision it was granted on. Edit the document and the
//     override goes stale, because the judgement was about that text.
//   - It lives in tracked state and surfaces in status, doctor and stats
//     for the life of the recipe.

// OverrideRecord is one operator decision to proceed past a hard gate.
type OverrideRecord struct {
	Gate      string   `json:"gate"`               // gate_registry ID, e.g. "replicated_clean_pass"
	Doc       string   `json:"doc,omitempty"`      // basename of the document it applies to
	DocHash   string   `json:"doc_hash,omitempty"` // revision it was granted on
	Findings  []string `json:"findings,omitempty"` // finding IDs being excused
	Reason    string   `json:"reason"`             // why, in the operator's words
	Iteration int      `json:"iteration,omitempty"`
	Timestamp string   `json:"timestamp"`
	// Revoked marks a superseding record that cancels earlier overrides
	// of the same gate+doc. Overrides are append-only; this is how one
	// is taken back.
	Revoked bool `json:"revoked,omitempty"`
}

// OverridesPath returns the override ledger path for a recipe. It lives
// under specs/ rather than local/ because an override is part of the
// spec's provenance — it must travel with the branch, not sit in a
// gitignored directory the next clone never sees.
func OverridesPath(root, recipeID string) string {
	return filepath.Join(RecipeDir(root, recipeID), "overrides.jsonl")
}

// AppendOverride records one override, stamping the timestamp.
func AppendOverride(root, recipeID string, rec *OverrideRecord) error {
	if rec.Gate == "" {
		return fmt.Errorf("an override must name the gate it bypasses")
	}
	if strings.TrimSpace(rec.Reason) == "" {
		return fmt.Errorf("an override must carry a reason")
	}
	if rec.Timestamp == "" {
		rec.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	path := OverridesPath(root, recipeID)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	return AppendJSONL(path, rec)
}

// ReadOverrides returns every override record in file order. A missing
// ledger returns nil, nil. Malformed lines are skipped.
func ReadOverrides(root, recipeID string) ([]OverrideRecord, error) {
	f, err := os.Open(OverridesPath(root, recipeID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var out []OverrideRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r OverrideRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			continue
		}
		out = append(out, r)
	}
	return out, sc.Err()
}

// OverrideStatus is the result of asking whether a gate is overridden.
type OverrideStatus struct {
	Active  bool
	Stale   bool           // an override exists but was granted on another revision
	Record  OverrideRecord // the governing record, when one was found
	Granted string         // hash the override was granted on, when stale
}

// ActiveOverride reports whether gate is currently overridden for doc at
// the given revision.
//
// Later records win, so a `--revoke` cancels an earlier grant. An
// override granted on a different revision is reported as Stale rather
// than Active: the operator weighed a specific text, and an edit since
// then is exactly the case where that judgement has to be made again.
//
// A pinned override against an UNKNOWN revision is stale too. The
// earlier form asked `r.DocHash != "" && docHash != ""`, which read a
// missing round hash as "no conflict" and let the override through. The
// gate that fires precisely when the round recorded no doc_hash is
// revision_recorded_before_final, so the one case the guard had to
// handle was the one it failed open on. An override is evidence about a
// specific text; without knowing which text is in front of us, it is not
// evidence about anything.
func ActiveOverride(records []OverrideRecord, gate, docBase, docHash string) OverrideStatus {
	var st OverrideStatus
	for i := range records {
		r := records[i]
		if r.Gate != gate {
			continue
		}
		if r.Doc != "" && docBase != "" && r.Doc != filepath.Base(docBase) {
			continue
		}
		if r.Revoked {
			st = OverrideStatus{} // a revocation clears what came before
			continue
		}
		if r.DocHash != "" && r.DocHash != docHash {
			st = OverrideStatus{Stale: true, Record: r, Granted: r.DocHash}
			continue
		}
		st = OverrideStatus{Active: true, Record: r}
	}
	return st
}

// LiveOverrides folds the ledger to the set of overrides currently in
// force: newest record per gate+doc, revocations removed, and — when
// `current` is supplied — records pinned to a revision the document no
// longer has dropped as stale.
//
// `current` maps a document basename to its content hash right now. Pass
// nil only where the documents genuinely cannot be read; a nil map skips
// the staleness filter, which is the behaviour this function used to
// have unconditionally. That was a divergence with teeth: the stop hook
// correctly refused a stale override and re-blocked, while `bts recipe
// status` and `bts doctor` went on printing "override in force" and
// `bts stats` went on excluding the recipe from the correlation it added
// specifically to avoid over-claiming. Every surface disagreed with the
// gate, in the direction that flatters the override.
//
// Revocation matching mirrors ActiveOverride: a revocation naming no
// document takes back every override of that gate. Keying revocations
// the same way as grants meant `revoke --gate G` (no --doc) filed itself
// under a key nothing had ever granted.
func LiveOverrides(records []OverrideRecord, current map[string]string) []OverrideRecord {
	byKey := map[string]OverrideRecord{}
	var order []string
	for _, r := range records {
		if r.Revoked {
			for _, key := range order {
				gate, doc, _ := strings.Cut(key, "\x00")
				if gate == r.Gate && (r.Doc == "" || r.Doc == doc) {
					delete(byKey, key)
				}
			}
			continue
		}
		key := r.Gate + "\x00" + r.Doc
		if !slices.Contains(order, key) {
			order = append(order, key)
		}
		byKey[key] = r
	}
	var out []OverrideRecord
	for _, key := range order {
		r, ok := byKey[key]
		if !ok {
			continue
		}
		if current != nil && r.DocHash != "" && current[r.Doc] != r.DocHash {
			continue // granted on text the document no longer carries
		}
		out = append(out, r)
	}
	return out
}

// CurrentDocHashes hashes every document named by the given records, so
// LiveOverrides can drop the ones granted on a revision that is gone.
// Documents that cannot be read are omitted, which makes their overrides
// stale — the same answer ActiveOverride gives when it cannot confirm
// the text.
func CurrentDocHashes(root, recipeID string, records []OverrideRecord) map[string]string {
	out := map[string]string{}
	for _, r := range records {
		if r.Doc == "" || out[r.Doc] != "" {
			continue
		}
		if h, ok, err := FileContentHash(filepath.Join(RecipeDir(root, recipeID), r.Doc)); err == nil && ok {
			out[r.Doc] = h
		}
	}
	return out
}

// OverrideSummary renders live overrides for a one-line status display.
func OverrideSummary(root, recipeID string, records []OverrideRecord) string {
	live := LiveOverrides(records, CurrentDocHashes(root, recipeID, records))
	if len(live) == 0 {
		return ""
	}
	parts := make([]string, 0, len(live))
	for _, r := range live {
		p := r.Gate
		if n := len(r.Findings); n > 0 {
			p += fmt.Sprintf("(%d findings)", n)
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, ", ")
}
