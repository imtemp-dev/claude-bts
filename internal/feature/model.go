package feature

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Schema is the version of feature.json, claim.json and manifest.json this
// build writes.  A build refuses to write a file whose schema is newer: its
// structs would drop the fields it does not know (a rollback to an older
// release must not erase a fork point).  2 added feature.json's multica link.
const Schema = 2

// File names inside a feature directory.
const (
	FeatureFile   = "feature.json"
	IntentFile    = "intent.md"
	ContractsFile = "contracts.md"
	PlanFile      = "plan.json"
	ChangelogFile = "changelog.jsonl"
	ConfirmedDir  = "confirmed"
	ItemsDir      = "items"
)

// ItemTypes are the recipe types an item may have.  Spec-only types
// (analyze, design) end at finalize and make no change to deliver, so they
// are not items.
var ItemTypes = []string{"build", "blueprint", "fix", "debug"}

// Feature is feature.json: the plan and the decisions about it.  Only the
// manager writes it, under the lock (Mutate).  It holds no item progress;
// that is derived from the attempts children write (Derive).
type Feature struct {
	Schema        int             `json:"schema"`
	Rev           int             `json:"rev"`
	ID            string          `json:"id"`
	Title         string          `json:"title"`
	Ticket        string          `json:"ticket,omitempty"`
	Repo          Repo            `json:"repo"`
	BaseBranch    string          `json:"base_branch"`
	Setup         string          `json:"setup,omitempty"` // makes a fresh checkout runnable; see SetupFor
	CreatedAt     string          `json:"created_at"`
	Items         []Item          `json:"items"`
	Confirmations []Confirmation  `json:"confirmations,omitempty"`
	Closed        *Decision       `json:"closed,omitempty"`
	Seen          map[string]Seen `json:"seen,omitempty"`
	Multica       *MulticaLink    `json:"multica,omitempty"`
}

// Item is one planned piece of the feature, worked as one recipe.
type Item struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Type      string    `json:"type"`
	Express   bool      `json:"express,omitempty"`
	DependsOn []string  `json:"depends_on,omitempty"`
	Decision  *Decision `json:"decision,omitempty"` // skip or defer
	Released  []Release `json:"released,omitempty"`
}

// Decision is a choice made with the user's words beside it.
type Decision struct {
	Kind   string `json:"kind"`
	Answer string `json:"answer"`
	At     string `json:"at"`
}

// Release frees an attempt that will not finish (its worktree or run is
// gone), so the item can be claimed again.
type Release struct {
	Attempt int    `json:"attempt"`
	Answer  string `json:"answer"`
	At      string `json:"at"`
}

// Confirmation pins what the user confirmed: the hashes of the intent, the
// contracts and the plan, with copies under confirmed/<n>/.
type Confirmation struct {
	N             int    `json:"n"`
	Answer        string `json:"answer"`
	At            string `json:"at"`
	IntentHash    string `json:"intent_hash"`
	ContractsHash string `json:"contracts_hash"`
	PlanHash      string `json:"plan_hash"`
}

// Seen is the manager's reconcile cursor for one item.
type Seen struct {
	Attempt     int    `json:"attempt"`
	ContentHash string `json:"content_hash"`
	At          string `json:"at"`
}

// PlanItem is the part of an item a confirmation covers.
type PlanItem struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Type      string   `json:"type"`
	Express   bool     `json:"express,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"`
}

// Plan returns the items as a confirmation covers them.
func (f *Feature) Plan() []PlanItem {
	plan := make([]PlanItem, 0, len(f.Items))
	for _, it := range f.Items {
		plan = append(plan, PlanItem{ID: it.ID, Title: it.Title, Type: it.Type, Express: it.Express, DependsOn: it.DependsOn})
	}
	return plan
}

// Item returns the item with id, or nil.
func (f *Feature) Item(id string) *Item {
	for i := range f.Items {
		if f.Items[i].ID == id {
			return &f.Items[i]
		}
	}
	return nil
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hashFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return hashBytes(nil)
	}
	return hashBytes(data)
}

func planHash(plan []PlanItem) string {
	b, _ := json.Marshal(plan)
	return hashBytes(b)
}

