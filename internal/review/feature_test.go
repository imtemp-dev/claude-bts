package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// featureFixture is a session project with the review kit and a feature
// directory holding intent.md and contracts.md; the plan is held by the
// caller and read through the subject at each fingerprint.
func featureFixture(t *testing.T) (root, dir string, plan *string, logged *[]Revision) {
	t.Helper()
	root = t.TempDir()
	for _, k := range KitFiles {
		write(t, filepath.Join(root, k), "/* kit */")
	}
	dir = filepath.Join(t.TempDir(), "f-005-attach")
	write(t, filepath.Join(dir, "intent.md"), "# Intent\n\n## Problem\n\nImage upload is untested on GPT.\n")
	write(t, filepath.Join(dir, "contracts.md"), "# Contracts\n\n## C1\n\ni1 provides the file-reference message; i2 relies on it.\n")
	p := `[{"id":"i1","title":"Existing chat on GPT","type":"debug"},{"id":"i2","title":"Global Ask server","type":"build","depends_on":["i1"]}]`
	plan = &p
	logged = &[]Revision{}
	return root, dir, plan, logged
}

func featureSub(root, dir string, plan *string, logged *[]Revision) Subject {
	sub := FeatureSubject(root, dir, "f-005-attach", func() (string, error) { return *plan, nil })
	sub.Log = func(rev Revision) { *logged = append(*logged, rev) }
	return sub
}

func TestFeatureBrief_OnePointOneConfirmRowCoveringEverything(t *testing.T) {
	root, dir, plan, logged := featureFixture(t)
	sub := featureSub(root, dir, plan, logged)
	if _, err := PageBrief(sub, "scope"); err == nil {
		t.Error("a feature's page carries the feature point only")
	}
	b, err := PageBrief(sub, "")
	if err != nil {
		t.Fatal(err)
	}
	if b.Point != FeaturePoint || b.Kind != KindFeature || b.Arg != "--feature f-005-attach" {
		t.Errorf("point \"\" means the feature's point, got %+v", b)
	}
	if b.Page != filepath.Join(root, ".bts", "local", "features", "f-005-attach", "review", "page.html") {
		t.Errorf("the page lives in the session's project, where Artifact reads it, got %s", b.Page)
	}
	if fi, err := os.Stat(filepath.Dir(b.Page)); err != nil || !fi.IsDir() {
		t.Error("the brief makes the page's directory")
	}
	joined := strings.Join(b.Sections, "\n")
	for _, want := range []string{"dependency graph", "contracts between items", "confirm row for the feature"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the sections name %q, got %v", want, b.Sections)
		}
	}
	if b.Config["feature"] != "f-005-attach" || b.Config["recipe"] != nil {
		t.Errorf("the config names the feature in place of a recipe, got %v", b.Config)
	}
	if c, _ := b.Config["confirm"].([]string); len(c) != 1 || c[0] != FeatureBundle {
		t.Errorf("one confirm row, the bundle, got %v", b.Config["confirm"])
	}
	docs := b.Config["docs"].(map[string]interface{})
	for _, k := range []string{"intent", "contracts", "plan", "feature"} {
		if docs[k] == nil {
			t.Errorf("the config carries %s's version, got %v", k, docs)
		}
	}
	if f := docs["feature"].(map[string]interface{})["file"]; f != "intent.md + contracts.md + plan" {
		t.Errorf("the bundle's row names what it covers, got %v", f)
	}
	if f := docs["plan"].(map[string]interface{})["file"]; f != "plan" {
		t.Errorf("a document that is not a file names itself, got %v", f)
	}
	if b.Config["kit"] != KitVersion || KitVersion < "2" {
		t.Errorf("a feature page needs kit 2 or later, got %v", b.Config["kit"])
	}

	// No intent yet: nothing to build a page on.
	_ = os.Remove(filepath.Join(dir, "intent.md"))
	if _, err := PageBrief(sub, FeaturePoint); err == nil || !strings.Contains(err.Error(), "intent.md does not exist") {
		t.Errorf("a feature with no intent has no page yet, got %v", err)
	}
}

func TestFeatureRefresh_APlanChangeMovesThePlanAndTheBundle(t *testing.T) {
	root, dir, plan, logged := featureFixture(t)
	sub := featureSub(root, dir, plan, logged)
	if _, err := PageBrief(sub, ""); err != nil {
		t.Fatal(err)
	}
	*plan = strings.Replace(*plan, "Global Ask server", "Global Ask server and pane", 1)
	b, err := PageBrief(sub, "")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, r := range b.Revisions {
		got[r.File] = r.Version
		if r.Note != "edited outside a review round" {
			t.Errorf("an edit outside a round says so, got %q", r.Note)
		}
	}
	if got["plan"] != 2 || got["feature"] != 2 || len(got) != 2 {
		t.Fatalf("an item edit makes plan v2 and feature v2, nothing else, got %v", got)
	}
}

