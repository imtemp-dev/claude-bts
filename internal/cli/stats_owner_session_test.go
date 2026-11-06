package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/metrics"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #107: cool-crow built r-004 inside one session that began
// two hours before the recipe, and `bts stats` said "Sessions: 0" with no
// model, because sessions and models came only from SessionStart events
// inside the recipe.  The owner session counts, and its model comes from
// its transcript.
func TestOwnerSessionFloor_CountsTheSessionTheRecipeRanIn(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	dir := metrics.TranscriptDir(root)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	transcript := `{"type":"user","timestamp":"2026-11-06T00:45:00Z","message":{"role":"user","content":"go"}}` + "\n" +
		`{"type":"assistant","timestamp":"2026-11-06T00:45:05Z","message":{"id":"m1","model":"claude-opus-5-5","usage":{"input_tokens":1,"output_tokens":2}}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "9893e36b.jsonl"), []byte(transcript), 0644); err != nil {
		t.Fatal(err)
	}

	recipe := &state.RecipeState{ID: "r-004", OwnerSession: "9893e36b"}
	stats := &metrics.RecipeStats{}
	ownerSessionFloor(root, recipe, stats)
	if stats.TotalSessions != 1 || !reflect.DeepEqual(stats.Models, []string{"claude-opus-5-5"}) {
		t.Fatalf("want 1 session on claude-opus-5-5, got %d %v", stats.TotalSessions, stats.Models)
	}

	counted := &metrics.RecipeStats{TotalSessions: 2, Models: []string{"x"}}
	ownerSessionFloor(root, recipe, counted)
	if counted.TotalSessions != 2 || !reflect.DeepEqual(counted.Models, []string{"x"}) {
		t.Errorf("sessions the events counted stand: got %d %v", counted.TotalSessions, counted.Models)
	}
}