// Load reads feature.json.
func Load(dir string) (*Feature, error) {
	var f Feature
	if err := state.ReadJSON(filepath.Join(dir, FeatureFile), &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// ErrNoChange, returned by a Mutate function, leaves feature.json and the
// changelog untouched.
var ErrNoChange = errors.New("no change")

// Mutate changes feature.json under the feature's lock: it reads the file,
// refuses one written by a newer bts, applies fn, bumps rev and writes it
// back, and logs the change in the manager's changelog.
func Mutate(dir, action string, fn func(f *Feature) (string, error)) (*Feature, error) {
	return MutateWait(dir, action, lockTimeout, fn)
}

// savedError carries an error that MutateWait returns after it saves
// feature.json as the function left it.
type savedError struct{ err error }

func (e *savedError) Error() string { return e.err.Error() }
func (e *savedError) Unwrap() error { return e.err }

// saveAnd, returned by a Mutate function, saves its changes and then
// returns err: for a function that acted outside the store (on Multica)
// before it failed, so what it did is kept, not forgotten.
func saveAnd(err error) error { return &savedError{err: err} }

// MutateWait is Mutate waiting up to wait for the lock, for a change whose
// function talks to Multica while it holds the lock.
func MutateWait(dir, action string, wait time.Duration, fn func(f *Feature) (string, error)) (*Feature, error) {
	unlock, err := lockFor(dir, wait)
	if err != nil {
		return nil, err
	}
	defer unlock()
	f, err := Load(dir)
	if err != nil {
		return nil, err
	}
	if f.Schema > Schema {
		return nil, fmt.Errorf("%s was written by a newer bts (schema %d, this build knows %d); upgrade bts before changing it", FeatureFile, f.Schema, Schema)
	}
	detail, err := fn(f)
	if err == ErrNoChange {
		return f, nil
	}
	var saved *savedError
	if errors.As(err, &saved) {
		if werr := save(dir, action, strings.TrimSpace(detail+" (stopped: "+saved.err.Error()+")"), f); werr != nil {
			return nil, fmt.Errorf("%w; saving what was done before it also failed: %v", saved.err, werr)
		}
		return f, saved.err
	}
	if err != nil {
		return nil, err
	}
	if err := save(dir, action, detail, f); err != nil {
		return nil, err
	}
	return f, nil
}

// save writes feature.json with its rev bumped and logs the change.
func save(dir, action, detail string, f *Feature) error {
	f.Schema = Schema
	f.Rev++
	if err := state.WriteJSON(filepath.Join(dir, FeatureFile), f); err != nil {
		return err
	}
	_ = state.AppendJSONL(filepath.Join(dir, ChangelogFile), map[string]interface{}{
		"time": now(), "action": action, "rev": f.Rev, "detail": detail,
	})
	return nil
}

// CreateOptions are the inputs of Create.
type CreateOptions struct {
	Title  string
	Ticket string
	Repo   *Repo
	Base   string
	Setup  string
}

// SetSetup records the command that makes a fresh checkout of the
// feature's repository runnable; "" clears it.
func SetSetup(dir, setup string) error {
	_, err := Mutate(dir, "setup", func(f *Feature) (string, error) {
		setup = strings.TrimSpace(setup)
		if f.Setup == setup {
			return "", ErrNoChange
		}
		f.Setup = setup
		if setup == "" {
			return "setup cleared", nil
		}
		return "setup: " + setup, nil
	})
	return err
}

// SetupFor is the setup command of the feature a recipe is bound to, or
// "".  A Multica worktree's settings are written fresh by bts init, so the
// feature is where an item's worktree learns how to make the project
// runnable.
func SetupFor(r *state.RecipeState) string {
	if r == nil || r.Feature == "" {
		return ""
	}
	dir := r.FeatureDir
	if dir == "" || !fileExists(filepath.Join(dir, FeatureFile)) {
		d, err := Find(r.Feature)
		if err != nil {
			return ""
		}
		dir = d
	}
	f, err := Load(dir)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(f.Setup)
}

// Create makes a draft feature, or returns the open feature that already
// has the same ticket, so a manager that starts again does not make a twin.
func Create(o CreateOptions) (dir string, f *Feature, existed bool, err error) {
	if strings.TrimSpace(o.Title) == "" {
		return "", nil, false, fmt.Errorf("a feature needs a --title")
	}
	if o.Repo == nil || o.Repo.Key == "" {
		return "", nil, false, fmt.Errorf("no repository: run this inside the repository, or pass --repo")
	}
	if t := strings.TrimSpace(o.Ticket); t != "" {
		dirs, _ := All()
		for _, d := range dirs {
			if g, err := Load(d); err == nil && g.Ticket == t && g.Repo.Key == o.Repo.Key && g.Closed == nil {
				return d, g, true, nil
			}
		}
	}
	store, err := StoreRoot()
	if err != nil {
		return "", nil, false, err
	}
	id, dir, err := allocate(store, o.Repo.Key, o.Title)
	if err != nil {
		return "", nil, false, err
	}
	f = &Feature{
		Schema:     Schema,
		ID:         id,
		Title:      strings.TrimSpace(o.Title),
		Ticket:     strings.TrimSpace(o.Ticket),
		Repo:       Repo{URL: o.Repo.URL, Key: o.Repo.Key},
		BaseBranch: o.Base,
		Setup:      strings.TrimSpace(o.Setup),
		CreatedAt:  now(),
		Items:      []Item{},
	}
	if err := state.WriteJSON(filepath.Join(dir, FeatureFile), f); err != nil {
		return "", nil, false, err
	}
	intent := fmt.Sprintf("# Intent: %s\n\n## Problem\n\n## Purpose\n\n## Success criteria\n\n## Out of scope\n", f.Title)
	contracts := fmt.Sprintf("# Contracts: %s\n\nInterfaces and promises between items.  Each one names the item that provides it and the items that rely on it.\n", f.Title)
	for name, body := range map[string]string{IntentFile: intent, ContractsFile: contracts} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			return "", nil, false, err
		}
	}
	_ = state.AppendJSONL(filepath.Join(dir, ChangelogFile), map[string]interface{}{
		"time": now(), "action": "create", "rev": 0, "detail": f.Title,
	})
	return dir, f, false, nil
}

