// Package review records a review on an artifact page: the page the user
// reviews a recipe's intent, scope and spec on, or a feature's intent,
// contracts and plan; the rounds of notes they send from it, the proposals
// Claude makes from those notes, and the versions of the plan documents the
// accepted proposals produce.
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
// Kit 2 adds feature pages (config "feature" in place of "recipe").  Kit 3
// adds the local transport (config "transport": "local"), for a page
// `bts review serve` serves.
const KitVersion = "3"

// Schema is the version of review.json and the round records.
const Schema = 1

// Points a recipe's page can carry, in the order a recipe reaches them.
// spec is the document the implementation is built from (SpecDoc).
var Points = []string{"intent", "scope", "spec"}

// FeaturePoint is the one point a feature's page carries: the feature's
// confirmation of its intent, contracts and plan together.
const FeaturePoint = "feature"

// PlanDocs are the documents a recipe's review can change and confirm.  A
// recipe holds at most one of the two spec documents.
var PlanDocs = []string{"intent.md", "scope.md", "final.md", "fix-spec.md"}

// FeatureDocs are the documents a feature's review can change: its intent,
// its contracts, and its plan (the items as a confirmation covers them,
// read from feature.json).
var FeatureDocs = []string{"intent.md", "contracts.md", PlanDoc}

// PlanDoc names a feature's plan among its documents.
const PlanDoc = "plan"

// FeatureBundle is the confirm row of a feature's page: the intent,
// contracts and plan confirmed together, as `bts feature confirm` pins them.
const FeatureBundle = "feature"

// Subject kinds.
const (
	KindRecipe  = "recipe"
	KindFeature = "feature"
)

// SpecDoc is the document a recipe of this type implements, the one the
// spec point reviews: fix-spec.md for a fix recipe, final.md otherwise.
func SpecDoc(recipeType string) string {
	if recipeType == "fix" {
		return "fix-spec.md"
	}
	return "final.md"
}

// Subject is what a review page reviews: a recipe in a project or a
// feature in the store.  Dir holds the record (review.json, feedback/);
// Work holds what the session publishes and uploads (page.html, upload/,
// export/), always inside Root, the session's project, where the Artifact
// and ArtifactData tools read files and the review kit lives.
type Subject struct {
	Kind string
	ID   string
	Root string
	Dir  string
	Work string
	// Docs are the plan documents the page can change, in order.
	Docs []string
	// Bundle, when set, is the one confirm row that covers every document
	// together; its fingerprint combines theirs and has its own version.
	Bundle string
	// HashDoc fingerprints a document; "" when it does not exist.
	HashDoc func(doc string) (string, error)
	// Log records a version change in the subject's changelog; may be nil.
	Log func(rev Revision)
}

// RecipeSubject is a recipe's review: its documents and record live in the
// recipe's directory, its page and uploads in the project's local state.
func RecipeSubject(root, recipeID string) Subject {
	dir := state.RecipeDir(root, recipeID)
	return Subject{
		Kind: KindRecipe, ID: recipeID, Root: root, Dir: dir,
		Work: filepath.Join(state.LocalPath(root), "recipes", recipeID, "review"),
		Docs: PlanDocs,
		HashDoc: func(doc string) (string, error) {
			return fileFingerprint(filepath.Join(dir, doc))
		},
		Log: func(rev Revision) {
			_ = state.AppendChangelog(root, recipeID, &state.ChangelogEntry{
				Action: "review", Output: rev.File, Result: rev.summary(),
			})
		},
	}
}

