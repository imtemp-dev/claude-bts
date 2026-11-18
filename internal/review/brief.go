package review

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// KitFiles are the kit's files, relative to the project root, published
// beside the page under their base names.
var KitFiles = []string{
	".claude/skills/bts-review-page/kit/bts-review-kit.js",
	".claude/skills/bts-review-page/kit/bts-review-kit.css",
}

// sections lists what a page must show at each review point.  The intent
// sections stay when the scope sections are added, and both stay when the
// spec sections are.  The spec has three shapes; PageBrief adds its
// confirm row, which names the recipe's spec document.
var sections = map[string][]string{
	"intent": {
		"Where it fits: the item's place in the roadmap or the feature",
		"Today and after, drawn side by side (a mock of the output when there is one)",
		"The main scenario, step by step",
		"Key decisions: what was chosen and what was rejected",
		"A confirm row for intent.md",
	},
	"scope": {
		"The shape: this item solid, later work dashed, what is out greyed",
		"Lanes: earlier items, this item, the next item, out of scope (one card per In Scope and Out of Scope entry)",
		"Open decisions as option cards, the options drawn side by side",
		"Assumptions and hard parts",
		"A confirm row for scope.md",
	},
	// A blueprint or full build recipe's final.md.
	"spec": {
		"The parts the wireframe settled, drawn as one diagram with their files: each box says what it holds and who fills it",
		"Each invariant pinned to its owner on that diagram, with the test that proves it (a build recipe's falsifier: red now, a [carried] one green)",
		"Boundary contracts: what crosses each edge, in which shape, and what happens when it is missing or wrong",
		"Known Uncertainties: each one resolved and how, or deferred and what will show it",
		"The order of the work, each step with what it leaves runnable",
	},
	// An express build's trimmed final.md: one drawing carries it.
	"spec-express": {
		"The parts and files this change touches, drawn as one diagram, each invariant pinned to its owner with its test (red now, a [carried] one green)",
		"Known Uncertainties: each one resolved and how, or deferred and what will show it",
	},
	// A debug recipe's final.md or a fix recipe's fix-spec.md.
	"spec-fix": {
		"The failing path today, step by step, with the root cause marked where it lives",
		"The same path after the fix, drawn beside it",
		"Each change: the file, what it changes, and why that closes the cause",
		"The regression test: what it drives and why it fails today",
		"Risks and Known Uncertainties: what the fix could break and what is not yet known",
	},
}

// specShape picks the spec sections for a recipe.
func specShape(r *state.RecipeState) string {
	switch {
	case r.Type == "debug" || r.Type == "fix":
		return "spec-fix"
	case r.IsExpress():
		return "spec-express"
	}
	return "spec"
}

// DocBrief is one plan document as the page must show it.
type DocBrief struct {
	File    string `json:"file"`
	Version string `json:"version"`
	Hash    string `json:"hash"`
}

// Brief is what `bts review page` hands the session: what to build, with
// which kit, declaring which capabilities, showing which versions.
type Brief struct {
	Recipe       string                 `json:"recipe"`
	Point        string                 `json:"point"`
	URL          string                 `json:"url,omitempty"`
	Sections     []string               `json:"sections"`
	Docs         []DocBrief             `json:"docs"`
	Kit          map[string]string      `json:"kit"`
	KitVersion   string                 `json:"kit_version"`
	Capabilities map[string]interface{} `json:"capabilities"`
	Config       map[string]interface{} `json:"config"`
	Revisions    []Revision             `json:"revisions,omitempty"`
}

// Capabilities is the declaration every review page publishes with.
func Capabilities() map[string]interface{} {
	rule := func(path string) map[string]interface{} {
		return map[string]interface{}{"path": path, "read": "view", "write": "admin"}
	}
	return map[string]interface{}{
		"comments":  map[string]interface{}{},
		"dictation": map[string]interface{}{},
		"db": map[string]interface{}{"rules": []interface{}{
			rule("notes"), rule("rounds"), rule("proposals"),
		}},
	}
}

