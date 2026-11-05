package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A build recipe writes most of its code at the skeleton and falsify
// stages, so its tasks close in one pass and no window of tasks matches a
// stretch of new code: true-shark closed all 16 at once and ran no
// mid-run review, which the stats counted as three missing (trial problem
// #77, Multica AGEN-58).  Its code is checked in stages instead, so a
// build recipe expects no mid-run review; other types keep the cadence.
func TestComputeRecipeStats_BuildRecipeExpectsNoMidrunReview(t *testing.T) {
	for _, c := range []struct {
		typ  string
		want int
	}{{"build", 0}, {"blueprint", 3}} {
		root := t.TempDir()
		dir := filepath.Join(root, ".bts", "specs", "recipes", "r-1")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		var tasks []string
		for i := 1; i <= 16; i++ {
			tasks = append(tasks, fmt.Sprintf(`{"id":"t-%03d","file":"a%d.go","action":"create","status":"done"}`, i, i))
		}
		files := map[string]string{
			"recipe.json": `{"id":"r-1","type":"` + c.typ + `","phase":"implement","topic":"x","created":"2026-11-06T00:00:00Z","updated":"2026-11-06T00:00:00Z"}`,
			"tasks.json":  `{"recipe_id":"r-1","tasks":[` + strings.Join(tasks, ",") + `]}`,
		}
		for name, body := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
				t.Fatal(err)
			}
		}
		s, err := ComputeRecipeStats(root, dir)
		if err != nil {
			t.Fatal(err)
		}
		if s.CompletedTasks != 16 || s.MidrunExpected != c.want {
			t.Errorf("%s recipe: 16 done tasks with the default cadence of 5 expect %d mid-run reviews, got %d (completed %d)",
				c.typ, c.want, s.MidrunExpected, s.CompletedTasks)
		}
	}
}
