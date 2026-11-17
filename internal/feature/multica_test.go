package feature

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/multica"
)

// fakeMultica is an in-memory Multica the dispatcher talks to through the
// CLI runner.
type fakeMultica struct {
	mu       sync.Mutex
	n        int
	issues   map[string]*fakeIssue
	order    []string
	wakeups  []string
	reruns   []string
	calls    []string
	prStates map[string]string
	prBases  map[string]string // the branch a pull request merged into (default main)
	ghErr    map[string]error  // gh pr view fails for these URLs

	// failAfter lets n calls starting with a prefix succeed and fails the
	// next one, once.
	failAfter map[string]int

	// lockDir, when set, is tried from inside gh: a call made while dispatch
	// holds the feature lock sets ghUnderLock.
	lockDir     string
	ghCalls     int
	ghUnderLock bool
}

type fakeIssue struct {
	ID, Key, Title, Status, Parent, Assignee, Description string
	Stage                                                 int
	Meta                                                  map[string]interface{}
	Comments                                              []string
}

func newFakeMultica(t *testing.T) *fakeMultica {
	fm := &fakeMultica{issues: map[string]*fakeIssue{}, prStates: map[string]string{}, prBases: map[string]string{},
		ghErr: map[string]error{}, failAfter: map[string]int{}}
	t.Cleanup(multica.SetRunner(fm.run))
	t.Cleanup(SetGHRunner(fm.gh))
	return fm
}

// gh answers `gh pr view <url> --json state,…` from prStates (default OPEN)
// and prBases (default main).
func (fm *fakeMultica) gh(_ string, _ string, args ...string) ([]byte, error) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.ghCalls++
	if fm.lockDir != "" {
		if unlock, err := lockFor(fm.lockDir, 0); err != nil {
			fm.ghUnderLock = true
		} else {
			unlock()
		}
	}
	if len(args) >= 3 && args[0] == "pr" && args[1] == "view" {
		if err := fm.ghErr[args[2]]; err != nil {
			return nil, err
		}
		st := fm.prStates[args[2]]
		if st == "" {
			st = "OPEN"
		}
		base := fm.prBases[args[2]]
		if base == "" {
			base = "main"
		}
		return fm.json(map[string]interface{}{"state": st, "baseRefName": base, "mergedAt": "2026-11-11T00:00:00Z", "mergeCommit": map[string]string{"oid": "abc123merge"}}), nil
	}
	return nil, fmt.Errorf("fake gh: unexpected %v", args)
}

// writePR fakes the draft pull request a worker opened for an attempt.
func writePR(t *testing.T, dir, item string, n int, branch, base string) string {
	t.Helper()
	url := "https://github.com/org/app/pull/" + item
	if err := writeJSONFile(dir, item, n, PRFile, &PRRecord{Schema: Schema, URL: url, Branch: branch, Base: base, HeadSHA: "h"}); err != nil {
		t.Fatal(err)
	}
	return url
}

func flag(args []string, name string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == name {
			return args[i+1]
		}
	}
	return ""
}

func (fm *fakeMultica) json(v interface{}) []byte { b, _ := json.Marshal(v); return b }

func (fm *fakeMultica) issueJSON(i *fakeIssue) map[string]interface{} {
	return map[string]interface{}{"id": i.ID, "identifier": i.Key, "title": i.Title, "status": i.Status, "stage": i.Stage, "assignee_id": i.Assignee, "metadata": i.Meta}
}