// PageBrief refreshes the document versions and returns the brief for a
// review point.  It refuses a point it does not know and a recipe whose
// documents for that point do not exist yet.
func PageBrief(root, recipeID, point string) (*Brief, error) {
	point = strings.ToLower(strings.TrimSpace(point))
	if !contains(Points, point) {
		return nil, fmt.Errorf("--point takes %s, got %q", strings.Join(Points, ", "), point)
	}
	s, err := Load(root, recipeID)
	if errors.Is(err, ErrNoPage) {
		s = &State{Docs: map[string]*Doc{}, Kit: KitVersion}
	} else if err != nil {
		return nil, err
	}
	need := point + ".md"
	var recipe *state.RecipeState
	if point == "spec" {
		if recipe, err = state.LoadRecipeState(root, recipeID); err != nil {
			return nil, err
		}
		need = SpecDoc(recipe.Type)
	}
	if h, err := FileFingerprint(root, recipeID, need); err != nil {
		return nil, err
	} else if h == "" {
		return nil, fmt.Errorf("%s does not exist yet: write it before building its page", need)
	}
	revs, err := Refresh(root, recipeID, s)
	if err != nil {
		return nil, err
	}
	if err := Save(root, recipeID, s); err != nil {
		return nil, err
	}
	b := &Brief{Recipe: recipeID, Point: point, URL: s.URL, KitVersion: KitVersion,
		Capabilities: Capabilities(), Revisions: revs, Kit: map[string]string{}}
	switch point {
	case "intent":
		b.Sections = append(b.Sections, sections["intent"]...)
	case "scope":
		b.Sections = append(b.Sections, sections["intent"]...)
		b.Sections = append(b.Sections, sections["scope"]...)
	case "spec":
		// A fix or debug recipe has no intent or scope to keep.
		for _, p := range []string{"intent", "scope"} {
			if s.Docs[p+".md"] != nil {
				b.Sections = append(b.Sections, sections[p]...)
			}
		}
		b.Sections = append(b.Sections, sections[specShape(recipe)]...)
		b.Sections = append(b.Sections, "A confirm row for "+need)
	}
	docsCfg := map[string]interface{}{}
	for _, doc := range PlanDocs {
		d := s.Docs[doc]
		if d == nil {
			continue
		}
		v := fmt.Sprintf("v%d", d.Version)
		b.Docs = append(b.Docs, DocBrief{File: doc, Version: v, Hash: d.Hash})
		docsCfg[strings.TrimSuffix(doc, ".md")] = map[string]interface{}{"ver": v, "hash": d.Hash}
	}
	for _, rel := range KitFiles {
		abs := filepath.Join(root, rel)
		if _, err := os.Stat(abs); err != nil {
			return nil, fmt.Errorf("the review kit is missing (%s): run `bts update`", rel)
		}
		b.Kit[filepath.Base(rel)] = abs
	}
	b.Config = map[string]interface{}{"recipe": recipeID, "kit": KitVersion, "docs": docsCfg}
	if point == "spec" {
		// Intent and scope were confirmed at their own points; a second
		// row for them here would ask for a sign-off nobody needs.
		b.Config["confirm"] = []string{strings.TrimSuffix(need, ".md")}
	}
	return b, nil
}

// ConfirmCheck is the gate on `bts recipe confirm --via review`: the
// fingerprint the user confirmed must be the document's current one, and
// nothing may stand open on the page for that document.
func ConfirmCheck(root, recipeID, doc, hash string) error {
	s, err := Load(root, recipeID)
	if err != nil {
		return err
	}
	cur, err := FileFingerprint(root, recipeID, doc)
	if err != nil {
		return err
	}
	if cur == "" {
		return fmt.Errorf("%s does not exist", doc)
	}
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return errors.New("--hash is required with --via review: the fingerprint the user confirmed on the page")
	}
	if !strings.HasPrefix(cur, hash) || len(hash) < 8 {
		v := ""
		if d := s.Docs[doc]; d != nil {
			v = fmt.Sprintf(" (recorded v%d is %s)", d.Version, d.Hash)
		}
		return fmt.Errorf("the user confirmed %s %s, but the file is now %s%s: publish the current version and ask again", doc, hash, cur, v)
	}
	if d := s.Docs[doc]; d != nil && d.Hash != cur {
		return fmt.Errorf("%s changed since the page's v%d (%s): run `bts review page` and republish before confirming", doc, d.Version, d.Hash)
	}
	open, err := OpenWork(root, recipeID, doc)
	if err != nil {
		return err
	}
	if len(open) > 0 {
		return fmt.Errorf("the page still has open work on %s:\n  %s", doc, strings.Join(open, "\n  "))
	}
	return nil
}
