package state

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Evidence cache — memoises framework/platform claim research across
// verification rounds.
//
// `jig-evidence-policy.md` requires every CRITICAL/MAJOR claim about
// framework internals to be checked against official sources, in order:
// Context7 MCP → WebFetch on official domains → site-filtered WebSearch.
// Each round budgets up to 5 such lookups. Nothing cached them, so a
// long-running loop re-researched the SAME claim on every round — and
// network round trips are the slowest part of a verification iteration.
//
// The cache lives under .jig/local/ (never committed, like verify
// snapshots and tool traces) because it is a machine-local performance
// artifact, not project truth.

// Evidence verdicts, mirroring the reclassification table in
// jig-evidence-policy.md.
const (
	EvidenceContradicts = "contradicts" // official source contradicts the claim → critical
	EvidenceConfirms    = "confirms"    // official source confirms → finding removed
	EvidenceSilent      = "silent"      // official source says nothing → defensive classification
	EvidenceUnofficial  = "unofficial"  // only non-official sources found → downgrade to minor
	EvidenceUnavailable = "unavailable" // lookup failed (outage, rate limit, auth)
)

// EvidenceEntry is one memoised lookup.
type EvidenceEntry struct {
	Key       string   `json:"key"`
	Library   string   `json:"library"`
	Topic     string   `json:"topic"`
	Claim     string   `json:"claim"`
	Verdict   string   `json:"verdict"`
	Gathered  string   `json:"gathered"` // the Gathered: line to reproduce verbatim
	URLs      []string `json:"urls,omitempty"`
	Summary   string   `json:"summary,omitempty"`
	FetchedAt string   `json:"fetched_at"`
}

// The cache is an append-only JSONL log folded on read (last write per
// key wins), not a rewritten JSON object.
//
// The spec loop runs /jig-verify and /jig-audit concurrently, and
// both gather evidence. A read-modify-write of a single JSON document
// loses one of two concurrent puts: each process reads the same map,
// adds its own key, and the second rename overwrites the first. Appends
// cannot lose an entry, and this matches how every other multi-writer
// store in the repo works (changelog, verify-log, findings).
func evidenceCachePath(root string) string {
	return filepath.Join(LocalPath(root), "evidence-cache.jsonl")
}

// EvidenceKey derives the cache key for a claim lookup. Library and
// topic are normalised so casing and spacing churn does not miss.
func EvidenceKey(library, topic, claim string) string {
	norm := func(s string) string {
		return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
	}
	sum := sha256.Sum256([]byte(norm(library) + "|" + norm(topic) + "|" + norm(claim)))
	return hex.EncodeToString(sum[:])[:16]
}

// loadEvidenceCache folds the append-only log into the live entry set.
// Malformed lines are skipped: a damaged cache is a latency artifact,
// not project truth, so it must never fail a verification round.
func loadEvidenceCache(root string) (map[string]*EvidenceEntry, error) {
	entries := map[string]*EvidenceEntry{}
	f, err := os.Open(evidenceCachePath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return entries, nil
		}
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e EvidenceEntry
		if json.Unmarshal([]byte(line), &e) != nil || e.Key == "" {
			continue
		}
		entries[e.Key] = &e // later lines win
	}
	return entries, sc.Err()
}

// rewriteEvidenceCache replaces the log with exactly the given entries.
// Only prune uses it — it is a whole-file rewrite and therefore races
// with concurrent appends, which is acceptable for an explicit
// maintenance command but never on the put path.
func rewriteEvidenceCache(root string, entries map[string]*EvidenceEntry) error {
	path := evidenceCachePath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	for _, k := range keys {
		data, err := json.Marshal(entries[k])
		if err != nil {
			return err
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// EvidenceExpired reports whether an entry is older than ttlDays.
// ttlDays <= 0 means entries never expire.
//
// Unavailable results get a deliberately short life: a Context7 outage
// or rate limit must not pin a claim to "evidence unavailable" for a
// month. One hour is long enough to spare a stuck loop from hammering a
// failing endpoint every round, short enough that the next session
// retries for real.
func EvidenceExpired(e *EvidenceEntry, ttlDays int, now time.Time) bool {
	t, err := time.Parse(time.RFC3339, e.FetchedAt)
	if err != nil {
		return true
	}
	if e.Verdict == EvidenceUnavailable {
		return now.Sub(t) > time.Hour
	}
	if ttlDays <= 0 {
		return false
	}
	return now.Sub(t) > time.Duration(ttlDays)*24*time.Hour
}

// GetEvidence returns a live cache entry, or nil on miss or expiry.
func GetEvidence(root, library, topic, claim string, ttlDays int) (*EvidenceEntry, error) {
	entries, err := loadEvidenceCache(root)
	if err != nil {
		return nil, err
	}
	e, ok := entries[EvidenceKey(library, topic, claim)]
	if !ok || EvidenceExpired(e, ttlDays, time.Now().UTC()) {
		return nil, nil
	}
	return e, nil
}

// PutEvidence records a lookup result. Appends rather than rewriting, so
// two concurrent verify/audit forks cannot lose each other's entry; the
// fold on read takes the last line for a key.
func PutEvidence(root string, e *EvidenceEntry) error {
	e.Key = EvidenceKey(e.Library, e.Topic, e.Claim)
	if e.FetchedAt == "" {
		e.FetchedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return AppendJSONL(evidenceCachePath(root), e)
}

// PruneEvidence drops expired entries and returns how many were removed.
// This compacts the append-only log, so run it when the loop is idle.
func PruneEvidence(root string, ttlDays int) (int, error) {
	entries, err := loadEvidenceCache(root)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	removed := 0
	for k, e := range entries {
		if EvidenceExpired(e, ttlDays, now) {
			delete(entries, k)
			removed++
		}
	}
	if removed == 0 {
		return 0, nil
	}
	return removed, rewriteEvidenceCache(root, entries)
}

// ListEvidence returns cached entries sorted newest first.
func ListEvidence(root string) ([]*EvidenceEntry, error) {
	entries, err := loadEvidenceCache(root)
	if err != nil {
		return nil, err
	}
	out := make([]*EvidenceEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FetchedAt > out[j].FetchedAt })
	return out, nil
}