func (fm *fakeMultica) run(_ context.Context, stdin string, args ...string) ([]byte, error) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	call := strings.Join(args, " ")
	fm.calls = append(fm.calls, call)
	for prefix, n := range fm.failAfter {
		if strings.HasPrefix(call, prefix) {
			if n == 0 {
				delete(fm.failAfter, prefix)
				return nil, fmt.Errorf("fake multica: injected failure on %s", prefix)
			}
			fm.failAfter[prefix] = n - 1
		}
	}
	switch {
	case len(args) >= 2 && args[0] == "issue" && args[1] == "create":
		fm.n++
		i := &fakeIssue{ID: "id-" + strconv.Itoa(fm.n), Key: "T-" + strconv.Itoa(fm.n), Title: flag(args, "--title"),
			Status: flag(args, "--status"), Parent: flag(args, "--parent"), Assignee: flag(args, "--assignee-id"), Meta: map[string]interface{}{}}
		i.Stage, _ = strconv.Atoi(flag(args, "--stage"))
		fm.issues[i.ID] = i
		fm.order = append(fm.order, i.ID)
		return fm.json(fm.issueJSON(i)), nil
	case len(args) >= 3 && args[0] == "issue" && args[1] == "metadata" && args[2] == "set":
		fm.issues[args[3]].Meta[flag(args, "--key")] = flag(args, "--value")
		return nil, nil
	case len(args) >= 2 && args[0] == "issue" && args[1] == "status":
		fm.issues[args[2]].Status = args[3]
		return nil, nil
	case len(args) >= 2 && args[0] == "issue" && args[1] == "get":
		return fm.json(fm.issueJSON(fm.issues[args[2]])), nil
	case len(args) >= 2 && args[0] == "issue" && args[1] == "children":
		stages := map[int][]map[string]interface{}{}
		for _, id := range fm.order {
			if i := fm.issues[id]; i.Parent == args[2] {
				stages[i.Stage] = append(stages[i.Stage], fm.issueJSON(i))
			}
		}
		var out []map[string]interface{}
		for s := 1; s <= 9; s++ {
			if len(stages[s]) > 0 {
				out = append(out, map[string]interface{}{"stage": s, "issues": stages[s]})
			}
		}
		return fm.json(map[string]interface{}{"stages": out, "unstaged": []interface{}{}}), nil
	case len(args) >= 3 && args[0] == "issue" && args[1] == "wakeup" && args[2] == "create":
		fm.wakeups = append(fm.wakeups, fmt.Sprintf("%s→%s %s%s", args[3], flag(args, "--agent-id"), flag(args, "--event"), flag(args, "--until-issue")))
		return nil, nil
	case len(args) >= 2 && args[0] == "issue" && args[1] == "runs":
		return []byte("[]"), nil
	case len(args) >= 2 && args[0] == "issue" && args[1] == "rerun":
		fm.reruns = append(fm.reruns, args[2])
		return nil, nil
	case len(args) >= 2 && args[0] == "issue" && args[1] == "comment":
		fm.issues[args[3]].Comments = append(fm.issues[args[3]].Comments, stdin)
		return nil, nil
	case len(args) >= 2 && args[0] == "issue" && args[1] == "update":
		fm.issues[args[2]].Description = stdin
		return nil, nil
	}
	return nil, fmt.Errorf("fake multica: unexpected %v", args)
}

func (fm *fakeMultica) byItem(item string) *fakeIssue {
	for _, id := range fm.order {
		if i := fm.issues[id]; i.Meta["bts.item"] == item {
			return i
		}
	}
	return nil
}

