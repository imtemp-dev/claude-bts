package cli

import (
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #131: bts-architect's EXTEND step runs a follow-up debate
// "in a new -followup directory", but `bts debate log --topic` minted its
// own id, so the rounds and the state never shared one.  warm-owl's r-003
// re-logged its three follow-ups with --id <dirname>, and the three ids
// minted first were left with no rounds.  --followup-of takes the name the
// skill gives the directory.
func TestDebateLog_FollowupOfTakesTheNextFollowupID(t *testing.T) {
	root, _ := newTestRunFixture(t)
	t.Chdir(root)
	if err := runQuiet(t, debateLogCmd, "debate", "log", "--id", "d-1", "--topic", "two runners, one choice"); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"d-1-followup", "d-1-followup-2"} {
		if err := runQuiet(t, debateLogCmd, "debate", "log", "--followup-of", "d-1", "--topic", "settle the bounds rule", "--round", "1", "--content", "round one"); err != nil {
			t.Fatal(err)
		}
		ds, err := state.LoadDebateState(root, want)
		if err != nil {
			t.Fatalf("the follow-up must be recorded as %s: %v", want, err)
		}
		if ds.Rounds != 1 || ds.Topic != "settle the bounds rule" {
			t.Fatalf("%s: rounds %d, topic %q", want, ds.Rounds, ds.Topic)
		}
	}

	if err := runQuiet(t, debateLogCmd, "debate", "log", "--followup-of", "d-404"); err == nil || !strings.Contains(err.Error(), "d-404") {
		t.Errorf("a follow-up of a debate that was never logged must be refused, naming it; got %v", err)
	}
	if err := runQuiet(t, debateLogCmd, "debate", "log", "--followup-of", "d-1", "--id", "d-9"); err == nil || !strings.Contains(err.Error(), "--id") {
		t.Errorf("--followup-of names a new debate, so --id with it must be refused; got %v", err)
	}
}
