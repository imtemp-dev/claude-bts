package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/metrics"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #96, its last part: a build recipe's steps 9 to 13 (final.md
// and its review) log no phase, so `bts stats` counted swift-eagle's 1.5 h
// review loop as falsify (2 h 14 min, 48%).  The span is split at the first
// recorded review of final.md, and the second part is marked as derived.
func TestSplitBuildReviewTail(t *testing.T) {
	root := newRecipeFixture(t, "r-tail", "finalize", 3, 0, nil)
	r, _ := state.LoadRecipeState(root, "r-tail")
	r.Type = "build"
	_ = state.SaveRecipeState(root, r)
	writeVerifyLogEntries(t, root, "r-tail",
		state.VerifyLogEntry{Iteration: 1, Doc: "final.md", FullPass: true, Timestamp: "2026-11-06T00:36:04Z"},
		state.VerifyLogEntry{Iteration: 6, Doc: "final.md", Timestamp: "2026-11-06T02:07:00Z"},
	)
	in := func(s string) time.Time { ts, _ := time.Parse(time.RFC3339, s); return ts }
	stats := &metrics.RecipeStats{Phases: []metrics.PhaseSpan{
		{Phase: "skeleton", EnteredAt: in("2026-11-05T23:33:27Z"), ExitedAt: in("2026-11-05T23:53:19Z"), Duration: 19*time.Minute + 52*time.Second},
		{Phase: "falsify", EnteredAt: in("2026-11-05T23:53:19Z"), ExitedAt: in("2026-11-06T02:07:52Z"), Duration: 2*time.Hour + 14*time.Minute + 33*time.Second},
	}}
	splitBuildReviewTail(stats, root, r)

	if len(stats.Phases) != 3 {
		t.Fatalf("want skeleton, falsify and the review tail, got %+v", stats.Phases)
	}
	if p := stats.Phases[1]; p.Phase != "falsify" || p.Duration != 42*time.Minute+45*time.Second {
		t.Errorf("falsify runs to the first review of final.md (00:36:04), got %s %s", p.Phase, p.Duration)
	}
	if p := stats.Phases[2]; p.Phase != "review*" || p.Duration != time.Hour+31*time.Minute+48*time.Second {
		t.Errorf("the derived review tail runs 00:36:04 to 02:07:52, got %s %s", p.Phase, p.Duration)
	}
}

// writeVerifyLogEntries writes entries with their own timestamps, which
// AppendVerifyLog would replace with the current time.
func writeVerifyLogEntries(t *testing.T, root, id string, entries ...state.VerifyLogEntry) {
	t.Helper()
	var b strings.Builder
	for _, e := range entries {
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(line)
		b.WriteString("\n")
	}
	if err := os.WriteFile(filepath.Join(state.RecipeDir(root, id), "verify-log.jsonl"), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
}
