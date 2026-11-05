package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// setupBuildRoot is a build recipe ready to finalize except for what each
// test leaves out: domain.md with two invariants, a wireframe naming one
// code file, the skeleton file, and final.md.
func setupBuildRoot(t *testing.T) (root, id string) {
	t.Helper()
	root, id = setupStopRoot(t)
	recipe, _ := state.LoadRecipeState(root, id)
	recipe.Type = "build"
	recipe.Phase = "falsify"
	if err := state.SaveRecipeState(root, recipe); err != nil {
		t.Fatal(err)
	}
	dir := state.RecipeDir(root, id)
	write := func(p, s string) {
		t.Helper()
		_ = os.MkdirAll(filepath.Dir(p), 0755)
		if err := os.WriteFile(p, []byte(s), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "domain.md"), "## 2. Invariants\n\n| ID | Statement | Owner |\n|---|---|---|\n"+
		"| INV-001 | one issue per PR | relay |\n| INV-002 | never post with the user token | relay |\n")
	write(filepath.Join(dir, "wireframe.md"), "## File Structure\n\n| # | File | Action | Depends On | Responsibility |\n"+
		"|---|---|---|---|---|\n| 1 | `src/relay.py` | create | — | the relay |\n")
	write(filepath.Join(root, "src", "relay.py"), "def handle(e): raise NotImplementedError\n")
	write(filepath.Join(dir, "final.md"), "# Build spec\n")
	return root, id
}

func recordEvidence(t *testing.T, root, id string, skeletonExit int, redInvs ...string) {
	t.Helper()
	hash, err := engine.CodeHash(root, id)
	if err != nil || hash == "" {
		t.Fatalf("code hash: %q, %v", hash, err)
	}
	ev := &state.BuildEvidence{
		Skeleton:   &state.CommandRun{Command: "mypy src", ExitCode: skeletonExit, CodeHash: hash},
		Falsifiers: map[string]*state.Falsifier{},
	}
	for _, inv := range redInvs {
		ev.Falsifiers[inv] = &state.Falsifier{Command: "pytest -k " + inv, Red: &state.CommandRun{Command: "pytest", ExitCode: 1, CodeHash: hash}}
	}
	if err := state.SaveBuildEvidence(root, id, ev); err != nil {
		t.Fatal(err)
	}
}

func buildDone(t *testing.T, root string) *HookOutput {
	t.Helper()
	out, err := NewStopHandler().Handle(&HookInput{CWD: root, StopHookContent: "<bts>DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	return out
}

func TestBuildSpecDone_BlocksWithoutSkeletonBuild(t *testing.T) {
	root, _ := setupBuildRoot(t)
	if out := buildDone(t, root); out.Decision != "block" || !strings.Contains(out.Reason, "No skeleton build") {
		t.Fatalf("expected skeleton_builds block, got %q %q", out.Decision, out.Reason)
	}
}

func TestBuildSpecDone_BlocksAnInvariantWithoutARedFalsifier(t *testing.T) {
	root, id := setupBuildRoot(t)
	recordEvidence(t, root, id, 0, "INV-001")
	if out := buildDone(t, root); out.Decision != "block" || !strings.Contains(out.Reason, "INV-002") {
		t.Fatalf("expected falsifiers_red block naming INV-002, got %q %q", out.Decision, out.Reason)
	}
}

func TestBuildSpecDone_BlocksWhenTheSkeletonChangedAfterItsBuild(t *testing.T) {
	root, id := setupBuildRoot(t)
	recordEvidence(t, root, id, 0, "INV-001", "INV-002")
	_ = os.WriteFile(filepath.Join(root, "src", "relay.py"), []byte("def handle(e, extra): raise NotImplementedError\n"), 0644)
	if out := buildDone(t, root); out.Decision != "block" || !strings.Contains(out.Reason, "changed after its last build") {
		t.Fatalf("expected a stale skeleton build to block, got %q %q", out.Decision, out.Reason)
	}
}

func TestBuildSpecDone_NeedsOneCleanReviewOfFinal(t *testing.T) {
	root, id := setupBuildRoot(t)
	recordEvidence(t, root, id, 0, "INV-001", "INV-002")

	if out := buildDone(t, root); out.Decision != "block" || !strings.Contains(out.Reason, "No review of final.md") {
		t.Fatalf("expected build_review_clean to ask for a review, got %q %q", out.Decision, out.Reason)
	}

	writeVerifyLog(t, root, id, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "final.md", FullPass: true, Critical: 0, Major: 1, Status: "continue"},
	})
	if out := buildDone(t, root); out.Decision != "block" || !strings.Contains(out.Reason, "1 major") {
		t.Fatalf("expected a major in the review to block, got %q %q", out.Decision, out.Reason)
	}

	// Minors do not hold the build recipe: they are fixed or declared, and
	// nothing waits for a replicated clean pass.
	writeVerifyLog(t, root, id, []state.VerifyLogEntry{
		{Iteration: 2, Doc: "final.md", FullPass: true, Critical: 0, Major: 0, MinorResolvable: 3, Status: "continue"},
	})
	out := buildDone(t, root)
	if out.Decision == "block" {
		t.Fatalf("one clean review with the evidence in place must finalize: %s", out.Reason)
	}
	if r, _ := state.LoadRecipeState(root, id); r.Phase != "finalize" {
		t.Fatalf("phase must be finalize, got %q", r.Phase)
	}
}

