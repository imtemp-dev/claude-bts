package feature

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Attempt-directory names.
const (
	ClaimFile    = "claim.json"
	SnapshotDir  = "snapshot"
	ManifestFile = "manifest.json"
)

// Claim is claim.json, written once when a child takes an item.
type Claim struct {
	Schema       int    `json:"schema"`
	Feature      string `json:"feature"`
	FeatureDir   string `json:"feature_dir"`
	Item         string `json:"item"`
	Attempt      int    `json:"attempt"`
	Kind         string `json:"kind"` // "build" or "fix-forward"
	Amends       string `json:"amends,omitempty"`
	RecipeID     string `json:"recipe_id"`
	RecipeType   string `json:"recipe_type"`
	Confirmation int    `json:"confirmation"`
	Worktree     string `json:"worktree"`
	StartSHA     string `json:"start_sha,omitempty"`
	Branch       string `json:"branch,omitempty"`
	MulticaIssue string `json:"multica_issue,omitempty"`
	BtsVersion   string `json:"bts_version"`
	ClaimedAt    string `json:"claimed_at"`
}

// Manifest is snapshot/manifest.json, rewritten on every sync.
type Manifest struct {
	Schema          int    `json:"schema"`
	RecipeID        string `json:"recipe_id"`
	Phase           string `json:"phase"`
	Type            string `json:"type"`
	Mode            string `json:"mode,omitempty"`
	Outcome         string `json:"outcome,omitempty"`       // "done", "cancelled", or "" while open
	CompletedVia    string `json:"completed_via,omitempty"` // "gate", "forced", "unknown"
	ActiveOverrides int    `json:"active_overrides,omitempty"`
	Branch          string `json:"branch,omitempty"`
	HeadSHA         string `json:"head_sha,omitempty"`
	Dirty           bool   `json:"dirty,omitempty"`
	BtsVersion      string `json:"bts_version"`
	ContentHash     string `json:"content_hash"`
	SyncedAt        string `json:"synced_at"`
	CompletedAt     string `json:"completed_at,omitempty"` // when the attempt ended; its snapshot is frozen from then
}

// Attempt is one try at an item, as the store records it.
type Attempt struct {
	N        int
	Dir      string
	Claim    *Claim
	Manifest *Manifest // nil until the first sync
	Released *Release
}

// Open reports whether the attempt can still finish.
func (a *Attempt) Open() bool {
	return a.Released == nil && (a.Manifest == nil || a.Manifest.Outcome == "")
}

// Done reports whether the attempt finished its recipe.
func (a *Attempt) Done() bool {
	return a.Released == nil && a.Manifest != nil && a.Manifest.Outcome == "done"
}

// claimGrace is how long an attempt directory may lack claim.json before it
// is taken for a claim that died half way and ignored.
const claimGrace = 10 * time.Minute

// Attempts lists an item's attempts, oldest first.
func Attempts(dir string, it *Item) []*Attempt {
	base := filepath.Join(dir, ItemsDir, it.ID)
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	released := map[int]*Release{}
	for i := range it.Released {
		released[it.Released[i].Attempt] = &it.Released[i]
	}
	var out []*Attempt
	for _, e := range entries {
		var n int
		if !e.IsDir() || len(e.Name()) < 2 || e.Name()[0] != 'a' {
			continue
		}
		if _, err := fmt.Sscanf(e.Name(), "a%d", &n); err != nil || n < 1 {
			continue
		}
		a := &Attempt{N: n, Dir: filepath.Join(base, e.Name()), Released: released[n]}
		var c Claim
		if err := state.ReadJSON(filepath.Join(a.Dir, ClaimFile), &c); err != nil {
			if fi, err := os.Stat(a.Dir); err == nil && time.Since(fi.ModTime()) < claimGrace {
				// Being claimed right now: it holds the item.
				a.Claim = &Claim{Item: it.ID, Attempt: n}
				out = append(out, a)
			}
			continue
		}
		a.Claim = &c
		var m Manifest
		if snap := currentSnapshot(a.Dir); snap != "" {
			if err := state.ReadJSON(filepath.Join(snap, ManifestFile), &m); err == nil {
				a.Manifest = &m
			}
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].N < out[j].N })
	return out
}

// currentSnapshot is the attempt's snapshot directory: snapshot/, or, when a
// sync died between its two renames, the newest .prev-* it set aside.
func currentSnapshot(ad string) string {
	snap := filepath.Join(ad, SnapshotDir)
	if fileExists(filepath.Join(snap, ManifestFile)) {
		return snap
	}
	prevs, _ := filepath.Glob(filepath.Join(ad, ".prev-*"))
	sort.Strings(prevs)
	for i := len(prevs) - 1; i >= 0; i-- {
		if fileExists(filepath.Join(prevs[i], ManifestFile)) {
			return prevs[i]
		}
	}
	return ""
}

