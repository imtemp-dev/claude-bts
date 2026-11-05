package state

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/pkg/version"
)

// Trial problem #55: r-003 started on one bts build and finished on
// another, and nothing in its record said which build wrote what.  Every
// changelog entry bts writes carries the build that wrote it.
func TestAppendChangelog_StampsTheBtsVersion(t *testing.T) {
	root := t.TempDir()
	id := "r-055-version"
	_ = os.MkdirAll(RecipeDir(root, id), 0755)

	if err := AppendChangelog(root, id, &ChangelogEntry{Action: "draft"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(ChangelogPath(root, id))
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &raw); err != nil {
		t.Fatalf("parse %q: %v", data, err)
	}
	if got, _ := raw["bts_version"].(string); got != version.GetTemplateVersion() {
		t.Fatalf("bts_version = %q, want %q, in %s", got, version.GetTemplateVersion(), data)
	}
}

// A changelog written before the field existed still loads.
func TestReadChangelog_AnEntryWithoutAVersionStillLoads(t *testing.T) {
	root := t.TempDir()
	id := "r-055-legacy"
	_ = os.MkdirAll(RecipeDir(root, id), 0755)
	legacy := `{"time":"2026-03-18T10:00:00Z","action":"research","output":"research/v1.md"}` + "\n"
	if err := os.WriteFile(ChangelogPath(root, id), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	entries, err := ReadChangelog(root, id)
	if err != nil || len(entries) != 1 || entries[0].Action != "research" {
		t.Fatalf("a legacy entry must load, got %+v err=%v", entries, err)
	}
}
