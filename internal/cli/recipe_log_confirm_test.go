package cli

import (
	"fmt"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #105: after the one review completed (a round with no
// critical or major), fixing its minors edited final.md, Rule 3 asked for
// another delta, and every delta found new minors.  cool-crow ran rounds 2,
// 3 and 4 that way and swift-eagle 5 and 6.  A confirm round checks only
// the cited minors' fixes and raises nothing new, so the loop ends there.

func confirmBlock(critical, major, minor int, findings, confirmedFixed string) string {
	return fmt.Sprintf("# Verification\n\n<bts-findings>\n{\n"+
		"  \"critical\": %d, \"major\": %d, \"minor_resolvable\": %d, \"minor_deferred\": 0, \"info\": 0,\n"+
		"  \"paths_total\": 3, \"paths_unspecified\": 0,\n"+
		"  \"findings\": [%s],\n  \"confirmed_fixed\": [%s]\n}\n</bts-findings>\n",
		critical, major, minor, findings, confirmedFixed)
}

func buildRecipeForConfirm(t *testing.T, id string) string {
	t.Helper()
	root := newRecipeFixture(t, id, "falsify", 0, 0, nil)
	r, err := state.LoadRecipeState(root, id)
	if err != nil {
		t.Fatal(err)
	}
	r.Type = "build"
	if err := state.SaveRecipeState(root, r); err != nil {
		t.Fatal(err)
	}
	writeRecipeFile(t, root, id, "final.md", "# Build spec\n\nfirst\n")
	return root
}

func logFull(t *testing.T, root, id, block string) {
	t.Helper()
	v := writeRecipeFile(t, root, id, "verification.md", block)
	runRecipeLog(t, root, id, "--from-verification", v, "--doc", "final.md", "--scope", "full", "--dimension", "verify")
}

func logConfirm(t *testing.T, root, id, block string) string {
	t.Helper()
	v := writeRecipeFile(t, root, id, "verification.md", block)
	return runRecipeLog(t, root, id, "--from-verification", v, "--doc", "final.md", "--scope", "confirm", "--dimension", "verify")
}

func TestRecipeLog_ConfirmRoundEndsTheReview(t *testing.T) {
	root := buildRecipeForConfirm(t, "r-c1")
	logFull(t, root, "r-c1", confirmBlock(0, 0, 1,
		`{"severity": "minor_resolvable", "title": "U-002 names no command", "anchor": "U-002"}`, ""))

	writeRecipeFile(t, root, "r-c1", "final.md", "# Build spec\n\nfirst, U-002 fixed\n")
	id := state.FindingID("final.md", "U-002 names no command")
	out := logConfirm(t, root, "r-c1", confirmBlock(0, 0, 0, "", `"`+id+`"`))

	entries, _ := state.ReadVerifyLog(root, "r-c1")
	if len(entries) != 2 {
		t.Fatalf("the confirm round records, got %d entries:\n%s", len(entries), out)
	}
	if e := entries[1]; e.Scope != "confirm" || e.FullPass {
		t.Fatalf("recorded as scope %q, full %v; want a confirm round", e.Scope, e.FullPass)
	}
	if dirty, _ := state.DirtyVerifiedDocs(root, "r-c1"); len(dirty) != 0 {
		t.Fatalf("a clean confirm round absorbs the minors' fix, got dirty %v", dirty)
	}
}

// Trial problem #122: an ID a round lists as confirmed_fixed that is
// already fixed was reported as "[bts] warning: confirmed_fixed left as
// they were: F-... (already fixed)" on cool-crow (11-07 21:33Z).  Nothing
// is wrong with it, and a warning invites the agent to act.
func TestRecipeLog_AnAlreadyFixedConfirmedIDIsNoWarning(t *testing.T) {
	root := buildRecipeForConfirm(t, "r-c122")
	logFull(t, root, "r-c122", confirmBlock(0, 0, 1,
		`{"severity": "minor_resolvable", "title": "U-002 names no command", "anchor": "U-002"}`, ""))
	writeRecipeFile(t, root, "r-c122", "final.md", "# Build spec\n\nfirst, U-002 fixed\n")
	id := state.FindingID("final.md", "U-002 names no command")
	logConfirm(t, root, "r-c122", confirmBlock(0, 0, 0, "", `"`+id+`"`))

	writeRecipeFile(t, root, "r-c122", "final.md", "# Build spec\n\nfirst, U-002 fixed, reworded\n")
	v := writeRecipeFile(t, root, "r-c122", "verification.md", confirmBlock(0, 0, 0, "", `"`+id+`"`)+"\nDelta round after a rewording.\n")
	out := runRecipeLog(t, root, "r-c122", "--from-verification", v, "--doc", "final.md", "--scope", "delta", "--dimension", "verify")
	if strings.Contains(out, "warning: confirmed_fixed") {
		t.Fatalf("an ID that is already fixed is no warning:\n%s", out)
	}
	if !strings.Contains(out, id+" (already fixed)") {
		t.Fatalf("the ID is still named, as already fixed:\n%s", out)
	}
}

func TestRecipeLog_ConfirmFollowsACompletingReview(t *testing.T) {
	root := buildRecipeForConfirm(t, "r-c2")
	logFull(t, root, "r-c2", confirmBlock(0, 1, 0,
		`{"severity": "major", "title": "the switch has no answer for a failed start", "anchor": "§3"}`, ""))

	writeRecipeFile(t, root, "r-c2", "final.md", "# Build spec\n\nfirst, fixed\n")
	out := logConfirm(t, root, "r-c2", confirmBlock(0, 0, 0, "", ""))

	entries, _ := state.ReadVerifyLog(root, "r-c2")
	if len(entries) != 1 {
		t.Fatalf("a major still needs a delta review, so confirm is refused; got %d entries", len(entries))
	}
	if !strings.Contains(out, "confirm") || !strings.Contains(out, "major") {
		t.Errorf("the refusal says why, got:\n%s", out)
	}
}

func TestRecipeLog_ConfirmRaisesNoNewMinor(t *testing.T) {
	root := buildRecipeForConfirm(t, "r-c3")
	logFull(t, root, "r-c3", confirmBlock(0, 0, 1,
		`{"severity": "minor_resolvable", "title": "U-002 names no command", "anchor": "U-002"}`, ""))
	writeRecipeFile(t, root, "r-c3", "final.md", "# Build spec\n\nfirst, edited\n")

	out := logConfirm(t, root, "r-c3", confirmBlock(0, 0, 1,
		`{"severity": "minor_resolvable", "title": "U-004 cites a stale path", "anchor": "U-004"}`, ""))
	entries, _ := state.ReadVerifyLog(root, "r-c3")
	if len(entries) != 1 {
		t.Fatalf("a confirm round with a new minor is refused, got %d entries:\n%s", len(entries), out)
	}
	if !strings.Contains(out, "U-004 cites a stale path") {
		t.Errorf("the refusal names the new finding, got:\n%s", out)
	}

	// The cited minor reported again, unfixed, is what a confirm round may
	// say.  (The summary line keeps this verification.md apart from the
	// full round's: an identical file is refused as a re-record, #99.)
	out = logConfirm(t, root, "r-c3", confirmBlock(0, 0, 1,
		`{"severity": "minor_resolvable", "title": "U-002 names no command", "anchor": "U-002"}`, "")+
		"\nConfirm pass: U-002 is not fixed yet.\n")
	entries, _ = state.ReadVerifyLog(root, "r-c3")
	if len(entries) != 2 {
		t.Fatalf("re-reporting the unfixed cited minor records, got %d entries:\n%s", len(entries), out)
	}
}
