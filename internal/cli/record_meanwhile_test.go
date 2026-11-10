package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #117: a command that runs something long read its record
// before the run and wrote that copy back after it, so what another bts
// command recorded in between was lost.  neat-wolf deferred one journey
// four seconds into a drive of another, and the drive's save would have
// dropped the deferral.
//
// In each test the long command's own run copies in the record as another
// command left it, standing in for that command writing while it ran.

// recordedMeanwhile applies write to the record at path, keeps the result
// at other, and puts the record back as it was.
func recordedMeanwhile(t *testing.T, path, other string, write func() error) {
	t.Helper()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := write(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before, after) {
		t.Fatal("the write made meanwhile changed nothing")
	}
	if err := os.WriteFile(other, after, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
}

// cpOver is the shell step that copies other over the record at path.
func cpOver(other, path string) string {
	return fmt.Sprintf("cp '%s' '%s'; ", other, path)
}

func TestProveRun_KeepsADeferralRecordedWhileItDrove(t *testing.T) {
	root, id := newBuildFixture(t)
	if _, err := executeProvePlan(root, id, []string{"review", "real-customer"}, "", ""); err != nil {
		t.Fatal(err)
	}
	path, other := state.ProvePath(root, id), filepath.Join(t.TempDir(), "prove.json")
	recordedMeanwhile(t, path, other, func() error {
		return executeProveDefer(root, id, "real-customer", "needs a customer's Drive in production", "jongyoon")
	})
	drive := cpOver(other, path) + `mkdir -p .verify && printf '{"checks":[{"check":"review","pass":true}]}' > .verify/verdict.json`
	if _, err := executeProveRun(root, id, "review", drive, nil, ".verify/verdict.json", 30, io.Discard); err != nil {
		t.Fatal(err)
	}
	rec, _ := state.LoadProveRecord(root, id)
	if rec.Journeys["real-customer"].Deferred == nil {
		t.Fatal("the deferral recorded while the drive ran was lost")
	}
	if rec.Journeys["review"].Run == nil {
		t.Fatal("the drive must be recorded")
	}
}

// evidenceWithTwo gives the mutate fixture a second falsifier and makes
// INV-001's command copy other over the evidence first.
func evidenceWithTwo(t *testing.T, root, id, other string) string {
	t.Helper()
	path := state.BuildEvidencePath(root, id)
	ev, _ := state.LoadBuildEvidence(root, id)
	ev.Falsifiers["INV-001"].Command = cpOver(other, path) + fixtureFalsifier
	ev.Falsifiers["INV-002"] = &state.Falsifier{Command: "true"}
	if err := state.SaveBuildEvidence(root, id, ev); err != nil {
		t.Fatal(err)
	}
	return path
}

func markINV002NotMutable(root, id string) func() error {
	return func() error { return executeMutateNotMutable(root, id, "INV-002", "held by the type system") }
}

func TestFalsifierCheck_KeepsWhatWasRecordedWhileItRan(t *testing.T) {
	root, id := newMutateFixture(t)
	other := filepath.Join(t.TempDir(), "build-evidence.json")
	path := evidenceWithTwo(t, root, id, other)
	recordedMeanwhile(t, path, other, markINV002NotMutable(root, id))
	if _, err := executeFalsifierCheck(root, id, false, 30, io.Discard); err != nil {
		t.Fatal(err)
	}
	ev, _ := state.LoadBuildEvidence(root, id)
	if ev.Falsifiers["INV-002"].NotMutable == "" {
		t.Fatal("the not-mutable reason recorded while the check ran was lost")
	}
	for _, inv := range []string{"INV-001", "INV-002"} {
		if ev.Falsifiers[inv].Green == nil {
			t.Fatalf("%s's run must be recorded", inv)
		}
	}
}

func TestFalsifierCheck_DoesNotPutARunOnAFalsifierRecordedAnew(t *testing.T) {
	root, id := newMutateFixture(t)
	other := filepath.Join(t.TempDir(), "build-evidence.json")
	path := evidenceWithTwo(t, root, id, other)
	const anew = "pytest -q tests/test_relay.py -k inv_001"
	recordedMeanwhile(t, path, other, func() error {
		ev, _ := state.LoadBuildEvidence(root, id)
		ev.Falsifiers["INV-001"] = &state.Falsifier{Command: anew, Red: &state.CommandRun{Command: anew, ExitCode: 1}}
		return state.SaveBuildEvidence(root, id, ev)
	})
	results, err := executeFalsifierCheck(root, id, false, 30, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	ev, _ := state.LoadBuildEvidence(root, id)
	if f := ev.Falsifiers["INV-001"]; f.Command != anew || f.Green != nil {
		t.Fatalf("a run of the old command must not land on the falsifier recorded anew: %+v", f)
	}
	if _, ok := results["INV-001"]; ok {
		t.Fatal("the old command's run is not a result of this check")
	}
	if ev.Falsifiers["INV-002"].Green == nil {
		t.Fatal("INV-002's run must still be recorded")
	}
}

func TestFalsifierMutate_KeepsWhatWasRecordedWhileItRan(t *testing.T) {
	root, id := newMutateFixture(t)
	other := filepath.Join(t.TempDir(), "build-evidence.json")
	path := evidenceWithTwo(t, root, id, other)
	recordedMeanwhile(t, path, other, markINV002NotMutable(root, id))
	if _, err := executeFalsifierMutate(root, id, mutateOptions{
		Inv: "INV-001", PatchFile: writePatch(t, root, breakGuardPatch), Expect: inv001TestID, TimeoutSec: 30,
	}, io.Discard); err != nil {
		t.Fatal(err)
	}
	ev, _ := state.LoadBuildEvidence(root, id)
	if ev.Falsifiers["INV-002"].NotMutable == "" {
		t.Fatal("the not-mutable reason recorded while the mutation ran was lost")
	}
	if m := ev.Falsifiers["INV-001"].Mutation; m == nil || !m.Killed() {
		t.Fatalf("the mutation must be recorded: %+v", m)
	}
}

func TestMutateRerun_KeepsWhatWasRecordedWhileItRan(t *testing.T) {
	root, id := newMutateFixture(t)
	other := filepath.Join(t.TempDir(), "build-evidence.json")
	path := evidenceWithTwo(t, root, id, other)
	// The first mutation's copy finds no file to copy yet and runs on.
	if _, err := executeFalsifierMutate(root, id, mutateOptions{
		Inv: "INV-001", PatchFile: writePatch(t, root, breakGuardPatch), Expect: inv001TestID, TimeoutSec: 30,
	}, io.Discard); err != nil {
		t.Fatal(err)
	}
	recordedMeanwhile(t, path, other, markINV002NotMutable(root, id))
	if _, err := executeMutateRerun(root, id, 30, io.Discard); err != nil {
		t.Fatal(err)
	}
	ev, _ := state.LoadBuildEvidence(root, id)
	if ev.Falsifiers["INV-002"].NotMutable == "" {
		t.Fatal("the not-mutable reason recorded while the rerun ran was lost")
	}
}

func TestFalsifierRepoint_KeepsWhatWasRecordedWhileItRan(t *testing.T) {
	root, id := newMutateFixture(t)
	path, other := state.BuildEvidencePath(root, id), filepath.Join(t.TempDir(), "build-evidence.json")
	ev, _ := state.LoadBuildEvidence(root, id)
	ev.Falsifiers["INV-001"].Green = &state.CommandRun{Command: fixtureFalsifier, ExitCode: 0}
	ev.Falsifiers["INV-002"] = &state.Falsifier{Command: "true"}
	if err := state.SaveBuildEvidence(root, id, ev); err != nil {
		t.Fatal(err)
	}
	recordedMeanwhile(t, path, other, markINV002NotMutable(root, id))
	moved := cpOver(other, path) + fixtureFalsifier
	if _, err := executeFalsifierRepoint(root, id, "INV-001", moved, "", 30, io.Discard); err != nil {
		t.Fatal(err)
	}
	ev, _ = state.LoadBuildEvidence(root, id)
	if ev.Falsifiers["INV-002"].NotMutable == "" {
		t.Fatal("the not-mutable reason recorded while the repoint ran was lost")
	}
	if ev.Falsifiers["INV-001"].Command != moved {
		t.Fatal("the repoint must be recorded")
	}
}

// verifiedTwoFindings verifies a round where F-001 is a1's repro (command
// given) and F-002 is a2's failure on any code.
func verifiedTwoFindings(t *testing.T, root, id string, run *state.InterrogateRun, repro, command string) {
	t.Helper()
	a1, a2 := run.Adversaries[0], run.Adversaries[1]
	_ = os.MkdirAll(filepath.Join(a1.Copy, "repro"), 0755)
	_ = os.WriteFile(filepath.Join(a1.Copy, "repro", "zero.sh"), []byte(repro), 0755)
	writeReport(t, root, id, a1, []map[string]any{
		{"title": "allow(0) is true", "command": command, "repro_files": []string{"repro/zero.sh"}},
	})
	writeReport(t, root, id, a2, []map[string]any{{"title": "always fails", "command": "sh -c 'exit 3'"}})
	if _, err := executeInterrogateVerify(root, id, 30, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func waiveF002(root, id string) func() error {
	return func() error { return executeInterrogateWaive(root, id, "F-002", "the user accepts it") }
}

func TestInterrogateRecheck_KeepsAWaiverRecordedWhileItRan(t *testing.T) {
	root, id, run := newInterrogateFixture(t)
	path, other := state.InterrogatePath(root, id), filepath.Join(t.TempDir(), "interrogate.json")
	// Verify's own rerun finds no file to copy yet and runs on.
	verifiedTwoFindings(t, root, id, run, reproScript, cpOver(other, path)+"sh repro/zero.sh")
	_ = os.WriteFile(filepath.Join(root, "src", "relay.py"), []byte(fixedCode), 0644)
	recordedMeanwhile(t, path, other, waiveF002(root, id))
	if _, err := executeInterrogateRecheck(root, id, 30, io.Discard); err != nil {
		t.Fatal(err)
	}
	r, _ := state.LoadInterrogateRun(root, id)
	if r.Findings[1].Status != state.InterrogateWaived {
		t.Fatalf("the waiver recorded while the recheck ran was lost: F-002 is %s", r.Findings[1].Status)
	}
	if r.Findings[0].Status != state.InterrogateFixed {
		t.Fatalf("F-001's recheck must be recorded: %s", r.Findings[0].Status)
	}
}

func TestInterrogatePin_KeepsAWaiverRecordedWhileItRan(t *testing.T) {
	root, id, run := newInterrogateFixture(t)
	verifiedTwoFindings(t, root, id, run, shapeRepro, "sh repro/zero.sh")
	_ = os.WriteFile(filepath.Join(root, "src", "relay.py"), []byte(fixedCode), 0644)
	path, other := state.InterrogatePath(root, id), filepath.Join(t.TempDir(), "interrogate.json")
	recordedMeanwhile(t, path, other, waiveF002(root, id))
	if _, err := executeInterrogatePin(root, id, pinOptions{
		Finding: "F-001", Command: cpOver(other, path) + pinTest, RevertFile: writePatch(t, root, revertFixDiff),
		Expect: pinTestID, TimeoutSec: 30,
	}, io.Discard); err != nil {
		t.Fatal(err)
	}
	r, _ := state.LoadInterrogateRun(root, id)
	if r.Findings[1].Status != state.InterrogateWaived {
		t.Fatalf("the waiver recorded while the pin ran was lost: F-002 is %s", r.Findings[1].Status)
	}
	if r.Findings[0].Status != state.InterrogateFixed || r.Findings[0].Pin == nil {
		t.Fatalf("the pin must be recorded: %+v", r.Findings[0])
	}
}

func TestInterrogateVerify_KeepsAWaiverRecordedWhileItRan(t *testing.T) {
	root, id, run := newInterrogateFixture(t)
	a1, a2 := run.Adversaries[0], run.Adversaries[1]
	writeReport(t, root, id, a1, []map[string]any{{"title": "always fails", "command": "sh -c 'exit 3'"}})
	writeReport(t, root, id, a2, nil)
	if _, err := executeInterrogateVerify(root, id, 30, io.Discard); err != nil {
		t.Fatal(err)
	}

	// A new code file asks for a second round, as in the rescope test.
	_ = os.WriteFile(filepath.Join(state.RecipeDir(root, id), "wireframe.md"), []byte("## File Structure\n\n| # | File | Action | Depends On | Responsibility |\n"+
		"|---|---|---|---|---|\n| 1 | `src/relay.py` | create | — | the relay |\n| 2 | `src/retry.py` | create | — | retries |\n"), 0644)
	_ = os.WriteFile(filepath.Join(root, "src", "retry.py"), []byte("def retry(): pass\n"), 0644)
	rescoped, err := executeInterrogateRescope(root, id, 1)
	if err != nil {
		t.Fatal(err)
	}
	var seat *state.Adversary
	for _, a := range rescoped.Adversaries {
		if a.InRound(2) {
			seat = a
		}
	}
	t.Cleanup(func() { _ = os.RemoveAll(seat.Copy) })

	path, other := state.InterrogatePath(root, id), filepath.Join(t.TempDir(), "interrogate.json")
	writeReport(t, root, id, seat, []map[string]any{{"title": "retry never stops", "command": cpOver(other, path) + "sh -c 'exit 4'"}})
	recordedMeanwhile(t, path, other, func() error {
		return executeInterrogateWaive(root, id, "F-001", "the user accepts it")
	})
	if _, err := executeInterrogateVerify(root, id, 30, io.Discard); err != nil {
		t.Fatal(err)
	}
	r, _ := state.LoadInterrogateRun(root, id)
	if r.Findings[0].Status != state.InterrogateWaived {
		t.Fatalf("the waiver recorded while verify ran was lost: F-001 is %s", r.Findings[0].Status)
	}
	if len(r.Findings) != 2 || r.Findings[1].Status != state.InterrogateReproduced || r.VerifiedAt == "" {
		t.Fatalf("round 2's finding must be recorded: %d findings, verified %q", len(r.Findings), r.VerifiedAt)
	}
}
