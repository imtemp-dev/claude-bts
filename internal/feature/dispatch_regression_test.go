package feature

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Regression tests from the feature store review of 2026-11-16 (bts #144 to
// #147).

func notes(acts []Action) string {
	var out []string
	for _, a := range acts {
		if a.Note {
			out = append(out, a.Item+": "+a.What)
		}
	}
	return strings.Join(out, "\n")
}

// mergedInto reads multica.items.<item>.merged.into from feature.json.
func mergedInto(t *testing.T, dir, item string) string {
	t.Helper()
	var raw struct {
		Multica struct {
			Items map[string]struct {
				Merged *struct {
					Into string `json:"into"`
				} `json:"merged"`
			} `json:"items"`
		} `json:"multica"`
	}
	if err := state.ReadJSON(filepath.Join(dir, FeatureFile), &raw); err != nil {
		t.Fatal(err)
	}
	if m := raw.Multica.Items[item].Merged; m != nil {
		return m.Into
	}
	return ""
}

func countCalls(fm *fakeMultica, prefix string) int {
	n := 0
	for _, c := range fm.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// bts #144: Next treats a skipped dependency as met, so chooseBase must too;
// otherwise the item waits for a pull request that will never exist.
func TestDispatchSkippedDependencyStartsOnBase(t *testing.T) {
	fm := newFakeMultica(t)
	dir, _ := newFeature(t)
	i1, _ := AddItem(dir, ItemSpec{Title: ptr("one"), Type: ptr("build")})
	i2, _ := AddItem(dir, ItemSpec{Title: ptr("two"), Type: ptr("build"), DependsOn: &[]string{i1}})
	_, _ = Confirm(dir, "go")
	if _, _, err := Publish(dir, "mgr", "wrk"); err != nil {
		t.Fatal(err)
	}
	if _, err := Dispatch(dir); err != nil {
		t.Fatal(err)
	}
	if err := DecideItem(dir, i1, "skip", "not needed"); err != nil {
		t.Fatal(err)
	}
	acts, err := Dispatch(dir)
	if err != nil {
		t.Fatal(err)
	}
	c2 := fm.byItem(i2)
	if c2.Status != "todo" || !strings.Contains(c2.Description, "--ref main") {
		t.Fatalf("i2 must start on main once i1 is skipped: %s\n%s", c2.Status, notes(acts))
	}
	f, _ := Load(dir)
	if mi := f.Multica.Items[i2]; mi.Base != "main" || mi.BaseItem != "" {
		t.Fatalf("i2's base: %+v", mi)
	}
}

// bts #145: a pull request that merged into the branch it was stacked on
// has not reached the base; its dependents wait until that branch merges.
func TestMergeIntoStackedBranchWaitsForTheBase(t *testing.T) {
	fm := newFakeMultica(t)
	dir, _ := newFeature(t)
	i1, _ := AddItem(dir, ItemSpec{Title: ptr("one"), Type: ptr("build")})
	i2, _ := AddItem(dir, ItemSpec{Title: ptr("two"), Type: ptr("build"), DependsOn: &[]string{i1}})
	i3, _ := AddItem(dir, ItemSpec{Title: ptr("three"), Type: ptr("build"), DependsOn: &[]string{i2}})
	_, _ = Confirm(dir, "go")
	_, _, _ = Publish(dir, "mgr", "wrk")
	_, _ = Dispatch(dir)
	writeAttempt(t, dir, i1, 1, "complete", "done", "gate")
	u1 := writePR(t, dir, i1, 1, "b1", "main")
	_, _ = Dispatch(dir) // i2 stacks on b1
	writeAttempt(t, dir, i2, 1, "complete", "done", "gate")
	u2 := writePR(t, dir, i2, 1, "b2", "b1")

	// i2's pull request merges into b1, i1's branch, which has not merged.
	fm.prStates[u2], fm.prBases[u2] = "MERGED", "b1"
	acts, err := Dispatch(dir)
	if err != nil {
		t.Fatal(err)
	}
	if into := mergedInto(t, dir, i2); into != "b1" {
		t.Fatalf("i2's merge must record where it went: %q", into)
	}
	if c3 := fm.byItem(i3); c3.Status != "backlog" {
		t.Fatalf("i3 must not start on main without i1 and i2: %s\n%s", c3.Status, c3.Description)
	}
	if n := notes(acts); !strings.Contains(n, i3+": waits: ") || !strings.Contains(n, "merged into b1") {
		t.Fatalf("the wait must name the branch i2 merged into:\n%s", n)
	}

	// i1 merges into main: i2's code reached main through b1; i3 starts on
	// main, and nothing asks i2, merged itself, to restack.
	fm.prStates[u1] = "MERGED"
	if _, err := Dispatch(dir); err != nil {
		t.Fatal(err)
	}
	c3 := fm.byItem(i3)
	if c3.Status != "todo" || !strings.Contains(c3.Description, "--ref main") {
		t.Fatalf("i3 starts on main once b1 merged: %s\n%s", c3.Status, c3.Description)
	}
	if c2 := fm.byItem(i2); len(c2.Comments) != 0 {
		t.Fatalf("a merged item is never asked to restack: %v", c2.Comments)
	}
}

// bts #145: a merge into a branch that is neither the base nor the one the
// item was stacked on is reported, and its dependents wait.
func TestMergeIntoAnotherBranchIsReported(t *testing.T) {
	fm := newFakeMultica(t)
	dir, _ := newFeature(t)
	i1, _ := AddItem(dir, ItemSpec{Title: ptr("one"), Type: ptr("build")})
	i2, _ := AddItem(dir, ItemSpec{Title: ptr("two"), Type: ptr("build"), DependsOn: &[]string{i1}})
	_, _ = Confirm(dir, "go")
	_, _, _ = Publish(dir, "mgr", "wrk")
	_, _ = Dispatch(dir)
	writeAttempt(t, dir, i1, 1, "complete", "done", "gate")
	u1 := writePR(t, dir, i1, 1, "b1", "main")
	fm.prStates[u1], fm.prBases[u1] = "MERGED", "release"
	acts, err := Dispatch(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c2 := fm.byItem(i2); c2.Status != "backlog" {
		t.Fatalf("i2 must wait: i1 merged into release, not main: %s", c2.Status)
	}
	if n := notes(acts); !strings.Contains(n, "merged into release") {
		t.Fatalf("the merge into release must be reported:\n%s", n)
	}
}

// bts #146: an item set to todo keeps its base when a later call in the same
// dispatch fails.
func TestDispatchFailureKeepsWhatItDid(t *testing.T) {
	fm := newFakeMultica(t)
	dir, _ := newFeature(t)
	i1, _ := AddItem(dir, ItemSpec{Title: ptr("one"), Type: ptr("build")})
	i2, _ := AddItem(dir, ItemSpec{Title: ptr("two"), Type: ptr("build")})
	_, _ = Confirm(dir, "go")
	_, _, _ = Publish(dir, "mgr", "wrk")
	fm.failAfter["issue update"] = 1 // i1 starts; starting i2 fails
	if _, err := Dispatch(dir); err == nil {
		t.Fatal("the injected failure must surface")
	}
	if c1 := fm.byItem(i1); c1.Status != "todo" {
		t.Fatalf("i1 started before the failure: %s", c1.Status)
	}
	f, _ := Load(dir)
	if f.Multica.Items[i1] == nil || f.Multica.Items[i1].Base != "main" {
		t.Fatalf("i1's base must be kept with the failure: %+v", f.Multica.Items)
	}
	if _, err := Dispatch(dir); err != nil {
		t.Fatal(err)
	}
	f, _ = Load(dir)
	if f.Multica.Items[i1].Base != "main" || f.Multica.Items[i2].Base != "main" || fm.byItem(i2).Status != "todo" {
		t.Fatalf("after the retry: i1 %+v, i2 %+v", f.Multica.Items[i1], f.Multica.Items[i2])
	}
}

// bts #146: a wakeup that failed to register is registered by the next
// dispatch, once.
func TestDispatchRegistersMissingWakeups(t *testing.T) {
	fm := newFakeMultica(t)
	dir, _ := newFeature(t)
	_, _ = AddItem(dir, ItemSpec{Title: ptr("one"), Type: ptr("build")})
	_, _ = Confirm(dir, "go")
	_, _, _ = Publish(dir, "mgr", "wrk")
	fm.failAfter["issue wakeup create"] = 0
	if _, err := Dispatch(dir); err == nil {
		t.Fatal("the injected failure must surface")
	}
	if _, err := Dispatch(dir); err != nil {
		t.Fatal(err)
	}
	if len(fm.wakeups) != 2 {
		t.Fatalf("both wakeups must be registered by the retry: %v", fm.wakeups)
	}
	if _, err := Dispatch(dir); err != nil || len(fm.wakeups) != 2 {
		t.Fatalf("and never twice: %v %v", fm.wakeups, err)
	}
}

// bts #146: a sub-issue whose metadata failed is found by its recorded id,
// not made again.
func TestDispatchFindsSubIssueWithoutMetadata(t *testing.T) {
	fm := newFakeMultica(t)
	dir, _ := newFeature(t)
	i1, _ := AddItem(dir, ItemSpec{Title: ptr("one"), Type: ptr("build")})
	_, _ = Confirm(dir, "go")
	_, _, _ = Publish(dir, "mgr", "wrk")
	fm.failAfter["issue metadata set"] = 0
	if _, err := Dispatch(dir); err == nil {
		t.Fatal("the injected failure must surface")
	}
	if _, err := Dispatch(dir); err != nil {
		t.Fatal(err)
	}
	if n := countCalls(fm, "issue create"); n != 2 {
		t.Fatalf("parent + one sub-issue, got %d creates", n)
	}
	c1 := fm.byItem(i1)
	f, _ := Load(dir)
	if c1 == nil || f.Multica.Items[i1] == nil || f.Multica.Items[i1].Issue != c1.ID || c1.Status != "todo" || len(fm.wakeups) != 2 {
		t.Fatalf("the sub-issue is finished and started: %+v %+v %v", c1, f.Multica.Items[i1], fm.wakeups)
	}
}

// bts #147: a pull request state gh cannot read is reported, not skipped in
// silence.
func TestDispatchReportsUnreadablePRState(t *testing.T) {
	fm := newFakeMultica(t)
	dir, _ := newFeature(t)
	i1, _ := AddItem(dir, ItemSpec{Title: ptr("one"), Type: ptr("build")})
	_, _ = Confirm(dir, "go")
	_, _, _ = Publish(dir, "mgr", "wrk")
	_, _ = Dispatch(dir)
	writeAttempt(t, dir, i1, 1, "complete", "done", "gate")
	u1 := writePR(t, dir, i1, 1, "b1", "main")
	fm.ghErr[u1] = errors.New("gh: authentication required")
	acts, err := Dispatch(dir)
	if err != nil {
		t.Fatal(err)
	}
	if n := notes(acts); !strings.Contains(n, i1+": ") || !strings.Contains(n, "authentication required") {
		t.Fatalf("the failed read must be reported:\n%s", n)
	}
}

// bts #147: dispatch reads pull request states before it takes the feature
// lock, so a slow gh never holds every other feature command.
func TestDispatchReadsPRStatesOutsideTheLock(t *testing.T) {
	fm := newFakeMultica(t)
	dir, _ := newFeature(t)
	i1, _ := AddItem(dir, ItemSpec{Title: ptr("one"), Type: ptr("build")})
	_, _ = Confirm(dir, "go")
	_, _, _ = Publish(dir, "mgr", "wrk")
	_, _ = Dispatch(dir)
	writeAttempt(t, dir, i1, 1, "complete", "done", "gate")
	u1 := writePR(t, dir, i1, 1, "b1", "main")
	fm.prStates[u1] = "MERGED"
	fm.lockDir = dir
	if _, err := Dispatch(dir); err != nil {
		t.Fatal(err)
	}
	if fm.ghCalls == 0 || fm.ghUnderLock {
		t.Fatalf("gh calls %d, under the lock %v", fm.ghCalls, fm.ghUnderLock)
	}
	f, _ := Load(dir)
	if f.Multica.Items[i1].Merged == nil {
		t.Fatal("the merge read outside the lock must still be recorded")
	}
}
