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
	mu      sync.Mutex
	n       int
	issues  map[string]*fakeIssue
	order   []string
	wakeups []string
	reruns  []string
	calls   []string
}

type fakeIssue struct {
	ID, Key, Title, Status, Parent, Assignee string
	Stage                                    int
	Meta                                     map[string]interface{}
}

func newFakeMultica(t *testing.T) *fakeMultica {
	fm := &fakeMultica{issues: map[string]*fakeIssue{}}
	t.Cleanup(multica.SetRunner(fm.run))
	return fm
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
	fm.calls = append(fm.calls, strings.Join(args, " "))
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

	// i1 done: i2 starts.  i3 skipped by the user: cancelled.
	writeAttempt(t, dir, i1, 1, "complete", "done", "gate")
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

	// i2 cancelled by its worker: claimable, restarted with no active run.
	writeAttempt(t, dir, i2, 1, "cancelled", "cancelled", "")
	c2.Status = "cancelled"
	if _, err := Dispatch(dir); err != nil {
		t.Fatal(err)
	}
	if c2.Status != "todo" || len(fm.reruns) != 1 || fm.reruns[0] != c2.ID {
		t.Fatalf("a claimable item restarts: %s %v", c2.Status, fm.reruns)
	}

	// Every item settled: the parent goes to review.
	writeAttempt(t, dir, i2, 2, "complete", "done", "gate")
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
