package cli

import (
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #99: on swift-eagle the orchestrator's Write of a delta
// review's output failed after a compaction, in the same batch as the
// record, so `bts recipe log --from-verification` read round 1's file
// again.  Round 2 went into the log as a delta that "found 1 critical and
// 3 major", cost one of six rounds, forced an extra full review, and could
// not be taken back.  A file already recorded for this document is not a
// new reading of it.
func TestRecipeLog_RefusesAVerificationFileAlreadyRecorded(t *testing.T) {
	root := newRecipeFixture(t, "r-v99", "falsify", 0, 0, nil)
	writeRecipeFile(t, root, "r-v99", "final.md", "# Build spec\n\nfirst revision\n")
	v := writeRecipeFile(t, root, "r-v99", "verification.md", mergeTestVerify)

	full := []string{"r-v99", "--from-verification", v, "--doc", "final.md", "--scope", "full", "--dimension", "verify"}
	runRecipeLog(t, root, full...)

	// The fix lands in final.md, but the delta's output never reached
	// verification.md: the record would read round 1's file.
	writeRecipeFile(t, root, "r-v99", "final.md", "# Build spec\n\nsecond revision\n")
	delta := []string{"r-v99", "--from-verification", v, "--doc", "final.md", "--scope", "delta", "--dimension", "verify"}
	out := runRecipeLog(t, root, delta...)

	entries, err := state.ReadVerifyLog(root, "r-v99")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("a re-read of round 1's verification.md must not record a round, got %d entries", len(entries))
	}
	if !strings.Contains(out, "iteration 1") {
		t.Errorf("the refusal names the round that recorded the file, got:\n%s", out)
	}

	// Once this round's output is written, it records.
	writeRecipeFile(t, root, "r-v99", "verification.md",
		strings.Replace(mergeTestVerify, "INV-001 has two owners", "INV-002 is unowned", 2))
	runRecipeLog(t, root, delta...)
	entries, _ = state.ReadVerifyLog(root, "r-v99")
	if len(entries) != 2 {
		t.Fatalf("a new verification.md records a new round, got %d entries", len(entries))
	}
}
