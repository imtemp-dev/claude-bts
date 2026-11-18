package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

const rid = "r-001-review"

// fixture is a project with a recipe holding intent.md and scope.md and
// the review kit deployed.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := state.RecipeDir(root, rid)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "intent.md"), "# Intent\n\nStatus: EXPLORING\n\n## Purpose\n\nMake reports honest.\n")
	write(t, filepath.Join(dir, "scope.md"), "## Scope\n\n### In Scope\n\n- Contract v4.\n\n### Out of Scope\n\n- Declined outcomes and the file-claim rule.\n\n### Status: DRAFT\n")
	for _, k := range KitFiles {
		write(t, filepath.Join(root, k), "/* kit */")
	}
	return root
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// export writes an ArtifactData out_dir export: wrapped documents for
// rounds, bare fields for notes, the two shapes the reader accepts.
func export(t *testing.T, dir string, rounds map[string]map[string]interface{}, notes map[string]map[string]interface{}) {
	t.Helper()
	for id, d := range rounds {
		b, _ := json.Marshal(map[string]interface{}{"id": id, "data": d, "version": 1})
		write(t, filepath.Join(dir, "rounds", id+".json"), string(b))
	}
	for id, d := range notes {
		b, _ := json.Marshal(d)
		write(t, filepath.Join(dir, "notes", id+".json"), string(b))
	}
}

func TestFingerprint_IgnoresTheStatusLine(t *testing.T) {
	a := "# Intent\n\nStatus: EXPLORING\n\nBody.\n"
	b := "# Intent\n\nStatus: CONFIRMED\n\nBody.\n"
	if Fingerprint(a) != Fingerprint(b) {
		t.Error("confirming flips the status line and must not change the fingerprint")
	}
	if Fingerprint(a) == Fingerprint(strings.Replace(a, "Body", "Other", 1)) {
		t.Error("a content change must change the fingerprint")
	}
	if len(Fingerprint(a)) != 12 {
		t.Errorf("a fingerprint is 12 hex digits, got %q", Fingerprint(a))
	}
}

func TestPageBrief_RecordsVersionOneAndNeedsTheDocument(t *testing.T) {
	root := fixture(t)
	if err := os.Remove(filepath.Join(state.RecipeDir(root, rid), "scope.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := PageBrief(RecipeSubject(root, rid), "scope"); err == nil || !strings.Contains(err.Error(), "scope.md does not exist") {
		t.Fatalf("a scope page needs scope.md, got %v", err)
	}
	b, err := PageBrief(RecipeSubject(root, rid), "intent")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Docs) != 1 || b.Docs[0].Version != "v1" || b.Docs[0].File != "intent.md" {
		t.Fatalf("intent.md starts at v1, got %+v", b.Docs)
	}
	if len(b.Kit) != 2 || b.Capabilities["db"] == nil || b.Config["recipe"] != rid {
		t.Errorf("the brief names the kit, the capabilities and the config, got %+v", b)
	}
	if _, err := PageBrief(RecipeSubject(root, rid), "final"); err == nil {
		t.Error("an unknown point is refused")
	}
	// The kit missing from the project is refused with the way out.
	_ = os.Remove(filepath.Join(root, KitFiles[0]))
	if _, err := PageBrief(RecipeSubject(root, rid), "intent"); err == nil || !strings.Contains(err.Error(), "bts update") {
		t.Errorf("a missing kit is refused, got %v", err)
	}
}

func TestPageBrief_AnEditOutsideARoundIsAVisibleVersion(t *testing.T) {
	root := fixture(t)
	if _, err := PageBrief(RecipeSubject(root, rid), "scope"); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(state.RecipeDir(root, rid), "scope.md"), "## Scope\n\n### In Scope\n\n- Contract v4, edited.\n\n### Status: DRAFT\n")
	b, err := PageBrief(RecipeSubject(root, rid), "scope")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Revisions) != 1 || b.Revisions[0].Version != 2 || b.Revisions[0].Note != "edited outside a review round" {
		t.Fatalf("a direct edit is recorded as a new version, got %+v", b.Revisions)
	}
}

