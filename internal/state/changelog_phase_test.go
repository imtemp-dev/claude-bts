package state

import (
	"os"
	"testing"
)

// Trial problem #94: `bts recipe log --phase X` while the recipe was
// already in X wrote "discovery → discovery", "domain-model →
// domain-model" and "wireframe → wireframe" into swift-eagle's changelog,
// and `bts stats` printed each as an extra 0 s row.  A transition to the
// same phase is no transition.
func TestLogPhaseChange_SkipsTheSamePhase(t *testing.T) {
	root := t.TempDir()
	id := "r-094"
	if err := os.MkdirAll(RecipeDir(root, id), 0755); err != nil {
		t.Fatal(err)
	}
	if err := LogPhaseChange(root, id, "discovery", "discovery"); err != nil {
		t.Fatal(err)
	}
	if err := LogPhaseChange(root, id, "discovery", "scoping"); err != nil {
		t.Fatal(err)
	}
	entries, err := ReadChangelog(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Result != "discovery → scoping" {
		t.Fatalf("want only the real transition, got %+v", entries)
	}
}
