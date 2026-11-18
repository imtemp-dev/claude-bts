// Package review records a recipe's review on an artifact page: the page
// the user reviews intent, scope and the spec on, the rounds of notes they
// send from it, the proposals Claude makes from those notes, and the
// versions of the plan documents the accepted proposals produce.
//
// bts cannot read the page's data: only the session's ArtifactData tool can.
// The session exports the page's collections with ArtifactData's out_dir,
// and `bts review import` reads that export.  What bts owns is the record:
// which note led to which proposal, which proposal to which version, and
// which version the user confirmed (see docs/research/review-pages-design.md).
package review

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// KitVersion is the review kit the templates ship.  A page records the kit
// it was built with, so a later kit change is visible on an old page.
const KitVersion = "1"

// Schema is the version of review.json and the round records.
const Schema = 1

// Points a page can carry, in the order a recipe reaches them.  spec is the
// document the implementation is built from (SpecDoc).
var Points = []string{"intent", "scope", "spec"}

// PlanDocs are the documents a review can change and confirm.  A recipe
// holds at most one of the two spec documents.
var PlanDocs = []string{"intent.md", "scope.md", "final.md", "fix-spec.md"}

// SpecDoc is the document a recipe of this type implements, the one the
// spec point reviews: fix-spec.md for a fix recipe, final.md otherwise.
func SpecDoc(recipeType string) string {
	if recipeType == "fix" {
		return "fix-spec.md"
	}
	return "final.md"
}

// Note dispositions.
const (
	Answered = "answered" // a question, answered on the pin and in the terminal
	Proposed = "proposed" // a change request, turned into proposals
	PageOnly = "page"     // the page's own wording changed, not the plan
	Declined = "declined" // not acted on, with the reason given
)

// Proposal statuses.
const (
	PStatusProposed = "proposed"
	PStatusAccepted = "accepted"
	PStatusRejected = "rejected"
	PStatusApplied  = "applied"
)

// Round kinds, as the page writes them.
const (
	KindReview  = "review"
	KindApply   = "apply"
	KindConfirm = "confirm"
)

// Doc is one plan document's version history on the page.
type Doc struct {
	Version int        `json:"version"`
	Hash    string     `json:"hash"`
	History []Revision `json:"history,omitempty"`
}

// Revision is one version change of a plan document.
type Revision struct {
	File      string   `json:"file"`
	Version   int      `json:"version"`
	From      string   `json:"from"`
	To        string   `json:"to"`
	Round     int      `json:"round,omitempty"`
	Proposals []string `json:"proposals,omitempty"`
	Note      string   `json:"note,omitempty"`
	At        string   `json:"at"`
}

// State is review.json.
type State struct {
	Schema    int             `json:"schema"`
	URL       string          `json:"url,omitempty"`
	Kit       string          `json:"kit"`
	Points    []string        `json:"points,omitempty"`
	Docs      map[string]*Doc `json:"docs"`
	LastRound int             `json:"last_round,omitempty"`
	Updated   string          `json:"updated"`
}

// Item is one note in a round.
type Item struct {
	Note        string   `json:"note"`
	Anchor      string   `json:"anchor"`
	Excerpt     string   `json:"excerpt,omitempty"`
	Text        string   `json:"text"`
	Kind        string   `json:"kind"`
	Decision    string   `json:"decision,omitempty"` // a Pick: the decision's key
	Value       string   `json:"value,omitempty"`    // a Pick: the option; a confirm: the fingerprint
	Doc         string   `json:"doc,omitempty"`      // a confirm: intent, scope, final or fix-spec
	Disposition string   `json:"disposition,omitempty"`
	Proposals   []string `json:"proposals,omitempty"`
	Why         string   `json:"why,omitempty"`
}