// FeatureSubject is a feature's review.  Its record lives in the feature's
// store directory beside intent.md and contracts.md; its page and uploads
// in the session's project (root).  plan returns the feature's plan as it
// stands, in the form its fingerprint is taken of.
func FeatureSubject(root, dir, featureID string, plan func() (string, error)) Subject {
	return Subject{
		Kind: KindFeature, ID: featureID, Root: root, Dir: dir,
		Work:   filepath.Join(state.LocalPath(root), "features", featureID, "review"),
		Docs:   FeatureDocs,
		Bundle: FeatureBundle,
		HashDoc: func(doc string) (string, error) {
			if doc == PlanDoc {
				p, err := plan()
				if err != nil {
					return "", err
				}
				return Fingerprint(p), nil
			}
			return fileFingerprint(filepath.Join(dir, doc))
		},
		Log: func(rev Revision) {
			_ = state.AppendJSONL(filepath.Join(dir, "changelog.jsonl"), map[string]interface{}{
				"time": now(), "action": "review", "detail": rev.File + " " + rev.summary(),
			})
		},
	}
}

// Points lists the review points the subject's page can carry.
func (sub Subject) Points() []string {
	if sub.Kind == KindFeature {
		return []string{FeaturePoint}
	}
	return Points
}

// Arg is how a `bts review` command names the subject.
func (sub Subject) Arg() string {
	if sub.Kind == KindFeature {
		return "--feature " + sub.ID
	}
	return sub.ID
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

func (rev Revision) summary() string {
	s := fmt.Sprintf("v%d (%s → %s)", rev.Version, rev.From, rev.To)
	if len(rev.Proposals) > 0 {
		s += " from " + strings.Join(rev.Proposals, ", ")
	}
	if rev.Round > 0 {
		s += fmt.Sprintf(", round %d", rev.Round)
	}
	if rev.Note != "" {
		s += ", " + rev.Note
	}
	return s
}

// State is review.json.
type State struct {
	Schema int    `json:"schema"`
	URL    string `json:"url,omitempty"`
	// Surface is "local" for a page bts serves itself (`bts review open`);
	// empty for a claude.ai artifact.
	Surface   string          `json:"surface,omitempty"`
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
	Doc         string   `json:"doc,omitempty"`      // a confirm: intent, scope, final, fix-spec or feature
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
	Schema int `json:"schema"`
	// Recipe is the reviewed subject's id: a recipe's, or a feature's.
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

func fileFingerprint(path string) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return Fingerprint(string(data)), nil
}

// FileFingerprint fingerprints a plan document of the recipe; "" when the
// file does not exist.
func FileFingerprint(root, recipeID, doc string) (string, error) {
	return fileFingerprint(filepath.Join(state.RecipeDir(root, recipeID), doc))
}

// bundleHash fingerprints the subject's documents together: what its one
// confirm row signs.  "" when the subject has no bundle.
func bundleHash(sub Subject) (string, error) {
	if sub.Bundle == "" {
		return "", nil
	}
	var parts []string
	for _, doc := range sub.Docs {
		h, err := sub.HashDoc(doc)
		if err != nil {
			return "", err
		}
		parts = append(parts, doc+":"+h)
	}
	return Fingerprint(strings.Join(parts, "\n")), nil
}

// CurrentHash is a document's fingerprint as it stands, the bundle's
// included; "" when it does not exist.
func CurrentHash(sub Subject, doc string) (string, error) {
	if doc == sub.Bundle && doc != "" {
		return bundleHash(sub)
	}
	return sub.HashDoc(doc)
}

func statePath(sub Subject) string {
	return filepath.Join(sub.Dir, "review.json")
}

// RoundPath is where a round's record lives.
func RoundPath(sub Subject, n int) string {
	return filepath.Join(sub.Dir, "feedback", fmt.Sprintf("round-%d.json", n))
}

// UploadDir is where bts writes the page documents the session uploads
// with ArtifactData (file_path), one JSON object per document.
func UploadDir(sub Subject) string {
	return filepath.Join(sub.Work, "upload")
}

// ErrNoPage means the subject has no review page.
var ErrNoPage = errors.New("no review page: run `bts review page` first")

// Load reads review.json; ErrNoPage when there is none.
func Load(sub Subject) (*State, error) {
	var s State
	if err := state.ReadJSON(statePath(sub), &s); err != nil {
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
func Save(sub Subject, s *State) error {
	s.Schema = Schema
	s.Updated = now()
	return state.WriteJSON(statePath(sub), s)
}

// LoadRound reads a round's record.
func LoadRound(sub Subject, n int) (*Round, error) {
	var r Round
	if err := state.ReadJSON(RoundPath(sub, n), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// SaveRound writes a round's record.
func SaveRound(sub Subject, r *Round) error {
	r.Schema = Schema
	if err := os.MkdirAll(filepath.Dir(RoundPath(sub, r.Round)), 0755); err != nil {
		return err
	}
	return state.WriteJSON(RoundPath(sub, r.Round), r)
}

// Rounds lists the recorded rounds in order.
func Rounds(sub Subject) ([]*Round, error) {
	dir := filepath.Join(sub.Dir, "feedback")
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
		r, err := LoadRound(sub, n)
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
func Refresh(sub Subject, s *State) ([]Revision, error) {
	var revs []Revision
	for _, doc := range sub.Docs {
		h, err := sub.HashDoc(doc)
		if err != nil {
			return nil, err
		}
		if rev := bump(s, doc, h, Revision{Note: "edited outside a review round"}); rev != nil {
			revs = append(revs, *rev)
		}
	}
	if sub.Bundle != "" {
		h, err := bundleHash(sub)
		if err != nil {
			return nil, err
		}
		if rev := bump(s, sub.Bundle, h, Revision{Note: "edited outside a review round"}); rev != nil {
			revs = append(revs, *rev)
		}
	}
	return revs, nil
}

// bump records hash h as doc's version: version 1 the first time, the
// next version when it changed (returned, with how from tmpl), nothing
// when it did not or the document does not exist.
func bump(s *State, doc, h string, tmpl Revision) *Revision {
	if h == "" {
		return nil
	}
	d := s.Docs[doc]
	if d == nil {
		s.Docs[doc] = &Doc{Version: 1, Hash: h}
		return nil
	}
	if d.Hash == h {
		return nil
	}
	rev := tmpl
	rev.File, rev.Version, rev.From, rev.To, rev.At = doc, d.Version+1, d.Hash, h, now()
	d.Version++
	d.Hash = h
	d.History = append(d.History, rev)
	return &rev
}

// Attach records the page's URL and the review point it now carries.
func Attach(sub Subject, url, point string) (*State, error) {
	return attach(sub, url, point, "")
}

// AttachLocal records a page bts serves itself: url is the local server's
// link, and the page's data lives in DataPath rather than on claude.ai.
func AttachLocal(sub Subject, url, point string) (*State, error) {
	return attach(sub, url, point, SurfaceLocal)
}

func attach(sub Subject, url, point, surface string) (*State, error) {
	s, err := Load(sub)
	if errors.Is(err, ErrNoPage) {
		s = &State{Docs: map[string]*Doc{}}
	} else if err != nil {
		return nil, err
	}
	if strings.TrimSpace(url) == "" {
		return nil, errors.New("--url is required: the page's claude.ai link")
	}
	s.URL = strings.TrimSpace(url)
	s.Surface = surface
	s.Kit = KitVersion
	if point != "" && !contains(s.Points, point) {
		s.Points = append(s.Points, point)
	}
	if _, err := Refresh(sub, s); err != nil {
		return nil, err
	}
	return s, Save(sub, s)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func currentHashes(sub Subject) (map[string]string, error) {
	out := map[string]string{}
	for _, doc := range sub.Docs {
		h, err := sub.HashDoc(doc)
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
// and accepted proposals not yet applied.  doc "" (or the subject's
// bundle) means every document.
func OpenWork(sub Subject, doc string) ([]string, error) {
	if doc == sub.Bundle {
		doc = ""
	}
	rounds, err := Rounds(sub)
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
