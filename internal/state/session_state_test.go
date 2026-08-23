package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupSessionRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".jig", "local"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return root
}

func TestSessionState_SaveAndLoad(t *testing.T) {
	root := setupSessionRoot(t)
	s := &SessionState{
		RecentTools: []ToolTraceEntry{{ToolName: "Read", File: "a.go"}},
		OpenFiles:   []string{"a.go"},
	}
	if err := SaveSessionState(root, s); err != nil {
		t.Fatalf("save: %v", err)
	}
	if s.SavedAt == "" {
		t.Error("SavedAt should be stamped")
	}
	loaded, err := LoadSessionState(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.RecentTools) != 1 || loaded.RecentTools[0].File != "a.go" {
		t.Errorf("unexpected RecentTools: %+v", loaded.RecentTools)
	}
	if len(loaded.OpenFiles) != 1 || loaded.OpenFiles[0] != "a.go" {
		t.Errorf("unexpected OpenFiles: %+v", loaded.OpenFiles)
	}
}

func TestSessionState_LoadMissing(t *testing.T) {
	root := setupSessionRoot(t)
	_, err := LoadSessionState(root)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestSessionState_BuildFromTrace(t *testing.T) {
	root := setupSessionRoot(t)

	_ = AppendToolTrace(root, &ToolTraceEntry{Phase: "post", ToolName: "Read", File: "foo.go"})
	_ = AppendToolTrace(root, &ToolTraceEntry{Phase: "post", ToolName: "Grep", File: ""})
	_ = AppendToolTrace(root, &ToolTraceEntry{Phase: "post", ToolName: "Edit", File: "bar.go"})
	_ = AppendToolTrace(root, &ToolTraceEntry{Phase: "post", ToolName: "Read", File: "foo.go"})

	s, err := BuildSessionState(root)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if s == nil {
		t.Fatal("expected session state, got nil")
	}
	if len(s.RecentTools) != 4 {
		t.Errorf("want 4 RecentTools, got %d", len(s.RecentTools))
	}
	// OpenFiles should dedupe and only include Read/Edit/Write
	if len(s.OpenFiles) != 2 {
		t.Errorf("want 2 unique open files, got %v", s.OpenFiles)
	}
	if s.OpenFiles[0] != "foo.go" || s.OpenFiles[1] != "bar.go" {
		t.Errorf("unexpected order: %v", s.OpenFiles)
	}
}

func TestSessionState_BuildEmpty(t *testing.T) {
	root := setupSessionRoot(t)
	// Override HOME to an empty dir so plan lookup returns nothing
	home := t.TempDir()
	t.Setenv("HOME", home)

	s, err := BuildSessionState(root)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if s != nil {
		t.Errorf("expected nil when no trace/plan, got %+v", s)
	}
}

func TestSessionState_LatestPlanFile(t *testing.T) {
	root := setupSessionRoot(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	plansDir := filepath.Join(home, ".claude", "plans")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatalf("mkdir plans: %v", err)
	}

	older := filepath.Join(plansDir, "older.md")
	newer := filepath.Join(plansDir, "newer.md")
	if err := os.WriteFile(older, []byte("old"), 0644); err != nil {
		t.Fatalf("write older: %v", err)
	}
	if err := os.WriteFile(newer, []byte("new"), 0644); err != nil {
		t.Fatalf("write newer: %v", err)
	}
	// Make older actually older
	pastTime := time.Now().Add(-time.Hour)
	if err := os.Chtimes(older, pastTime, pastTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	// seed a tool trace so BuildSessionState returns non-nil
	_ = AppendToolTrace(root, &ToolTraceEntry{ToolName: "Read", File: "x.go"})

	s, err := BuildSessionState(root)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil")
	}
	if s.PendingPlan != newer {
		t.Errorf("expected %s, got %s", newer, s.PendingPlan)
	}
}