// Proposal is a change Claude proposes to a plan document from a note.
type Proposal struct {
	ID      string `json:"id"`
	Round   int    `json:"round"`
	Note    string `json:"note"`
	Target  string `json:"target"`
	Kind    string `json:"kind"`
	Summary string `json:"summary"`
	Before  string `json:"before,omitempty"`
	After   string `json:"after,omitempty"`
	Why     string `json:"why,omitempty"`
	Anchor  string `json:"anchor,omitempty"`
	ToLane  string `json:"to_lane,omitempty"`
	ToLabel string `json:"to_label,omitempty"`
	Status  string `json:"status"`
	Created string `json:"created"`
	Applied string `json:"applied,omitempty"`
}

// Round is one round's record, feedback/round-N.json.
type Round struct {
	Schema      int               `json:"schema"`
	Recipe      string            `json:"recipe"`
	Round       int               `json:"round"`
	Kind        string            `json:"kind"`
	Page        string            `json:"page,omitempty"`
	Submitted   string            `json:"submitted,omitempty"`
	Imported    string            `json:"imported"`
	Processed   string            `json:"processed,omitempty"`
	FilesBefore map[string]string `json:"files_before"`
	FilesAfter  map[string]string `json:"files_after,omitempty"`
	Items       []Item            `json:"items,omitempty"`
	Proposals   []Proposal        `json:"proposals,omitempty"`
	Accepted    []string          `json:"accepted,omitempty"`
	Rejected    []string          `json:"rejected,omitempty"`
	Revisions   []Revision        `json:"revisions,omitempty"`
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

// Fingerprint is the version id the page shows and Confirm carries: the
// first 12 hex digits of the SHA-256 of the document with its status line
// blanked, so confirming (which flips the status line) does not change it.
func Fingerprint(content string) string {
	norm := content
	if state.DocStatus(content) != "" {
		norm = state.SetDocStatus(content, "X")
	}
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:])[:12]
}

// FileFingerprint fingerprints a plan document of the recipe; "" when the
// file does not exist.
func FileFingerprint(root, recipeID, doc string) (string, error) {
	data, err := os.ReadFile(filepath.Join(state.RecipeDir(root, recipeID), doc))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return Fingerprint(string(data)), nil
}

func statePath(root, recipeID string) string {
	return filepath.Join(state.RecipeDir(root, recipeID), "review.json")
}

// RoundPath is where a round's record lives.
func RoundPath(root, recipeID string, n int) string {
	return filepath.Join(state.RecipeDir(root, recipeID), "feedback", fmt.Sprintf("round-%d.json", n))
}

// UploadDir is where bts writes the page documents the session uploads
// with ArtifactData (file_path), one JSON object per document.
func UploadDir(root, recipeID string) string {
	return filepath.Join(state.LocalPath(root), "recipes", recipeID, "review", "upload")
}

// ErrNoPage means the recipe has no review page.
var ErrNoPage = errors.New("no review page: run `bts review page` first")

// Load reads review.json; ErrNoPage when there is none.
func Load(root, recipeID string) (*State, error) {
	var s State
	if err := state.ReadJSON(statePath(root, recipeID), &s); err != nil {
		if os.IsNotExist(err) || errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoPage
		}
		return nil, err
	}
	if s.Docs == nil {
		s.Docs = map[string]*Doc{}
	}
	return &s, nil
}

// Save writes review.json.
func Save(root, recipeID string, s *State) error {
	s.Schema = Schema
	s.Updated = now()
	return state.WriteJSON(statePath(root, recipeID), s)
}