func TestPublishAndDispatch(t *testing.T) {
	fm := newFakeMultica(t)
	dir, f := newFeature(t)
	i1, _ := AddItem(dir, ItemSpec{Title: ptr("door"), Type: ptr("build")})
	i2, _ := AddItem(dir, ItemSpec{Title: ptr("turn"), Type: ptr("build"), DependsOn: &[]string{i1}})
	i3, _ := AddItem(dir, ItemSpec{Title: ptr("docs"), Type: ptr("fix")})

	if _, _, err := Publish(dir, "mgr", "wrk"); err == nil {
		t.Fatal("an unconfirmed feature must not publish")
	}
	_, _ = Confirm(dir, "go")
	link, existed, err := Publish(dir, "mgr", "wrk")
	if err != nil || existed {
		t.Fatalf("publish: %+v %v %v", link, existed, err)
	}
	parent := fm.issues[link.Parent]
	if parent.Assignee != "mgr" || parent.Status != "todo" || parent.Meta["bts.feature"] != f.ID {
		t.Fatalf("parent: %+v", parent)
	}
	if again, existed, _ := Publish(dir, "mgr", "wrk"); !existed || again.Parent != link.Parent {
		t.Fatal("a second publish returns the first")
	}

	acts, err := Dispatch(dir)
	if err != nil {
		t.Fatal(err)
	}
	c1, c2, c3 := fm.byItem(i1), fm.byItem(i2), fm.byItem(i3)
	if c1 == nil || c2 == nil || c3 == nil {
		t.Fatalf("every confirmed item gets a sub-issue: %v", acts)
	}
	if c1.Status != "todo" || c3.Status != "todo" || c2.Status != "backlog" {
		t.Fatalf("ready items start, i2 waits: %s %s %s", c1.Status, c2.Status, c3.Status)
	}
	if c2.Stage != 2 || c1.Stage != 1 || c2.Assignee != "wrk" || c2.Parent != link.Parent {
		t.Fatalf("i2 sub-issue: %+v", c2)
	}
	if len(fm.wakeups) != 6 {
		t.Fatalf("two wakeups per item, got %v", fm.wakeups)
	}

	// Again: in step, nothing created or started.
	calls := len(fm.calls)
	acts, err = Dispatch(dir)
	if err != nil || len(acts) != 0 {
		t.Fatalf("a second dispatch: %v %v", acts, err)
	}
	for _, c := range fm.calls[calls:] {
		if strings.HasPrefix(c, "issue create") || strings.HasPrefix(c, "issue status") {
			t.Fatalf("a second dispatch changed something: %s", c)
		}
	}

	// i1 done with its draft pull request: i2 starts, stacked on i1's
	// branch.  i3 skipped by the user: cancelled.
	writeAttempt(t, dir, i1, 1, "complete", "done", "gate")
	writePR(t, dir, i1, 1, "u/NA-T-2-door", "main")
	writeAttempt(t, dir, i3, 1, "cancelled", "cancelled", "")
	if err := DecideItem(dir, i3, "skip", "not needed"); err != nil {
		t.Fatal(err)
	}
	if _, err := Dispatch(dir); err != nil {
		t.Fatal(err)
	}
	if c2.Status != "todo" || c3.Status != "cancelled" {
		t.Fatalf("after i1 done and i3 skipped: i2 %s, i3 %s", c2.Status, c3.Status)
	}
	if !strings.Contains(c2.Description, "--ref u/NA-T-2-door") || !strings.Contains(c2.Description, "stacked on "+i1) {
		t.Fatalf("i2 must start on i1's branch:\n%s", c2.Description)
	}

	// i2 cancelled by its worker: claimable, restarted with no active run.
	writeAttempt(t, dir, i2, 1, "cancelled", "cancelled", "")
	c2.Status = "cancelled"
	if _, err := Dispatch(dir); err != nil {
		t.Fatal(err)
	}
	if c2.Status != "todo" || len(fm.reruns) != 1 || fm.reruns[0] != c2.ID {
		t.Fatalf("a claimable item restarts: %s %v", c2.Status, fm.reruns)
	}

	// Every item settled, but i2 has no draft pull request yet: the parent
	// waits; with it, the parent goes to review.
	writeAttempt(t, dir, i2, 2, "complete", "done", "gate")
	if _, err := Dispatch(dir); err != nil {
		t.Fatal(err)
	}
	if parent.Status == "in_review" {
		t.Fatal("the parent waits for every done item's draft pull request")
	}
	writePR(t, dir, i2, 2, "u/NA-T-3-turn", "u/NA-T-2-door")
	if _, err := Dispatch(dir); err != nil {
		t.Fatal(err)
	}
	if parent.Status != "in_review" {
		t.Fatalf("parent: %s", parent.Status)
	}
	f, _ = Load(dir)
	if s, _ := Derive(dir, f); s.Multica == nil || len(s.Multica.Items) != 3 {
		t.Fatalf("status --json carries the link: %+v", s.Multica)
	}
}

