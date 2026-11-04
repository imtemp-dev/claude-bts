package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// simulate.finding_batch also capped /bts-defend; with simulation gone it
// lives at defend.finding_batch. An old settings.yaml that still sets the
// simulate key keeps its value (prove-stage-design.md, "Removing
// simulation").
func TestSettings_DefendFindingBatch(t *testing.T) {
	load := func(yaml string) int {
		t.Helper()
		root := t.TempDir()
		if yaml != "" {
			_ = os.MkdirAll(filepath.Join(root, ".bts", "config"), 0755)
			if err := os.WriteFile(filepath.Join(root, ".bts", "config", "settings.yaml"), []byte(yaml), 0644); err != nil {
				t.Fatal(err)
			}
		}
		s, err := LoadSettings(root)
		if err != nil {
			t.Fatal(err)
		}
		return s.Defend.FindingBatch
	}
	if got := load(""); got != 6 {
		t.Errorf("default = %d, want 6", got)
	}
	if got := load("defend:\n  finding_batch: 4\n"); got != 4 {
		t.Errorf("defend.finding_batch = %d, want 4", got)
	}
	if got := load("simulate:\n  finding_batch: 5\n  min_scenarios: 5\n"); got != 5 {
		t.Errorf("a legacy simulate.finding_batch must carry over, got %d", got)
	}
	if got := load("defend:\n  finding_batch: 3\nsimulate:\n  finding_batch: 9\n"); got != 3 {
		t.Errorf("defend.finding_batch wins over the legacy key, got %d", got)
	}
	if got := load("defend:\n  finding_batch: 0\n"); got != 0 {
		t.Errorf("0 disables the cap and must be kept, got %d", got)
	}
}
