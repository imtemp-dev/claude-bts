package state

import "testing"

func fs(id, sev, status string, openRounds, first int) *FindingState {
	return &FindingState{
		FindingEvent:   FindingEvent{ID: id, Severity: sev, Status: status},
		OpenRounds:     openRounds,
		FirstIteration: first,
	}
}

func ids(list []*FindingState) []string {
	out := make([]string, 0, len(list))
	for _, st := range list {
		out = append(out, st.ID)
	}
	return out
}

func TestDefendBatch_SelectsOpenCriticalAndMajorInOrder(t *testing.T) {
	states := []*FindingState{
		fs("F-minor", "minor_resolvable", FindingOpen, 5, 1),
		fs("F-maj-new", "major", FindingOpen, 1, 3),
		fs("F-crit-fixed", "critical", FindingFixed, 2, 1),
		fs("F-maj-old", "major", FindingUnreported, 3, 1), // unreported is still owed
		fs("F-crit", "critical", FindingOpen, 1, 2),
		fs("F-deferred", "minor_deferred", FindingDeferred, 1, 1),
		fs("F-dismissed", "major", FindingDismissed, 4, 1),
	}
	batch, rest := DefendBatch(states, 0)
	got := ids(batch)
	want := []string{"F-crit", "F-maj-old", "F-maj-new"}
	if len(got) != len(want) {
		t.Fatalf("batch = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("batch[%d] = %s, want %s (full: %v)", i, got[i], want[i], got)
		}
	}
	if len(rest) != 0 {
		t.Errorf("no cap must leave nothing undefended, got %v", ids(rest))
	}
}

func TestDefendBatch_CapsAndReportsTheRest(t *testing.T) {
	var states []*FindingState
	for i := 0; i < 15; i++ {
		sev := "major"
		if i%3 == 0 {
			sev = "critical"
		}
		states = append(states, fs("F-"+string(rune('a'+i)), sev, FindingOpen, i, 1))
	}
	batch, rest := DefendBatch(states, 12)
	if len(batch) != 12 || len(rest) != 3 {
		t.Fatalf("batch=%d rest=%d, want 12/3", len(batch), len(rest))
	}
	// Every critical made the batch; the three left out are the majors
	// with the fewest rounds open.
	for _, st := range rest {
		if st.Severity == "critical" {
			t.Errorf("a critical must never be left undefended while a major is in the batch: %s", st.ID)
		}
	}
	for _, st := range batch {
		for _, r := range rest {
			if st.Severity == r.Severity && st.OpenRounds < r.OpenRounds {
				t.Errorf("%s (%d rounds) in batch but %s (%d rounds) left out", st.ID, st.OpenRounds, r.ID, r.OpenRounds)
			}
		}
	}
}

func TestDefendBatch_EmptyLedger(t *testing.T) {
	batch, rest := DefendBatch(nil, 12)
	if len(batch) != 0 || len(rest) != 0 {
		t.Errorf("empty ledger must yield nothing, got %v / %v", ids(batch), ids(rest))
	}
}
