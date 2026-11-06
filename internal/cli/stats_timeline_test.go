package cli

import (
	"testing"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/metrics"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #96: `bts stats` gave swift-eagle's discovery 0 s and a
// 4 h 40 min timeline against a 4 h 59 min duration, because the timeline
// began at the first phase record rather than the recipe's creation; and
// same-phase records printed as extra 0 s rows.
func TestRecipeTimeline_StartsAtTheRecipesCreation(t *testing.T) {
	events := []metrics.MetricsEvent{
		{Kind: metrics.KindPhaseChange, Phase: "discovery", PreviousPhase: "discovery", Timestamp: "2026-11-05T21:27:12Z"},
		{Kind: metrics.KindPhaseChange, Phase: "scoping", PreviousPhase: "discovery", Timestamp: "2026-11-05T21:27:12Z"},
		{Kind: metrics.KindPhaseChange, Phase: "scoping", PreviousPhase: "scoping", Timestamp: "2026-11-05T21:30:00Z"},
		{Kind: metrics.KindPhaseChange, Phase: "research", PreviousPhase: "scoping", Timestamp: "2026-11-05T21:35:14Z"},
	}
	stats := metrics.AggregateRecipe(events)
	recipe := &state.RecipeState{ID: "r-001", StartedAt: "2026-11-05T21:08:25Z"}
	startTimeline(stats, recipe, events)

	if len(stats.Phases) != 2 {
		t.Fatalf("want discovery and scoping, no same-phase rows, got %+v", stats.Phases)
	}
	if p := stats.Phases[0]; p.Phase != "discovery" || p.Duration != 18*time.Minute+47*time.Second {
		t.Errorf("discovery runs from the recipe's creation (21:08:25) to 21:27:12, got %s %s", p.Phase, p.Duration)
	}
	if p := stats.Phases[1]; p.Phase != "scoping" || p.Duration != 8*time.Minute+2*time.Second {
		t.Errorf("scoping runs 21:27:12 to 21:35:14 in one row, got %s %s", p.Phase, p.Duration)
	}
}
