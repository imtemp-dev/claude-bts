package cli

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/imtemp-dev/claude-bts/pkg/version"
)

// readJSONL returns every parseable line of a JSONL file as a raw map, so
// a test sees the field names as written.
func readJSONL(t *testing.T, path string) []map[string]interface{} {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	var out []map[string]interface{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var m map[string]interface{}
		if json.Unmarshal([]byte(sc.Text()), &m) == nil {
			out = append(out, m)
		}
	}
	return out
}

// Trial problem #55: r-003 started on one bts build and finished on
// another, and no record said which build ran which phase.  A phase change
// through `bts recipe log --phase` is written to the changelog and to the
// phase_change metric with the bts version, and a changelog written before
// the field existed still validates.
func TestRecipeLogPhase_RecordsTheBtsVersion(t *testing.T) {
	id := "r-055-phase"
	root := newRecipeFixture(t, id, "draft", 1.0, 1, nil)
	legacy := `{"time":"2026-03-18T10:00:00Z","action":"draft","output":"draft.md"}` + "\n"
	if err := os.WriteFile(state.ChangelogPath(root, id), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}

	if out := runRecipeLog(t, root, id, "--phase", "verify"); strings.Contains(out, "Error") {
		t.Fatalf("phase change failed: %s", out)
	}

	want := version.GetTemplateVersion()
	var phaseEntry map[string]interface{}
	for _, e := range readJSONL(t, state.ChangelogPath(root, id)) {
		if e["action"] == "phase" {
			phaseEntry = e
		}
	}
	if phaseEntry == nil {
		t.Fatal("the phase change must be written to changelog.jsonl")
	}
	if phaseEntry["bts_version"] != want {
		t.Errorf("phase entry bts_version = %v, want %q", phaseEntry["bts_version"], want)
	}
	if r, _ := phaseEntry["result"].(string); !strings.Contains(r, "draft") || !strings.Contains(r, "verify") {
		t.Errorf("phase entry must name both phases, got result %q", r)
	}

	metricsPath := filepath.Join(root, ".bts", "local", "recipes", id, "metrics.jsonl")
	var change map[string]interface{}
	for _, e := range readJSONL(t, metricsPath) {
		if e["kind"] == "phase_change" {
			change = e
		}
	}
	if change == nil || change["bts_version"] != want {
		t.Errorf("the phase_change metric must carry bts_version %q, got %v", want, change)
	}

	errs, err := engine.ValidateRecipeDir(state.RecipeDir(root, id))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range errs {
		if e.File == "changelog.jsonl" {
			t.Errorf("bts validate rejects the changelog: %s %s", e.Field, e.Message)
		}
	}
}