func TestBuildSpecDone_DoesNotUseTheBlueprintLoop(t *testing.T) {
	root, id := setupBuildRoot(t)
	recordEvidence(t, root, id, 0, "INV-001", "INV-002")
	writeVerifyLog(t, root, id, []state.VerifyLogEntry{
		{Iteration: 1, Doc: "final.md", FullPass: true, Dimensions: []string{"verify"}, Status: "continue"},
	})
	// No audit, no simulate, no replicated pass, no draft.md: the blueprint
	// gates would all block this; the build recipe does not ask for them.
	if out := buildDone(t, root); out.Decision == "block" {
		t.Fatalf("a build recipe must not be held to the blueprint loop: %s", out.Reason)
	}
}

func TestImplementDone_BuildRecipeNeedsGreenFalsifiers(t *testing.T) {
	root, id := setupImplementRoot(t)
	recipe, _ := state.LoadRecipeState(root, id)
	recipe.Type = "build"
	_ = state.SaveRecipeState(root, recipe)
	dir := state.RecipeDir(root, id)
	_ = os.WriteFile(filepath.Join(dir, "domain.md"), []byte("## 2. Invariants\n\n| ID | Statement | Owner |\n|---|---|---|\n| INV-001 | x | a |\n"), 0644)
	_ = os.MkdirAll(filepath.Join(root, "src"), 0755)
	_ = os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("package a\n"), 0644)

	hash, _ := engine.CodeHash(root, id)
	ev := &state.BuildEvidence{Falsifiers: map[string]*state.Falsifier{
		"INV-001": {Command: "go test -run Inv001", Red: &state.CommandRun{ExitCode: 1}, Green: &state.CommandRun{ExitCode: 1, CodeHash: hash}},
	}}
	_ = state.SaveBuildEvidence(root, id, ev)

	out, err := NewStopHandler().Handle(&HookInput{CWD: root, StopHookContent: "<bts>IMPLEMENT DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision != "block" || !strings.Contains(out.Reason, "INV-001 not passing") {
		t.Fatalf("expected falsifiers_green block, got %q %q", out.Decision, out.Reason)
	}

	ev.Falsifiers["INV-001"].Green = &state.CommandRun{ExitCode: 0, CodeHash: hash}
	ev.Falsifiers["INV-001"].Mutation = &state.MutationRun{ExitCode: 1, Matched: true, BaseHash: hash}
	_ = state.SaveBuildEvidence(root, id, ev)
	out, _ = NewStopHandler().Handle(&HookInput{CWD: root, StopHookContent: "<bts>IMPLEMENT DONE</bts>"})
	if out.Decision == "block" {
		t.Fatalf("green falsifiers on the current code must pass: %s", out.Reason)
	}
}

