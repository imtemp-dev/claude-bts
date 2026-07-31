package state

import (
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
// `bts-evidence-policy.md` requires every CRITICAL/MAJOR claim about
// framework internals to be checked against official sources, in order:
// Context7 MCP → WebFetch on official domains → site-filtered WebSearch.
// Each round budgets up to 5 such lookups. Nothing cached them, so a
// long-running loop re-researched the SAME claim on every round — and
// network round trips are the slowest part of a verification iteration.
//
// The cache lives under .bts/local/ (never committed, like verify
// snapshots and tool traces) because it is a machine-local performance
// artifact, not project truth.

// Evidence verdicts, mirroring the reclassification table in
// bts-evidence-policy.md.
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

type evidenceCacheFile struct {
	Version int                       `json:"version"`
	Entries map[string]*EvidenceEntry `json:"entries"`
}

func evidenceCachePath(root string) string {
	return filepath.Join(LocalPath(root), "evidence-cache.json")
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

func loadEvidenceCache(root string) (*evidenceCacheFile, error) {
	c := &evidenceCacheFile{Version: 1, Entries: map[string]*EvidenceEntry{}}
	data, err := os.ReadFile(evidenceCachePath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, c); err != nil {
		// A corrupt cache is a performance artifact, not project truth —
		// start over rather than failing the verification round.
		return &evidenceCacheFile{Version: 1, Entries: map[string]*EvidenceEntry{}}, nil
	}
	if c.Entries == nil {
		c.Entries = map[string]*EvidenceEntry{}
	}
	return c, nil
}

func saveEvidenceCache(root string, c *evidenceCacheFile) error {
	path := evidenceCachePath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
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
	c, err := loadEvidenceCache(root)
	if err != nil {
		return nil, err
	}
	e, ok := c.Entries[EvidenceKey(library, topic, claim)]
	if !ok || EvidenceExpired(e, ttlDays, time.Now().UTC()) {
		return nil, nil
	}
	return e, nil
}

// PutEvidence records a lookup result, overwriting any prior entry.
func PutEvidence(root string, e *EvidenceEntry) error {
	c, err := loadEvidenceCache(root)
	if err != nil {
		return err
	}
	e.Key = EvidenceKey(e.Library, e.Topic, e.Claim)
	if e.FetchedAt == "" {
		e.FetchedAt = time.Now().UTC().Format(time.RFC3339)
	}
	c.Entries[e.Key] = e
	return saveEvidenceCache(root, c)
}

// PruneEvidence drops expired entries and returns how many were removed.
func PruneEvidence(root string, ttlDays int) (int, error) {
	c, err := loadEvidenceCache(root)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	removed := 0
	for k, e := range c.Entries {
		if EvidenceExpired(e, ttlDays, now) {
			delete(c.Entries, k)
			removed++
		}
	}
	if removed == 0 {
		return 0, nil
	}
	return removed, saveEvidenceCache(root, c)
}

// ListEvidence returns cached entries sorted newest first.
func ListEvidence(root string) ([]*EvidenceEntry, error) {
	c, err := loadEvidenceCache(root)
	if err != nil {
		return nil, err
	}
	out := make([]*EvidenceEntry, 0, len(c.Entries))
	for _, e := range c.Entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FetchedAt > out[j].FetchedAt })
	return out, nil
}