// maxAttempt is the highest a<N> under an item, whatever state it is in, so
// a new attempt never lands on an old number.
func maxAttempt(dir, item string) int {
	entries, _ := os.ReadDir(filepath.Join(dir, ItemsDir, item))
	max := 0
	for _, e := range entries {
		var n int
		if _, err := fmt.Sscanf(e.Name(), "a%d", &n); err == nil && n > max {
			max = n
		}
	}
	return max
}

// Item states Derive reports.
const (
	StatePlanned   = "planned"   // no attempt yet
	StateActive    = "active"    // an attempt is open
	StateDone      = "done"      // an attempt finished its recipe
	StateClaimable = "claimable" // the last attempt was cancelled or released
	StateSkipped   = "skipped"
	StateDeferred  = "deferred"
)

// ItemStatus is an item's derived state.
type ItemStatus struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Type        string      `json:"type"`
	Express     bool        `json:"express,omitempty"`
	DependsOn   []string    `json:"depends_on,omitempty"`
	Stage       int         `json:"stage"`
	InConfirmed bool        `json:"in_confirmed_plan"`
	State       string      `json:"state"`
	Note        string      `json:"note,omitempty"`
	Attempts    int         `json:"attempts"`
	Latest      *AttemptRef `json:"latest,omitempty"`
	DoneAttempt *AttemptRef `json:"done_attempt,omitempty"`
	Removed     bool        `json:"removed_pending_confirmation,omitempty"`
	Merged      bool        `json:"merged,omitempty"`

	latest, done *Attempt
	doneAll      []*Attempt // every done attempt, oldest first
}

// AttemptRef is an attempt as status output shows it.
type AttemptRef struct {
	N            int    `json:"n"`
	Kind         string `json:"kind,omitempty"`
	RecipeID     string `json:"recipe_id,omitempty"`
	Worktree     string `json:"worktree,omitempty"`
	MulticaIssue string `json:"multica_issue,omitempty"`
	Phase        string `json:"phase,omitempty"`
	Outcome      string `json:"outcome,omitempty"`
	CompletedVia string `json:"completed_via,omitempty"`
	HeadSHA      string `json:"head_sha,omitempty"`
	SyncedAt     string `json:"synced_at,omitempty"`
	Released     bool   `json:"released,omitempty"`
	PRURL        string `json:"pr_url,omitempty"`
	PRBranch     string `json:"pr_branch,omitempty"`
	PRBase       string `json:"pr_base,omitempty"`
}

func ref(a *Attempt) *AttemptRef {
	if a == nil {
		return nil
	}
	r := &AttemptRef{N: a.N, Released: a.Released != nil}
	if a.Claim != nil {
		r.Kind, r.RecipeID, r.Worktree, r.MulticaIssue = a.Claim.Kind, a.Claim.RecipeID, a.Claim.Worktree, a.Claim.MulticaIssue
	}
	if a.Manifest != nil {
		r.Phase, r.Outcome, r.CompletedVia, r.HeadSHA, r.SyncedAt = a.Manifest.Phase, a.Manifest.Outcome, a.Manifest.CompletedVia, a.Manifest.HeadSHA, a.Manifest.SyncedAt
	}
	if p := readPR(a.Dir); p != nil {
		r.PRURL, r.PRBranch, r.PRBase = p.URL, p.Branch, p.Base
	}
	return r
}

// Feature states Derive reports.
const (
	FeatureDraft     = "draft"
	FeatureConfirmed = "confirmed"
	FeatureActive    = "active"
	FeatureReady     = "ready_for_review"
)

// Status is a feature's derived state.
type Status struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	Ticket       string        `json:"ticket,omitempty"`
	Dir          string        `json:"dir"`
	Repo         Repo          `json:"repo"`
	BaseBranch   string        `json:"base_branch"`
	State        string        `json:"state"`
	Confirmation int           `json:"confirmation"`
	Pending      []string      `json:"pending,omitempty"`
	Items        []*ItemStatus `json:"items"`
	Rev          int           `json:"rev"`
	Multica      *MulticaLink  `json:"multica,omitempty"`
}

