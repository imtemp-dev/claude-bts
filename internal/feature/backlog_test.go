package feature

import (
	"strings"
	"testing"
)

func noteFor(acts []Action, item, text string) bool {
	for _, a := range acts {
		if a.Note && a.Item == item && strings.Contains(a.What, text) {
			return true
		}
	}
	return false
}

// Trial problems #150 and #153 (AGEN-208, AGEN-211): a done item without a
// draft pull request, or with one closed without merging, held the parent
// back with nothing said.
func TestDispatchSaysWhyTheParentWaits(t *testing.T) {
	fm := newFakeMultica(t)
	dir, _ := newFeature(t)
	i1, _ := AddItem(dir, ItemSpec{Title: ptr("door"), Type: ptr("build")})
	_, _ = Confirm(dir, "go")
	link, _, err := Publish(dir, "mgr", "wrk")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Dispatch(dir); err != nil {
		t.Fatal(err)
	}
	parent := fm.issues[link.Parent]

	writeAttempt(t, dir, i1, 1, "complete", "done", "gate")
	acts, err := Dispatch(dir)
	if err != nil && err != ErrNoChange {
		t.Fatal(err)
	}
	if parent.Status == "in_review" {
		t.Fatal("the parent waits for the done item's draft pull request")
	}
	if !noteFor(acts, i1, "done without a draft pull request") {
		t.Errorf("dispatch must say the done item has no pull request, got %+v", acts)
	}

	url := writePR(t, dir, i1, 1, "u/NA-T-2-door", "main")
	fm.prStates[url] = "CLOSED"
	acts, err = Dispatch(dir)
	if err != nil && err != ErrNoChange {
		t.Fatal(err)
	}
	if parent.Status == "in_review" {
		t.Fatal("a pull request closed without merging must not count toward the parent's review")
	}
	if !noteFor(acts, i1, "closed without merging") {
		t.Errorf("dispatch must note the closed pull request, got %+v", acts)
	}

	fm.prStates[url] = "OPEN"
	if _, err := Dispatch(dir); err != nil {
		t.Fatal(err)
	}
	if parent.Status != "in_review" {
		t.Fatalf("reopened: the parent goes to review, got %s", parent.Status)
	}
}

// Trial problem #151 (AGEN-209): status showed only the latest attempt's
// pull request, so an a2 still open after a1 opened one showed none.
func TestDeriveShowsTheItemsPullRequest(t *testing.T) {
	dir, _ := newFeature(t)
	i1, _ := AddItem(dir, ItemSpec{Title: ptr("door"), Type: ptr("build")})
	_, _ = Confirm(dir, "go")
	writeAttempt(t, dir, i1, 1, "complete", "done", "gate")
	url := writePR(t, dir, i1, 1, "u/NA-T-2-door", "main")
	writeAttempt(t, dir, i1, 2, "implement", "", "")

	f, _ := Load(dir)
	s, err := Derive(dir, f)
	if err != nil {
		t.Fatal(err)
	}
	is := s.item(i1)
	if is.Latest == nil || is.Latest.N != 2 || is.Latest.PRURL != "" {
		t.Fatalf("fixture: the latest attempt is a2 with no pull request: %+v", is.Latest)
	}
	if is.PR == nil || is.PR.URL != url || is.PR.Attempt != 1 {
		t.Fatalf("status must show the item's pull request, opened by a1: %+v", is.PR)
	}
}