func TestImplementDone_BuildRecipeNeedsFalsifiersAbleToFail(t *testing.T) {
	root, id := setupImplementRoot(t)
	recipe, _ := state.LoadRecipeState(root, id)
	recipe.Type = "build"
	_ = state.SaveRecipeState(root, recipe)
	dir := state.RecipeDir(root, id)
	_ = os.WriteFile(filepath.Join(dir, "domain.md"), []byte("## 2. Invariants\n\n| ID | Statement | Owner |\n|---|---|---|\n"+
		"| INV-001 | x | a |\n| INV-002 | y | a |\n"), 0644)
	_ = os.MkdirAll(filepath.Join(root, "src"), 0755)
	_ = os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("package a\n"), 0644)
	hash, _ := engine.CodeHash(root, id)
	green := func() *state.CommandRun { return &state.CommandRun{ExitCode: 0, CodeHash: hash} }
	ev := &state.BuildEvidence{Falsifiers: map[string]*state.Falsifier{
		"INV-001": {Command: "t1", Green: green()},
		"INV-002": {Command: "t2", Green: green()},
	}}
	done := func() *HookOutput {
		t.Helper()
		_ = state.SaveBuildEvidence(root, id, ev)
		out, err := NewStopHandler().Handle(&HookInput{CWD: root, StopHookContent: "<bts>IMPLEMENT DONE</bts>"})
		if err != nil {
			t.Fatalf("handle: %v", err)
		}
		return out
	}

	if out := done(); out.Decision != "block" || !strings.Contains(out.Reason, "no mutation run for INV-001, INV-002") {
		t.Fatalf("green alone must not finish: %q %q", out.Decision, out.Reason)
	}

	// A mutation the falsifier survived, or one that failed somewhere other
	// than the expected test, shows nothing about the invariant.
	ev.Falsifiers["INV-001"].Mutation = &state.MutationRun{ExitCode: 0, BaseHash: hash}
	ev.Falsifiers["INV-002"].Mutation = &state.MutationRun{ExitCode: 2, Matched: false, BaseHash: hash}
	if out := done(); out.Decision != "block" || !strings.Contains(out.Reason, "with the guard broken for INV-001, INV-002") {
		t.Fatalf("survived and unmatched mutations must block: %q", out.Reason)
	}

	// A killed mutation run on older code is stale.
	ev.Falsifiers["INV-001"].Mutation = &state.MutationRun{ExitCode: 1, Matched: true, BaseHash: "sha256:older"}
	ev.Falsifiers["INV-002"].Mutation = nil
	ev.Falsifiers["INV-002"].NotMutable = "held by the type system"
	if out := done(); out.Decision != "block" || !strings.Contains(out.Reason, "code changed after the mutation runs for INV-001") {
		t.Fatalf("a mutation run on older code must block: %q", out.Reason)
	}

	ev.Falsifiers["INV-001"].Mutation.BaseHash = hash
	if out := done(); out.Decision == "block" {
		t.Fatalf("a killed mutation and a recorded not-mutable reason must pass: %s", out.Reason)
	}
}