// A chain i1 ← i2 ← i3 stacks two deep at most; i4 needing two unmerged
// items waits; a merge is recorded and the item stacked on it is asked to
// restack.
func TestStackLimitMergeAndRestackRequest(t *testing.T) {
	fm := newFakeMultica(t)
	dir, _ := newFeature(t)
	i1, _ := AddItem(dir, ItemSpec{Title: ptr("one"), Type: ptr("build")})
	i2, _ := AddItem(dir, ItemSpec{Title: ptr("two"), Type: ptr("build"), DependsOn: &[]string{i1}})
	i3, _ := AddItem(dir, ItemSpec{Title: ptr("three"), Type: ptr("build"), DependsOn: &[]string{i2}})
	i4, _ := AddItem(dir, ItemSpec{Title: ptr("four"), Type: ptr("build"), DependsOn: &[]string{i1, i2}})
	_, _ = Confirm(dir, "go")
	_, _, _ = Publish(dir, "mgr", "wrk")
	_, _ = Dispatch(dir)

	writeAttempt(t, dir, i1, 1, "complete", "done", "gate")
	u1 := writePR(t, dir, i1, 1, "b1", "main")
	_, _ = Dispatch(dir) // i2 stacks on b1
	writeAttempt(t, dir, i2, 1, "complete", "done", "gate")
	writePR(t, dir, i2, 1, "b2", "b1")
	acts, err := Dispatch(dir)
	if err != nil {
		t.Fatal(err)
	}
	var notes []string
	for _, a := range acts {
		if a.Note {
			notes = append(notes, a.Item+": "+a.What)
		}
	}
	all := strings.Join(notes, "\n")
	if fm.byItem(i3).Status != "backlog" || !strings.Contains(all, i3+": waits: stacking on "+i2+" would make 3") {
		t.Fatalf("i3 must wait for a merge (stack limit):\n%s", all)
	}
	if fm.byItem(i4).Status != "backlog" || !strings.Contains(all, i4+": waits:") || !strings.Contains(all, "one unmerged item at most") {
		t.Fatalf("i4 must wait (two unmerged dependencies):\n%s", all)
	}

	// i1 merges: recorded; i2, stacked on it, is asked to restack; i3 can
	// now stack on i2 (one unmerged level under it).
	fm.prStates[u1] = "MERGED"
	_, err = Dispatch(dir)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := Load(dir)
	if m := f.Multica.Items[i1].Merged; m == nil || m.SHA != "abc123merge" {
		t.Fatalf("i1's merge must be recorded: %+v", f.Multica.Items[i1])
	}
	c2 := fm.byItem(i2)
	if len(c2.Comments) != 1 || !strings.Contains(c2.Comments[0], "bts feature restack") {
		t.Fatalf("i2 must be asked to restack: %v", c2.Comments)
	}
	if fm.byItem(i3).Status != "todo" || !strings.Contains(fm.byItem(i3).Description, "--ref b2") {
		t.Fatalf("i3 now stacks on i2: %s\n%s", fm.byItem(i3).Status, fm.byItem(i3).Description)
	}
	if _, err := Dispatch(dir); err != nil || len(c2.Comments) != 1 {
		t.Fatalf("the restack request is made once: %v %v", c2.Comments, err)
	}
}

func TestDispatchFindsSubIssuesNotRecorded(t *testing.T) {
	fm := newFakeMultica(t)
	dir, _ := newFeature(t)
	i1, _ := AddItem(dir, ItemSpec{Title: ptr("door"), Type: ptr("build")})
	_, _ = Confirm(dir, "go")
	if _, _, err := Publish(dir, "mgr", "wrk"); err != nil {
		t.Fatal(err)
	}
	if _, err := Dispatch(dir); err != nil {
		t.Fatal(err)
	}
	// A dispatch that died after creating the sub-issue but before recording it.
	if _, err := Mutate(dir, "test", func(f *Feature) (string, error) { f.Multica.Items = nil; return "x", nil }); err != nil {
		t.Fatal(err)
	}
	creates := 0
	if _, err := Dispatch(dir); err != nil {
		t.Fatal(err)
	}
	for _, c := range fm.calls {
		if strings.HasPrefix(c, "issue create") {
			creates++
		}
	}
	f, _ := Load(dir)
	if creates != 2 || f.Multica.Items[i1] == nil {
		t.Fatalf("the sub-issue is found again by its metadata, not made twice: creates %d (parent + 1), link %+v", creates, f.Multica.Items)
	}
}