// The full loop: a review round with a question and a change request, a
// proposal, the apply round, then the confirmation gate.
func TestRoundLoop_NoteToProposalToVersionToConfirm(t *testing.T) {
	root := fixture(t)
	if _, err := PageBrief(RecipeSubject(root, rid), "scope"); err != nil {
		t.Fatal(err)
	}
	if _, err := Attach(RecipeSubject(root, rid), "https://claude.ai/artifact/x", "scope"); err != nil {
		t.Fatal(err)
	}
	ex := filepath.Join(t.TempDir(), "export")
	export(t, ex,
		map[string]map[string]interface{}{
			"r-1": {"round": 1, "kind": "review", "status": "submitted", "ids": []string{"n-a", "n-b"}, "page": "scope v1"},
		},
		map[string]map[string]interface{}{
			"n-a": {"label": "Lane › PR 3", "anchor": "Lane › PR 3", "text": "what is this?", "kind": "note"},
			"n-b": {"anchor": "PR 4 › file claims", "text": "move the file-claim rule to PR 3", "kind": "note"},
		})
	rounds, err := Import(RecipeSubject(root, rid), ex, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rounds) != 1 || len(rounds[0].Items) != 2 || rounds[0].Items[1].Text != "move the file-claim rule to PR 3" {
		t.Fatalf("round 1 holds both notes verbatim, got %+v", rounds)
	}
	if rounds[0].FilesBefore["scope.md"] == "" {
		t.Error("the round records the documents' fingerprints before")
	}

	if _, err := Done(RecipeSubject(root, rid), 1); err == nil {
		t.Error("a round with undisposed notes cannot be marked processed")
	}
	if err := Dispose(RecipeSubject(root, rid), 1, "n-a", Declined, ""); err == nil {
		t.Error("declining needs the reason")
	}
	if err := Dispose(RecipeSubject(root, rid), 1, "n-a", Proposed, ""); err == nil {
		t.Error("a change request is disposed by propose, not dispose")
	}
	if err := Dispose(RecipeSubject(root, rid), 1, "n-a", Answered, "explained the lane"); err != nil {
		t.Fatal(err)
	}
	p, path, err := Propose(RecipeSubject(root, rid), 1, Proposal{Note: "n-b", Target: "scope.md", Kind: "move",
		Summary: "The file-claim rule moves into PR 3.", ToLane: "Lane › PR 3", ToLabel: "PR 3"})
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "p-1-1" || p.Anchor != "PR 4 › file claims" {
		t.Fatalf("the proposal is numbered by round and anchored on its note, got %+v", p)
	}
	var up map[string]interface{}
	raw, _ := os.ReadFile(path)
	_ = json.Unmarshal(raw, &up)
	if up["status"] != "proposed" || up["to_lane"] != "Lane › PR 3" {
		t.Errorf("the upload document carries what the page draws, got %v", up)
	}
	if _, err := Done(RecipeSubject(root, rid), 1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Propose(RecipeSubject(root, rid), 1, Proposal{Note: "n-b", Target: "wireframe.md", Summary: "x"}); err == nil {
		t.Error("a proposal targets intent.md or scope.md only")
	}

	if err := ConfirmCheck(RecipeSubject(root, rid), "scope.md", rounds[0].FilesBefore["scope.md"]); err == nil || !strings.Contains(err.Error(), "not decided") {
		t.Fatalf("an undecided proposal blocks the confirmation, got %v", err)
	}

	// The user accepts and presses Update plan: round 2, kind apply.
	export(t, ex, map[string]map[string]interface{}{
		"r-1": {"round": 1, "kind": "review", "status": "processed", "ids": []string{"n-a", "n-b"}},
		"r-2": {"round": 2, "kind": "apply", "status": "submitted", "ids": []string{"p-1-1"}, "rejected": []string{}},
	}, nil)
	if _, err := Import(RecipeSubject(root, rid), ex, 0); err != nil {
		t.Fatal(err)
	}
	if err := ConfirmCheck(RecipeSubject(root, rid), "scope.md", rounds[0].FilesBefore["scope.md"]); err == nil || !strings.Contains(err.Error(), "not applied") {
		t.Fatalf("an accepted proposal not yet applied blocks the confirmation, got %v", err)
	}
	if _, _, err := Apply(RecipeSubject(root, rid), 2); err == nil || !strings.Contains(err.Error(), "unchanged") {
		t.Fatalf("applying without editing the document is refused, got %v", err)
	}
	scope := filepath.Join(state.RecipeDir(root, rid), "scope.md")
	write(t, scope, "## Scope\n\n### In Scope\n\n- Contract v4, with the file-claim rule.\n\n### Out of Scope\n\n- Declined outcomes.\n\n### Status: DRAFT\n")
	revs, uploads, err := Apply(RecipeSubject(root, rid), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 1 || revs[0].Version != 2 || revs[0].Proposals[0] != "p-1-1" || len(uploads) != 1 {
		t.Fatalf("scope.md becomes v2 from p-1-1, got %+v %v", revs, uploads)
	}
	if _, err := Done(RecipeSubject(root, rid), 2); err != nil {
		t.Fatal(err)
	}

	if err := ConfirmCheck(RecipeSubject(root, rid), "scope.md", rounds[0].FilesBefore["scope.md"]); err == nil || !strings.Contains(err.Error(), "is now") {
		t.Fatalf("confirming the old fingerprint is refused, got %v", err)
	}
	if err := ConfirmCheck(RecipeSubject(root, rid), "scope.md", revs[0].To); err != nil {
		t.Fatalf("the current version with nothing open confirms, got %v", err)
	}
	if err := ConfirmCheck(RecipeSubject(root, rid), "scope.md", ""); err == nil {
		t.Error("--via review needs the fingerprint")
	}
	// A change after the page's version needs republishing first.
	write(t, scope, "## Scope\n\n### In Scope\n\n- Something else.\n\n### Status: DRAFT\n")
	if err := ConfirmCheck(RecipeSubject(root, rid), "scope.md", revs[0].To); err == nil {
		t.Error("a document changed after the page's version is refused")
	}
}

func TestImport_KeepsDispositionsAndNamesMissingNotes(t *testing.T) {
	root := fixture(t)
	if _, err := PageBrief(RecipeSubject(root, rid), "intent"); err != nil {
		t.Fatal(err)
	}
	ex := filepath.Join(t.TempDir(), "export")
	export(t, ex,
		map[string]map[string]interface{}{"r-1": {"round": 1, "status": "picked", "ids": []string{"n-a"}}},
		map[string]map[string]interface{}{"n-a": {"anchor": "Header", "text": "hello", "kind": "note"}})
	if _, err := Import(RecipeSubject(root, rid), ex, 0); err != nil {
		t.Fatal(err)
	}
	if err := Dispose(RecipeSubject(root, rid), 1, "n-a", Answered, "said hello"); err != nil {
		t.Fatal(err)
	}
	if _, err := Import(RecipeSubject(root, rid), ex, 1); err != nil {
		t.Fatal(err)
	}
	r, _ := LoadRound(RecipeSubject(root, rid), 1)
	if r.Items[0].Disposition != Answered || r.Kind != KindReview {
		t.Errorf("a re-import keeps the disposition and defaults the kind to review, got %+v", r)
	}

	export(t, ex, map[string]map[string]interface{}{"r-2": {"round": 2, "status": "submitted", "ids": []string{"n-gone"}}}, nil)
	if _, err := Import(RecipeSubject(root, rid), ex, 2); err == nil || !strings.Contains(err.Error(), "n-gone") {
		t.Errorf("a round naming a note the export lacks is refused by name, got %v", err)
	}
	if _, err := Import(RecipeSubject(root, rid), t.TempDir(), 0); err == nil {
		t.Error("an empty export is refused")
	}
}

func TestConfirmCheck_NoPage(t *testing.T) {
	root := fixture(t)
	if err := ConfirmCheck(RecipeSubject(root, rid), "intent.md", "abcdef123456"); err != ErrNoPage {
		t.Errorf("--via review without a page is refused, got %v", err)
	}
}
