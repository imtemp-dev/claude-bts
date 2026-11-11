package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Feature #132, step 2: P3 cost a fixed 29-39 active minutes with two
// adversaries whatever the size of the change.  An express recipe's round
// takes one adversary by default, aimed at the scope items; the full
// recipe keeps two or three.

// prepareSeats runs `bts interrogate prepare` on a build fixture in mode
// with the extra args, and returns the seats or the refusal.
func prepareSeats(t *testing.T, mode string, extra ...string) ([]*state.Adversary, error) {
	t.Helper()
	root, id := newBuildFixture(t)
	r, err := state.LoadRecipeState(root, id)
	if err != nil {
		t.Fatal(err)
	}
	r.Type, r.Mode = "build", mode
	if err := state.SaveRecipeState(root, r); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	if err := runQuiet(t, interrogatePrepareCmd, append([]string{"interrogate", "prepare", id}, extra...)...); err != nil {
		return nil, err
	}
	run, err := state.LoadInterrogateRun(root, id)
	if err != nil || run == nil {
		t.Fatalf("no interrogate run: %v", err)
	}
	t.Cleanup(func() {
		for _, a := range run.Adversaries {
			_ = os.RemoveAll(a.Copy)
		}
	})
	return run.Adversaries, nil
}

func TestInterrogatePrepare_ExpressTakesOneAdversaryAimedAtTheScope(t *testing.T) {
	seats, err := prepareSeats(t, state.ModeExpress)
	if err != nil {
		t.Fatal(err)
	}
	if len(seats) != 1 || !strings.Contains(seats[0].Angle, "scope.md") {
		t.Fatalf("express takes one seat aimed at the scope items; got %d seat(s): %+v", len(seats), seats)
	}
}

func TestInterrogatePrepare_SeatCountsFollowTheMode(t *testing.T) {
	if seats, err := prepareSeats(t, ""); err != nil || len(seats) != 2 {
		t.Errorf("the full recipe keeps two seats by default; got %d, %v", len(seats), err)
	}
	if seats, err := prepareSeats(t, state.ModeFull); err != nil || len(seats) != 2 {
		t.Errorf("a recipe moved to full takes two seats; got %d, %v", len(seats), err)
	}
	if _, err := prepareSeats(t, "", "--adversaries", "1"); err == nil || !strings.Contains(err.Error(), "express") {
		t.Errorf("one seat on the full recipe must be refused, saying one seat is express's; got %v", err)
	}
	if seats, err := prepareSeats(t, state.ModeExpress, "--adversaries", "2"); err != nil || len(seats) != 2 {
		t.Errorf("an express recipe may ask for two seats; got %d, %v", len(seats), err)
	}
}