// writeCarriedDomain gives a build fixture a change recipe's domain.md:
// INV-001 is introduced, INV-002 is carried from earlier work (trial #41).
func writeCarriedDomain(t *testing.T, root, id string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(state.RecipeDir(root, id), "domain.md"), []byte(
		"## 2. Invariants\n\n| ID | Statement | Owner |\n|---|---|---|\n"+
			"| INV-001 | one issue per PR | relay |\n| INV-002 [carried: r-002 INV-008] | an issued token still pages | cursor |\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

// Trial problem #41: falsifiers_red demanded a red test for an invariant
// the change keeps.  A carried row is not required red; it needs its
// existing test recorded passing on the skeleton as it is now.
func TestBuildSpecDone_CarriedInvariantIsGreenNotRed(t *testing.T) {
	root, id := setupBuildRoot(t)
	writeCarriedDomain(t, root, id)
	hash, _ := engine.CodeHash(root, id)
	ev := &state.BuildEvidence{
		Skeleton: &state.CommandRun{Command: "mypy src", ExitCode: 0, CodeHash: hash},
		Falsifiers: map[string]*state.Falsifier{
			"INV-001": {Command: "pytest -k inv_001", Red: &state.CommandRun{ExitCode: 1, CodeHash: hash}},
		},
	}
	done := func() *HookOutput {
		t.Helper()
		if err := state.SaveBuildEvidence(root, id, ev); err != nil {
			t.Fatal(err)
		}
		return buildDone(t, root)
	}

	if out := done(); out.Decision != "block" || !strings.Contains(out.Reason, "INV-002") || !strings.Contains(out.Reason, "passing test") {
		t.Fatalf("a carried invariant with no recorded test must block: %q %q", out.Decision, out.Reason)
	}
	ev.Falsifiers["INV-002"] = &state.Falsifier{Command: "pytest -k old_token", Green: &state.CommandRun{ExitCode: 1, CodeHash: hash}}
	if out := done(); out.Decision != "block" || !strings.Contains(out.Reason, "INV-002") {
		t.Fatalf("a carried invariant failing on the skeleton must block: %q", out.Reason)
	}
	ev.Falsifiers["INV-002"].Green = &state.CommandRun{ExitCode: 0, CodeHash: "sha256:older"}
	if out := done(); out.Decision != "block" || !strings.Contains(out.Reason, "falsifier check "+id+" --red") {
		t.Fatalf("a carried pass on an older skeleton must block: %q", out.Reason)
	}
	ev.Falsifiers["INV-002"].Green = &state.CommandRun{ExitCode: 0, CodeHash: hash}
	if out := done(); out.Decision != "block" || !strings.Contains(out.Reason, "No review of final.md") {
		t.Fatalf("one red introduced invariant and one green carried one pass the falsifier gate: %q", out.Reason)
	}
}

// At IMPLEMENT DONE a carried invariant must still pass on the code as it
// is now (falsifiers_green), but it introduced no guard of this change's
// to break, so falsifiers_able_to_fail does not ask for a mutation.
func TestImplementDone_CarriedInvariantMustPassButNeedsNoMutation(t *testing.T) {
	root, id := setupImplementRoot(t)
	recipe, _ := state.LoadRecipeState(root, id)
	recipe.Type = "build"
	_ = state.SaveRecipeState(root, recipe)
	writeCarriedDomain(t, root, id)
	_ = os.MkdirAll(filepath.Join(root, "src"), 0755)
	_ = os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("package a\n"), 0644)
	hash, _ := engine.CodeHash(root, id)
	ev := &state.BuildEvidence{Falsifiers: map[string]*state.Falsifier{
		"INV-001": {Command: "t1", Red: &state.CommandRun{ExitCode: 1}, Green: &state.CommandRun{ExitCode: 0, CodeHash: hash},
			Mutation: &state.MutationRun{ExitCode: 1, Matched: true, BaseHash: hash}},
		"INV-002": {Command: "t2", Green: &state.CommandRun{ExitCode: 1, CodeHash: hash}},
	}}
	done := func() *HookOutput {
		t.Helper()
		_ = state.SaveBuildEvidence(root, id, ev)
		out, err := NewStopHandler().Handle(&HookInput{CWD: root, StopHookContent: "<bts>IMPLEMENT DONE</bts>"})
		if err != nil {
			t.Fatalf("handle: %v", err)
		}
		return out
	}

	if out := done(); out.Decision != "block" || !strings.Contains(out.Reason, "INV-002 not passing") {
		t.Fatalf("a carried invariant that no longer passes must block: %q %q", out.Decision, out.Reason)
	}
	ev.Falsifiers["INV-002"].Green = &state.CommandRun{ExitCode: 0, CodeHash: hash}
	if out := done(); out.Decision == "block" {
		t.Fatalf("a carried invariant green on the current code needs no mutation: %s", out.Reason)
	}
}

// Trial problem #66: nothing recorded that generated files (an OpenAPI
// document, client types) must be regenerated after the code they come
// from changed.  When the wireframe has a `generate` row, IMPLEMENT DONE
// needs a generated check on the current code, for every recipe type.
func TestImplementDone_GenerateRowNeedsAGeneratedCheck(t *testing.T) {
	root, id := setupImplementRoot(t)
	_ = os.WriteFile(filepath.Join(state.RecipeDir(root, id), "wireframe.md"), []byte(
		"## File Structure\n\n| File | Action | Depends On | Responsibility |\n|---|---|---|---|\n"+
			"| src/a.go | create | - | the code |\n"+
			"| gen/api.json | generate | src/a.go | the contract (generator: `mkdir -p gen && cp src/a.go gen/api.json`) |\n"), 0644)
	_ = os.MkdirAll(filepath.Join(root, "src"), 0755)
	_ = os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("package a\n"), 0644)
	recordImplHash(t, root, id)

	out, err := NewStopHandler().Handle(&HookInput{CWD: root, StopHookContent: "<bts>IMPLEMENT DONE</bts>"})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if out.Decision != "block" || !strings.Contains(out.Reason, "bts check generated "+id) {
		t.Fatalf("a generate row with no generated check must block: %q %q", out.Decision, out.Reason)
	}
}