func validType(t string) bool {
	for _, x := range ItemTypes {
		if t == x {
			return true
		}
	}
	return false
}

// checkPlan reports what makes a plan unfit to confirm or to claim from.
func checkPlan(plan []PlanItem) error {
	if len(plan) == 0 {
		return fmt.Errorf("the plan has no items")
	}
	ids := map[string]bool{}
	for _, it := range plan {
		ids[it.ID] = true
	}
	for _, it := range plan {
		if !validType(it.Type) {
			return fmt.Errorf("%s: type %q is not one of %s", it.ID, it.Type, strings.Join(ItemTypes, ", "))
		}
		if it.Express && it.Type != "build" {
			return fmt.Errorf("%s: express is a mode of the build recipe", it.ID)
		}
		for _, d := range it.DependsOn {
			if !ids[d] {
				return fmt.Errorf("%s depends on %s, which is not in the plan", it.ID, d)
			}
		}
	}
	if _, err := stages(plan); err != nil {
		return err
	}
	return nil
}

// stages layers the plan by dependency: an item's stage is one more than
// its latest dependency's.  A cycle is an error.
func stages(plan []PlanItem) (map[string]int, error) {
	deps := map[string][]string{}
	for _, it := range plan {
		deps[it.ID] = it.DependsOn
	}
	st := map[string]int{}
	visiting := map[string]bool{}
	var visit func(id string, path []string) (int, error)
	visit = func(id string, path []string) (int, error) {
		if s, ok := st[id]; ok {
			return s, nil
		}
		if visiting[id] {
			return 0, fmt.Errorf("dependency cycle: %s", strings.Join(append(path, id), " → "))
		}
		visiting[id] = true
		s := 1
		for _, d := range deps[id] {
			ds, err := visit(d, append(path, id))
			if err != nil {
				return 0, err
			}
			if ds+1 > s {
				s = ds + 1
			}
		}
		visiting[id] = false
		st[id] = s
		return s, nil
	}
	for _, it := range plan {
		if _, err := visit(it.ID, nil); err != nil {
			return nil, err
		}
	}
	return st, nil
}

