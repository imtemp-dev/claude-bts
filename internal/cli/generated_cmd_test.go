package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// newGeneratedFixture is a recipe whose wireframe has one hand-written row
// and two generate rows: a file, and a directory a second generator fills
// from the first one's output (trial problem #66).
func newGeneratedFixture(t *testing.T, apiGenerator string) (root, id string) {
	t.Helper()
	root, id = newBuildFixture(t)
	wf := "## File Structure\n\n| File | Action | Depends On | Responsibility |\n|---|---|---|---|\n" +
		"| `src/relay.py` | create | - | the relay |\n" +
		"| `gen/api.json` | generate | src/relay.py | the contract (generator: `" + apiGenerator + "`) |\n" +
		"| `gen/types/` | generate | gen/api.json | client types (generator: `mkdir -p gen/types && cp gen/api.json gen/types/api.ts`) |\n"
	if err := os.WriteFile(filepath.Join(state.RecipeDir(root, id), "wireframe.md"), []byte(wf), 0644); err != nil {
		t.Fatal(err)
	}
	return root, id
}

func TestExecuteGeneratedCheck(t *testing.T) {
	root, id := newGeneratedFixture(t, "mkdir -p gen && cp src/relay.py gen/api.json")

	g, err := executeGeneratedCheck(root, id, 30, io.Discard)
	if err != nil || !g.Passed() {
		t.Fatalf("both generators ran and wrote their files: %+v, %v", g, err)
	}
	if len(g.Runs) != 2 || strings.Join(g.Changed, ",") != "gen/api.json,gen/types/" || g.CodeHash == "" || g.FilesHash == "" {
		t.Fatalf("the record names the runs, what they rewrote, and both hashes: %+v", g)
	}
	ev, _ := state.LoadBuildEvidence(root, id)
	if ev.Generated == nil || ev.Generated.FilesHash != g.FilesHash {
		t.Fatalf("build-evidence.json must hold the run: %+v", ev.Generated)
	}
	if code, _ := engine.CodeHash(root, id); code != g.CodeHash {
		t.Fatal("generated files are not code: running the generators must not move the code hash")
	}

	again, err := executeGeneratedCheck(root, id, 30, io.Discard)
	if err != nil || !again.Passed() || len(again.Changed) != 0 || again.FilesHash != g.FilesHash {
		t.Fatalf("a rerun on unchanged code rewrites nothing: %+v, %v", again, err)
	}
}

func TestExecuteGeneratedCheck_FailingGeneratorStopsTheChain(t *testing.T) {
	root, id := newGeneratedFixture(t, "exit 3")
	g, err := executeGeneratedCheck(root, id, 30, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if g.Passed() || len(g.Runs) != 1 || g.Runs[0].ExitCode != 3 || len(g.Missing) != 2 {
		t.Fatalf("a failed generator is recorded failed and the next one does not run: %+v", g)
	}
	if ev, _ := state.LoadBuildEvidence(root, id); ev.Generated == nil || ev.Generated.Passed() {
		t.Fatal("a failed check is recorded too, so the gate sees it")
	}
}

func TestExecuteGeneratedCheck_RowsWithoutAGenerator(t *testing.T) {
	root, id := newBuildFixture(t)
	if _, err := executeGeneratedCheck(root, id, 30, io.Discard); err == nil || !strings.Contains(err.Error(), "no `generate` rows") {
		t.Fatalf("a wireframe with no generate rows has nothing to check, got %v", err)
	}
	_ = os.WriteFile(filepath.Join(state.RecipeDir(root, id), "wireframe.md"), []byte(
		"## File Structure\n\n| File | Action | Depends On | Responsibility |\n|---|---|---|---|\n| gen/api.json | generate | - | the contract |\n"), 0644)
	if _, err := executeGeneratedCheck(root, id, 30, io.Discard); err == nil || !strings.Contains(err.Error(), "gen/api.json name no generator") {
		t.Fatalf("a generate row with no generator must be named, got %v", err)
	}
}
