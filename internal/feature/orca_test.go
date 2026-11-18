package feature

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/orca"
)

// fakeOrca answers worker-start and send the way the Orca CLI's --json
// envelope does, and records each call.
type fakeOrca struct {
	mu     sync.Mutex
	starts [][]string
	sends  [][]string
	dirs   []string
}

func newFakeOrca(t *testing.T) *fakeOrca {
	fo := &fakeOrca{}
	t.Cleanup(orca.SetRunner(fo.run))
	return fo
}

func (fo *fakeOrca) run(_ context.Context, dir string, args ...string) ([]byte, error) {
	fo.mu.Lock()
	defer fo.mu.Unlock()
	fo.dirs = append(fo.dirs, dir)
	reply := func(v any) []byte {
		b, _ := json.Marshal(map[string]any{"ok": true, "result": v})
		return b
	}
	switch strings.Join(args[:2], " ") {
	case "orchestration worker-start":
		fo.starts = append(fo.starts, args)
		n := itoa(len(fo.starts))
		return reply(map[string]any{"dispatchId": "ctx_" + n, "taskId": "task_" + n, "state": "ready",
			"run":     map[string]any{"id": "run_1", "created": len(fo.starts) == 1},
			"effects": []map[string]any{{"kind": "worktree", "id": "ws_" + n}, {"kind": "terminal", "id": "term_" + n}}}), nil
	case "orchestration send":
		fo.sends = append(fo.sends, args)
		return reply(map[string]any{"messageId": "msg_1"}), nil
	}
	b, _ := json.Marshal(map[string]any{"ok": false, "error": map[string]string{"code": "unknown", "message": "unexpected " + strings.Join(args, " ")}})
	return b, nil
}

// startedFor returns the worker-start call that started item, or nil.
func (fo *fakeOrca) startedFor(f *Feature, item string) []string {
	for _, a := range fo.starts {
		if flag(a, "--name") == orcaWorktreeName(f, item) {
			return a
		}
	}
	return nil
}