// Confirm records the user's confirmation of the intent, the contracts and
// the plan as they are now, and copies them to confirmed/<n>/.  A change
// after this is pending until the next confirmation.
func Confirm(dir, answer string) (int, error) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return 0, fmt.Errorf("--answer must carry the user's words")
	}
	var n int
	_, err := Mutate(dir, "confirm", func(f *Feature) (string, error) {
		if f.Closed != nil {
			return "", fmt.Errorf("feature %s is %s", f.ID, f.Closed.Kind)
		}
		plan := f.Plan()
		if err := checkPlan(plan); err != nil {
			return "", err
		}
		// Each file is read once: the bytes hashed are the bytes copied.
		intent, _ := os.ReadFile(filepath.Join(dir, IntentFile))
		contracts, _ := os.ReadFile(filepath.Join(dir, ContractsFile))
		if !hasContent(string(intent)) {
			return "", fmt.Errorf("%s says nothing yet; write the intent before confirming", IntentFile)
		}
		c := Confirmation{
			N:             len(f.Confirmations) + 1,
			Answer:        answer,
			At:            now(),
			IntentHash:    hashBytes(intent),
			ContractsHash: hashBytes(contracts),
			PlanHash:      planHash(plan),
		}
		if k := len(f.Confirmations); k > 0 {
			last := f.Confirmations[k-1]
			if last.IntentHash == c.IntentHash && last.ContractsHash == c.ContractsHash && last.PlanHash == c.PlanHash {
				return "", fmt.Errorf("nothing changed since confirmation %d", last.N)
			}
		}
		cdir := filepath.Join(dir, ConfirmedDir, fmt.Sprint(c.N))
		if err := os.MkdirAll(cdir, 0755); err != nil {
			return "", err
		}
		for name, data := range map[string][]byte{IntentFile: intent, ContractsFile: contracts} {
			if err := os.WriteFile(filepath.Join(cdir, name), data, 0644); err != nil {
				return "", err
			}
		}
		if err := state.WriteJSON(filepath.Join(cdir, PlanFile), plan); err != nil {
			return "", err
		}
		f.Confirmations = append(f.Confirmations, c)
		n = c.N
		return fmt.Sprintf("confirmation %d: %s", c.N, answer), nil
	})
	return n, err
}

// hasContent reports whether a Markdown document says anything beyond
// headings.
func hasContent(doc string) bool {
	for _, line := range strings.Split(doc, "\n") {
		l := strings.TrimSpace(line)
		if l != "" && !strings.HasPrefix(l, "#") {
			return true
		}
	}
	return false
}

// Snapshot is a confirmation's copy, read and checked against its hashes.
type Snapshot struct {
	N         int
	Intent    []byte
	Contracts []byte
	Plan      []PlanItem
}

// ReadConfirmed reads the latest confirmation's copy under confirmed/<n>/
// and checks every part against the hashes recorded at confirm, so an edit
// of the copy is caught rather than served.  It returns nil when the feature
// has never been confirmed.
func ReadConfirmed(dir string, f *Feature) (*Snapshot, error) {
	if len(f.Confirmations) == 0 {
		return nil, nil
	}
	c := f.Confirmations[len(f.Confirmations)-1]
	cdir := filepath.Join(dir, ConfirmedDir, fmt.Sprint(c.N))
	s := &Snapshot{N: c.N}
	var err error
	if s.Intent, err = os.ReadFile(filepath.Join(cdir, IntentFile)); err != nil {
		return nil, fmt.Errorf("confirmation %d: %w", c.N, err)
	}
	if s.Contracts, err = os.ReadFile(filepath.Join(cdir, ContractsFile)); err != nil {
		return nil, fmt.Errorf("confirmation %d: %w", c.N, err)
	}
	if err := state.ReadJSON(filepath.Join(cdir, PlanFile), &s.Plan); err != nil {
		return nil, fmt.Errorf("confirmation %d has no plan: %w", c.N, err)
	}
	var bad []string
	if hashBytes(s.Intent) != c.IntentHash {
		bad = append(bad, IntentFile)
	}
	if hashBytes(s.Contracts) != c.ContractsHash {
		bad = append(bad, ContractsFile)
	}
	if planHash(s.Plan) != c.PlanHash {
		bad = append(bad, PlanFile)
	}
	if len(bad) > 0 {
		return nil, fmt.Errorf("confirmation %d was changed after the user confirmed it (%s in %s); restore it, or change the working copies and confirm again", c.N, strings.Join(bad, ", "), cdir)
	}
	return s, nil
}

// Confirmed returns the latest confirmation and its plan, or n == 0.
func Confirmed(dir string, f *Feature) (n int, plan []PlanItem, err error) {
	s, err := ReadConfirmed(dir, f)
	if err != nil || s == nil {
		return 0, nil, err
	}
	return s.N, s.Plan, nil
}

// Pending lists what changed since the latest confirmation: "intent",
// "contracts", "plan".
func Pending(dir string, f *Feature) []string {
	if len(f.Confirmations) == 0 {
		return nil
	}
	c := f.Confirmations[len(f.Confirmations)-1]
	var out []string
	if hashFile(filepath.Join(dir, IntentFile)) != c.IntentHash {
		out = append(out, "intent")
	}
	if hashFile(filepath.Join(dir, ContractsFile)) != c.ContractsHash {
		out = append(out, "contracts")
	}
	if planHash(f.Plan()) != c.PlanHash {
		out = append(out, "plan")
	}
	return out
}