// LoadRound reads a round's record.
func LoadRound(root, recipeID string, n int) (*Round, error) {
	var r Round
	if err := state.ReadJSON(RoundPath(root, recipeID, n), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// SaveRound writes a round's record.
func SaveRound(root, recipeID string, r *Round) error {
	r.Schema = Schema
	if err := os.MkdirAll(filepath.Dir(RoundPath(root, recipeID, r.Round)), 0755); err != nil {
		return err
	}
	return state.WriteJSON(RoundPath(root, recipeID, r.Round), r)
}

// Rounds lists the recorded rounds in order.
func Rounds(root, recipeID string) ([]*Round, error) {
	dir := filepath.Join(state.RecipeDir(root, recipeID), "feedback")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Round
	for _, e := range entries {
		var n int
		if _, err := fmt.Sscanf(e.Name(), "round-%d.json", &n); err != nil {
			continue
		}
		r, err := LoadRound(root, recipeID, n)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Round < out[j].Round })
	return out, nil
}

// Refresh brings review.json's document versions up to date with the
// files.  A document seen for the first time is version 1; a document that
// changed since its recorded hash without `bts review apply` gets a new
// version with a note saying so, so an edit made outside a round is
// visible rather than silent.  It returns the revisions it recorded.
func Refresh(root, recipeID string, s *State) ([]Revision, error) {
	var revs []Revision
	for _, doc := range PlanDocs {
		h, err := FileFingerprint(root, recipeID, doc)
		if err != nil {
			return nil, err
		}
		if h == "" {
			continue
		}
		d := s.Docs[doc]
		if d == nil {
			s.Docs[doc] = &Doc{Version: 1, Hash: h}
			continue
		}
		if d.Hash == h {
			continue
		}
		rev := Revision{File: doc, Version: d.Version + 1, From: d.Hash, To: h,
			Note: "edited outside a review round", At: now()}
		d.Version++
		d.Hash = h
		d.History = append(d.History, rev)
		revs = append(revs, rev)
	}
	return revs, nil
}

// Attach records the page's URL and the review point it now carries.
func Attach(root, recipeID, url, point string) (*State, error) {
	s, err := Load(root, recipeID)
	if errors.Is(err, ErrNoPage) {
		s = &State{Docs: map[string]*Doc{}}
	} else if err != nil {
		return nil, err
	}
	if strings.TrimSpace(url) == "" {
		return nil, errors.New("--url is required: the page's claude.ai link")
	}
	s.URL = strings.TrimSpace(url)
	s.Kit = KitVersion
	if point != "" && !contains(s.Points, point) {
		s.Points = append(s.Points, point)
	}
	if _, err := Refresh(root, recipeID, s); err != nil {
		return nil, err
	}
	return s, Save(root, recipeID, s)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func currentHashes(root, recipeID string) (map[string]string, error) {
	out := map[string]string{}
	for _, doc := range PlanDocs {
		h, err := FileFingerprint(root, recipeID, doc)
		if err != nil {
			return nil, err
		}
		if h != "" {
			out[doc] = h
		}
	}
	return out, nil
}

// OpenWork lists what still stands between a document and its
// confirmation: notes without a disposition, proposals not yet decided,
// and accepted proposals not yet applied.  doc "" means every document.
func OpenWork(root, recipeID, doc string) ([]string, error) {
	rounds, err := Rounds(root, recipeID)
	if err != nil {
		return nil, err
	}
	applied := map[string]bool{}
	rejected := map[string]bool{}
	accepted := map[string]bool{}
	for _, r := range rounds {
		for _, rev := range r.Revisions {
			for _, p := range rev.Proposals {
				applied[p] = true
			}
		}
		for _, p := range r.Rejected {
			rejected[p] = true
		}
		for _, p := range r.Accepted {
			accepted[p] = true
		}
	}
	var open []string
	for _, r := range rounds {
		for _, it := range r.Items {
			if it.Kind == KindConfirm {
				continue
			}
			if it.Disposition == "" {
				open = append(open, fmt.Sprintf("round %d note %s (%s) has no disposition", r.Round, it.Note, it.Anchor))
			}
		}
		for _, p := range r.Proposals {
			if doc != "" && p.Target != doc {
				continue
			}
			switch {
			case applied[p.ID] || rejected[p.ID] || p.Status == PStatusApplied || p.Status == PStatusRejected:
			case accepted[p.ID]:
				open = append(open, fmt.Sprintf("proposal %s on %s is accepted but not applied", p.ID, p.Target))
			default:
				open = append(open, fmt.Sprintf("proposal %s on %s is not decided", p.ID, p.Target))
			}
		}
	}
	return open, nil
}
