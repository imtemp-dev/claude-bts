package state

import (
	"os"
	"path/filepath"
	"testing"
)

func setupMarkerRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".jig", "local"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return root
}

func TestCompactMarker_WriteAndConsume(t *testing.T) {
	root := setupMarkerRoot(t)

	m := &CompactMarker{SessionID: "s1", RecipeID: "r-001-x", Phase: "draft"}
	if err := WriteCompactMarker(root, m); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(CompactMarkerPath(root)); err != nil {
		t.Fatalf("expected marker on disk: %v", err)
	}
	if m.CreatedAt == "" {
		t.Error("CreatedAt should be stamped")
	}

	got, err := ConsumeCompactMarker(root)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if got == nil {
		t.Fatal("expected marker, got nil")
	}
	if got.SessionID != "s1" || got.RecipeID != "r-001-x" || got.Phase != "draft" {
		t.Errorf("unexpected marker: %+v", got)
	}
	if _, err := os.Stat(CompactMarkerPath(root)); !os.IsNotExist(err) {
		t.Error("marker should be deleted after consume")
	}
}

func TestCompactMarker_ConsumeMissing(t *testing.T) {
	root := setupMarkerRoot(t)
	got, err := ConsumeCompactMarker(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestCompactMarker_ConsumeTwice(t *testing.T) {
	root := setupMarkerRoot(t)
	_ = WriteCompactMarker(root, &CompactMarker{SessionID: "s1"})

	if _, err := ConsumeCompactMarker(root); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	got, err := ConsumeCompactMarker(root)
	if err != nil {
		t.Fatalf("second consume: %v", err)
	}
	if got != nil {
		t.Errorf("second consume should return nil, got %+v", got)
	}
}

func TestCompactMarker_MalformedFile(t *testing.T) {
	root := setupMarkerRoot(t)
	path := CompactMarkerPath(root)
	if err := os.WriteFile(path, []byte("{not json"), 0644); err != nil {
		t.Fatalf("seed malformed: %v", err)
	}
	got, err := ConsumeCompactMarker(root)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if got != nil {
		t.Errorf("malformed should yield nil, got %+v", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("malformed file should be removed")
	}
}

func TestCompactMarker_OverwriteOnRewrite(t *testing.T) {
	root := setupMarkerRoot(t)
	_ = WriteCompactMarker(root, &CompactMarker{SessionID: "first"})
	_ = WriteCompactMarker(root, &CompactMarker{SessionID: "second"})
	got, _ := ConsumeCompactMarker(root)
	if got == nil || got.SessionID != "second" {
		t.Errorf("expected second, got %+v", got)
	}
}