// The generated check counts only while the code and the generated files
// are as they were when the generators ran: a code change or a hand edit
// of a generated file makes it stale (trial problem #66).
func TestImplementDone_GeneratedCheckMustBeCurrent(t *testing.T) {
	root, id := setupImplementRoot(t)
	_ = os.WriteFile(filepath.Join(state.RecipeDir(root, id), "wireframe.md"), []byte(
		"## File Structure\n\n| File | Action | Depends On | Responsibility |\n|---|---|---|---|\n"+
			"| src/a.go | create | - | the code |\n"+
			"| gen/api.json | generate | src/a.go | the contract (generator: `make api`) |\n"), 0644)
	_ = os.MkdirAll(filepath.Join(root, "src"), 0755)
	_ = os.MkdirAll(filepath.Join(root, "gen"), 0755)
	_ = os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("package a\n"), 0644)
	_ = os.WriteFile(filepath.Join(root, "gen", "api.json"), []byte("{}\n"), 0644)

	record := func() {
		t.Helper()
		recordImplHash(t, root, id)
		code, _ := engine.CodeHash(root, id)
		_, files, err := engine.GeneratedFiles(root, []string{"gen/api.json"})
		if err != nil {
			t.Fatal(err)
		}
		_ = state.SaveBuildEvidence(root, id, &state.BuildEvidence{Generated: &state.GeneratedRun{
			Runs:  []*state.CommandRun{{Command: "make api", ExitCode: 0, CodeHash: code}},
			Files: []string{"gen/api.json"}, FilesHash: files, CodeHash: code,
		}})
	}
	done := func() *HookOutput {
		t.Helper()
		out, err := NewStopHandler().Handle(&HookInput{CWD: root, StopHookContent: "<bts>IMPLEMENT DONE</bts>"})
		if err != nil {
			t.Fatalf("handle: %v", err)
		}
		return out
	}

	// The passing case comes last: a passing IMPLEMENT DONE completes the
	// recipe, and the hook does not judge a completed one again.
	record()
	_ = os.WriteFile(filepath.Join(root, "gen", "api.json"), []byte("{\"edited\": true}\n"), 0644)
	if out := done(); out.Decision != "block" || !strings.Contains(out.Reason, "never edited by hand") {
		t.Fatalf("a hand edit after the check must block: %q", out.Reason)
	}

	record()
	_ = os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("package a\n\nconst X = 1\n"), 0644)
	recordImplHash(t, root, id)
	if out := done(); out.Decision != "block" || !strings.Contains(out.Reason, "code changed after the generators") {
		t.Fatalf("a code change after the check must block: %q", out.Reason)
	}

	record()
	if out := done(); out.Decision == "block" {
		t.Fatalf("a current generated check must pass: %s", out.Reason)
	}
}