// Derive computes the feature's and every item's state from feature.json and
// the attempts in the store.
func Derive(dir string, f *Feature) (*Status, error) {
	n, confirmed, err := Confirmed(dir, f)
	if err != nil {
		return nil, err
	}
	inConfirmed := map[string]bool{}
	for _, p := range confirmed {
		inConfirmed[p.ID] = true
	}
	st, err := stages(f.Plan())
	if err != nil {
		st = map[string]int{}
	}
	s := &Status{
		ID: f.ID, Title: f.Title, Ticket: f.Ticket, Dir: dir, Repo: f.Repo, BaseBranch: f.BaseBranch,
		Confirmation: n, Pending: Pending(dir, f), Rev: f.Rev, Multica: f.Multica,
	}
	// The working items, then any confirmed item the working plan dropped:
	// it stays in force until the user confirms the removal.
	items := append([]Item(nil), f.Items...)
	removed := map[string]bool{}
	for _, p := range confirmed {
		if f.Item(p.ID) == nil {
			items = append(items, Item{ID: p.ID, Title: p.Title, Type: p.Type, Express: p.Express, DependsOn: p.DependsOn})
			removed[p.ID] = true
			if _, ok := st[p.ID]; !ok {
				st[p.ID] = 0
			}
		}
	}
	anyAttempt, allSettled := false, true
	for i := range items {
		it := &items[i]
		is := &ItemStatus{
			ID: it.ID, Title: it.Title, Type: it.Type, Express: it.Express, DependsOn: it.DependsOn,
			Stage: st[it.ID], InConfirmed: inConfirmed[it.ID], Removed: removed[it.ID],
		}
		atts := Attempts(dir, it)
		is.Attempts = len(atts)
		if len(atts) > 0 {
			anyAttempt = true
			is.latest = atts[len(atts)-1]
		}
		for _, a := range atts {
			if a.Done() {
				is.done = a
				is.doneAll = append(is.doneAll, a)
			}
		}
		is.Latest, is.DoneAttempt = ref(is.latest), ref(is.done)
		switch {
		case it.Decision != nil && it.Decision.Kind == "skip":
			is.State = StateSkipped
		case it.Decision != nil && it.Decision.Kind == "defer":
			is.State = StateDeferred
		case is.latest == nil:
			is.State = StatePlanned
		case is.latest.Open() && is.done == nil:
			is.State = StateActive
		case is.latest.Open():
			is.State = StateDone
			is.Note = fmt.Sprintf("fix-forward a%d open", is.latest.N)
		case is.done != nil:
			is.State = StateDone
		default:
			is.State = StateClaimable
		}
		if is.done != nil && is.done.Manifest.CompletedVia != "gate" {
			is.Note = strings.TrimSpace(is.Note + " completed via " + is.done.Manifest.CompletedVia)
		}
		if is.Removed {
			is.Note = strings.TrimSpace(is.Note + " removed from the plan; the removal awaits the user's confirmation")
		}
		if f.Multica != nil {
			if mi := f.Multica.Items[it.ID]; mi != nil && mi.Merged != nil {
				is.Merged = true
				is.Note = strings.TrimSpace(is.Note + " merged " + short(mi.Merged.SHA))
				if mi.Merged.Into != "" && mi.Merged.Into != f.BaseBranch {
					is.Note += " into " + mi.Merged.Into
				}
			}
		}
		if is.InConfirmed && is.State != StateDone && is.State != StateSkipped && is.State != StateDeferred {
			allSettled = false
		}
		s.Items = append(s.Items, is)
	}
	switch {
	case f.Closed != nil:
		s.State = f.Closed.Kind
	case n == 0:
		s.State = FeatureDraft
	case allSettled && len(confirmed) > 0:
		s.State = FeatureReady
	case anyAttempt:
		s.State = FeatureActive
	default:
		s.State = FeatureConfirmed
	}
	return s, nil
}

// item returns the status of item id, or nil.
func (s *Status) item(id string) *ItemStatus {
	for _, it := range s.Items {
		if it.ID == id {
			return it
		}
	}
	return nil
}

// depsSatisfied reports the dependencies of it that are not done or skipped.
func (s *Status) unmetDeps(deps []string) []string {
	var out []string
	for _, d := range deps {
		ds := s.item(d)
		if ds == nil || (ds.State != StateDone && ds.State != StateSkipped) {
			out = append(out, d)
		}
	}
	return out
}

// Next returns the confirmed items ready to start: planned or claimable,
// with every dependency done or skipped.  A draft or closed feature has
// none.
func Next(dir string, f *Feature) ([]*ItemStatus, *Status, error) {
	s, err := Derive(dir, f)
	if err != nil {
		return nil, nil, err
	}
	if f.Closed != nil || s.Confirmation == 0 {
		return nil, s, nil
	}
	_, plan, err := Confirmed(dir, f)
	if err != nil {
		return nil, s, err
	}
	var ready []*ItemStatus
	for _, p := range plan {
		is := s.item(p.ID)
		if is == nil || (is.State != StatePlanned && is.State != StateClaimable) {
			continue
		}
		if len(s.unmetDeps(p.DependsOn)) == 0 {
			ready = append(ready, is)
		}
	}
	return ready, s, nil
}