// The loop on a feature: a note becomes a proposal on the plan, Update plan
// applies it once the item list changed, and the bundle the user confirms
// moves with it.
func TestFeatureRoundLoop_PlanProposalMovesTheBundleItConfirms(t *testing.T) {
	root, dir, plan, logged := featureFixture(t)
	sub := featureSub(root, dir, plan, logged)
	b, err := PageBrief(sub, "")
	if err != nil {
		t.Fatal(err)
	}
	v1 := ""
	for _, d := range b.Docs {
		if d.File == FeatureBundle {
			v1 = d.Hash
		}
	}
	if _, err := Attach(sub, "https://claude.ai/artifact/f", ""); err != nil {
		t.Fatal(err)
	}
	ex := filepath.Join(t.TempDir(), "export")
	export(t, ex,
		map[string]map[string]interface{}{
			"r-1": {"round": 1, "kind": "review", "status": "submitted", "ids": []string{"n-a"}, "page": "feature v1"},
		},
		map[string]map[string]interface{}{
			"n-a": {"anchor": "Items › i2", "text": "split the pane into its own item", "kind": "note"},
		})
	rounds, err := Import(sub, ex, 0)
	if err != nil {
		t.Fatal(err)
	}
	if rounds[0].Recipe != "f-005-attach" || rounds[0].FilesBefore["plan"] == "" {
		t.Fatalf("the round names the feature and records the plan's fingerprint, got %+v", rounds[0])
	}
	if _, err := os.Stat(RoundPath(sub, 1)); err != nil || !strings.HasPrefix(RoundPath(sub, 1), dir) {
		t.Errorf("the record lives beside the feature's documents, got %s", RoundPath(sub, 1))
	}
	if _, _, err := Propose(sub, 1, Proposal{Note: "n-a", Target: "scope.md", Summary: "x"}); err == nil {
		t.Error("a feature has no scope.md to change")
	}
	p, path, err := Propose(sub, 1, Proposal{Note: "n-a", Target: PlanDoc, Kind: "add", Summary: "i3 builds the pane on top of i2."})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, filepath.Join(root, ".bts", "local", "features")) {
		t.Errorf("uploads live in the session's project, where ArtifactData reads them, got %s", path)
	}
	if _, err := Done(sub, 1); err != nil {
		t.Fatal(err)
	}
	if err := ConfirmCheck(sub, FeatureBundle, v1); err == nil || !strings.Contains(err.Error(), "not decided") {
		t.Fatalf("an undecided proposal on any document blocks the feature's confirmation, got %v", err)
	}

	export(t, ex, map[string]map[string]interface{}{
		"r-1": {"round": 1, "kind": "review", "status": "processed", "ids": []string{"n-a"}},
		"r-2": {"round": 2, "kind": "apply", "status": "submitted", "ids": []string{p.ID}, "rejected": []string{}},
	}, nil)
	if _, err := Import(sub, ex, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Apply(sub, 2); err == nil || !strings.Contains(err.Error(), "unchanged") {
		t.Fatalf("applying before the items changed is refused, got %v", err)
	}
	*plan = strings.TrimSuffix(*plan, "]") + `,{"id":"i3","title":"Global Ask pane","type":"build","depends_on":["i2"]}]`
	revs, _, err := Apply(sub, 2)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Revision{}
	for _, r := range revs {
		got[r.File] = r
	}
	if got["plan"].Version != 2 || got["feature"].Version != 2 || got["feature"].Proposals[0] != p.ID || len(revs) != 2 {
		t.Fatalf("the plan and the bundle each become v2 from %s, got %+v", p.ID, revs)
	}
	if len(*logged) != 2 {
		t.Errorf("each revision goes to the feature's changelog, got %+v", *logged)
	}
	if _, err := Done(sub, 2); err != nil {
		t.Fatal(err)
	}
	if err := ConfirmCheck(sub, FeatureBundle, v1); err == nil || !strings.Contains(err.Error(), "is now") {
		t.Fatalf("confirming feature v1 after v2 is refused, got %v", err)
	}
	if err := ConfirmCheck(sub, FeatureBundle, got["feature"].To); err != nil {
		t.Fatalf("feature v2 with nothing open confirms, got %v", err)
	}
	// Any one document changing after the page's version needs a republish.
	write(t, filepath.Join(dir, "contracts.md"), "# Contracts\n\n## C1\n\nChanged.\n")
	if err := ConfirmCheck(sub, FeatureBundle, got["feature"].To); err == nil {
		t.Error("a contracts edit after the page's version is refused")
	}
}