// Each ready item starts as a worker in a child worktree on the base
// chooseBase picks; the first start's Run is joined by the later ones; a
// worker is started once; a merge asks the stacked item's worker, once, to
// restack; and the brief the worker gets names its feature, item and base.
func TestDispatchOrca_StartsStacksAndRestacks(t *testing.T) {
	fm := newFakeMultica(t) // its gh answers the pull request reads
	fo := newFakeOrca(t)
	dir, _ := newFeature(t)
	i1, _ := AddItem(dir, ItemSpec{Title: ptr("one"), Type: ptr("build")})
	i2, _ := AddItem(dir, ItemSpec{Title: ptr("two"), Type: ptr("build"), DependsOn: &[]string{i1}})
	i3, _ := AddItem(dir, ItemSpec{Title: ptr("three"), Type: ptr("build"), DependsOn: &[]string{i2}})
	if _, err := Confirm(dir, "go"); err != nil {
		t.Fatal(err)
	}
	opts := OrcaDispatchOptions{Dir: "/coord"}

	if _, err := DispatchOrca(dir, opts); err != nil {
		t.Fatal(err)
	}
	f, _ := Load(dir)
	a := fo.startedFor(f, i1)
	if a != nil && flag(a, "--name") != strings.SplitN(f.ID, "-", 3)[0]+"-"+strings.SplitN(f.ID, "-", 3)[1]+"-"+i1 {
		t.Errorf("the worktree is named f-NNN-<item>, got %q", flag(a, "--name"))
	}
	if len(fo.starts) != 1 || a == nil || flag(a, "--base-branch") != "main" || flag(a, "--worktree") != "new-child" ||
		flag(a, "--agent") != "claude" || flag(a, "--run") != "" || fo.dirs[0] != "/coord" {
		t.Fatalf("one worker for i1 on main, from the coordinator's directory: %q", fo.starts)
	}
	if f.Orca == nil || f.Orca.Run != "run_1" || f.Orca.Items[i1].Dispatch != "ctx_1" || f.Orca.Items[i1].Workspace != "ws_1" {
		t.Fatalf("the Run and i1's worker must be recorded: %+v", f.Orca)
	}
	brief, err := os.ReadFile(flag(a, "--spec-file"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"bts recipe create --feature " + f.ID + " --item " + i1, "made from `main`", "bts feature pr " + f.ID + " " + i1,
		"bts recipe finish", "subagents are part of this task"} {
		if !strings.Contains(string(brief), want) {
			t.Errorf("the brief must contain %q:\n%s", want, brief)
		}
	}

	if _, err := DispatchOrca(dir, opts); err != nil || len(fo.starts) != 1 {
		t.Fatalf("a started item is not started again: %d starts, %v", len(fo.starts), err)
	}

	writeAttempt(t, dir, i1, 1, "complete", "done", "gate")
	u1 := writePR(t, dir, i1, 1, "b1", "main")
	if _, err := DispatchOrca(dir, opts); err != nil {
		t.Fatal(err)
	}
	f, _ = Load(dir)
	if a := fo.startedFor(f, i2); a == nil || flag(a, "--base-branch") != "b1" || flag(a, "--run") != "run_1" {
		t.Fatalf("i2 stacks on i1's branch and joins the Run: %q", fo.starts)
	}
	if b := f.Orca.Items[i2]; b.BaseItem != i1 || b.Base != "b1" {
		t.Fatalf("i2's base must be recorded: %+v", b)
	}

	writeAttempt(t, dir, i2, 1, "complete", "done", "gate")
	writePR(t, dir, i2, 1, "b2", "b1")
	acts, err := DispatchOrca(dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	f, _ = Load(dir)
	if fo.startedFor(f, i3) != nil || !strings.Contains(actionText(acts), i3+": waits: stacking on "+i2+" would make 3") {
		t.Fatalf("i3 waits for a merge (stack limit):\n%s", actionText(acts))
	}

	fm.prStates[u1] = "MERGED"
	if _, err := DispatchOrca(dir, opts); err != nil {
		t.Fatal(err)
	}
	f, _ = Load(dir)
	if m := f.Orca.Items[i1].Merged; m == nil || m.SHA != "abc123merge" {
		t.Fatalf("i1's merge must be recorded: %+v", f.Orca.Items[i1])
	}
	if len(fo.sends) != 1 || flag(fo.sends[0], "--to") != "dispatch:"+f.Orca.Items[i2].Dispatch ||
		!strings.Contains(flag(fo.sends[0], "--body"), "bts feature restack "+f.ID+" "+i2) {
		t.Fatalf("i2's worker must be asked to restack: %q", fo.sends)
	}
	if a := fo.startedFor(f, i3); a == nil || flag(a, "--base-branch") != "b2" {
		t.Fatalf("i3 now stacks on i2: %q", fo.starts)
	}
	if _, err := DispatchOrca(dir, opts); err != nil || len(fo.sends) != 1 {
		t.Fatalf("the restack request is made once: %q, %v", fo.sends, err)
	}
}

func actionText(acts []Action) string {
	var lines []string
	for _, a := range acts {
		lines = append(lines, a.Item+": "+a.What)
	}
	return strings.Join(lines, "\n")
}

// A dry run reports what would start and changes nothing; an item whose
// attempt was cancelled gets a new worker on the base it was given; and a
// feature dispatched to Multica is refused.
func TestDispatchOrca_DryRunRestartAndOneBackend(t *testing.T) {
	newFakeMultica(t)
	fo := newFakeOrca(t)
	dir, _ := newFeature(t)
	i1, _ := AddItem(dir, ItemSpec{Title: ptr("one"), Type: ptr("build")})
	if _, err := Confirm(dir, "go"); err != nil {
		t.Fatal(err)
	}

	acts, err := DispatchOrca(dir, OrcaDispatchOptions{Dir: "/coord", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	f, _ := Load(dir)
	if len(fo.starts) != 0 || f.Orca != nil || !strings.Contains(actionText(acts), i1+": would be started on main") {
		t.Fatalf("a dry run starts and saves nothing: %d starts, %+v\n%s", len(fo.starts), f.Orca, actionText(acts))
	}

	if _, err := DispatchOrca(dir, OrcaDispatchOptions{Dir: "/coord"}); err != nil {
		t.Fatal(err)
	}
	writeAttempt(t, dir, i1, 1, "cancelled", "cancelled", "gate")
	if _, err := DispatchOrca(dir, OrcaDispatchOptions{Dir: "/coord"}); err != nil {
		t.Fatal(err)
	}
	f, _ = Load(dir)
	if oi := f.Orca.Items[i1]; len(fo.starts) != 2 || oi.Starts != 2 || oi.Dispatch != "ctx_2" || oi.Base != "main" {
		t.Fatalf("a cancelled attempt gets a new worker on its base: %d starts, %+v", len(fo.starts), oi)
	}

	other, _ := newFeature(t)
	j1, _ := AddItem(other, ItemSpec{Title: ptr("one"), Type: ptr("build")})
	_, _ = Confirm(other, "go")
	if _, err := Mutate(other, "test", func(f *Feature) (string, error) {
		f.Multica = &MulticaLink{Parent: "p", Items: map[string]*MulticaItem{j1: {Issue: "x"}}}
		return "published", nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := DispatchOrca(other, OrcaDispatchOptions{Dir: "/coord"}); err == nil || !strings.Contains(err.Error(), "runs on Multica") {
		t.Fatalf("a Multica feature must be refused, got %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, ItemsDir, i1, OrcaBriefFile)); err != nil || !strings.Contains(string(b), "item "+i1) {
		t.Fatalf("the brief is kept beside the item's attempts: %v", err)
	}
}
