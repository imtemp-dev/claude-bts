package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupWorkStateRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".forge", "state", "recipes"), 0755)
	return root
}

func TestSaveAndLoadWorkState(t *testing.T) {
	root := setupWorkStateRoot(t)

	ws := &WorkState{
		RecipeID:    "r-1000",
		Phase:       "draft",
		Topic:       "OAuth2",
		LastActions: []string{"research done", "draft started"},
		Summary:     "Recipe r-1000 (blueprint) — phase: draft.",
	}

	if err := SaveWorkState(root, ws); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := LoadWorkState(root)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if loaded.RecipeID != "r-1000" {
		t.Errorf("RecipeID: got %s, want r-1000", loaded.RecipeID)
	}
	if loaded.Phase != "draft" {
		t.Errorf("Phase: got %s, want draft", loaded.Phase)
	}
	if loaded.SavedAt == "" {
		t.Error("SavedAt should be auto-set")
	}
	if len(loaded.LastActions) != 2 {
		t.Errorf("LastActions: got %d, want 2", len(loaded.LastActions))
	}
}

func TestLoadWorkState_NotFound(t *testing.T) {
	root := setupWorkStateRoot(t)
	_, err := LoadWorkState(root)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestBuildWorkState(t *testing.T) {
	t.Run("no active recipe returns nil", func(t *testing.T) {
		root := setupWorkStateRoot(t)

		ws, err := BuildWorkState(root)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ws != nil {
			t.Error("expected nil work state")
		}
	})

	t.Run("active recipe in draft phase", func(t *testing.T) {
		root := setupWorkStateRoot(t)
		saveTestRecipe(t, root, "r-1000", "draft")

		ws, err := BuildWorkState(root)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ws == nil {
			t.Fatal("expected work state, got nil")
		}
		if ws.RecipeID != "r-1000" {
			t.Errorf("RecipeID: got %s, want r-1000", ws.RecipeID)
		}
		if ws.Phase != "draft" {
			t.Errorf("Phase: got %s, want draft", ws.Phase)
		}
		if ws.Summary == "" {
			t.Error("Summary should be populated")
		}
	})

	t.Run("implement phase with tasks", func(t *testing.T) {
		root := setupWorkStateRoot(t)
		recipeID := "r-2000"
		saveTestRecipe(t, root, recipeID, "implement")

		ts := &TaskState{
			RecipeID: recipeID,
			Tasks: []Task{
				{ID: "t-1", File: "a.go", Status: "done"},
				{ID: "t-2", File: "b.go", Status: "in_progress", RetryCount: 1, LastError: "compile error"},
				{ID: "t-3", File: "c.go", Status: "pending"},
			},
		}
		WriteJSON(filepath.Join(RecipeDir(root, recipeID), "tasks.json"), ts)

		ws, err := BuildWorkState(root)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ws.CurrentTask == nil {
			t.Fatal("expected current task")
		}
		if ws.CurrentTask.ID != "t-2" {
			t.Errorf("CurrentTask.ID: got %s, want t-2", ws.CurrentTask.ID)
		}
		if ws.CurrentTask.RetryCount != 1 {
			t.Errorf("RetryCount: got %d, want 1", ws.CurrentTask.RetryCount)
		}

		// Should include task progress in last actions
		found := false
		for _, a := range ws.LastActions {
			if strings.Contains(a, "1/3 done") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("LastActions should contain task progress: %v", ws.LastActions)
		}
	})

	t.Run("finalized recipe fallback", func(t *testing.T) {
		root := setupWorkStateRoot(t)
		saveTestRecipe(t, root, "r-1000", "finalize")

		ws, err := BuildWorkState(root)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ws == nil {
			t.Fatal("expected work state from finalized recipe")
		}
		if ws.Phase != "finalize" {
			t.Errorf("Phase: got %s, want finalize", ws.Phase)
		}
	})

	t.Run("scope status detection", func(t *testing.T) {
		root := setupWorkStateRoot(t)
		recipeID := "r-3000"
		saveTestRecipe(t, root, recipeID, "draft")

		scopePath := filepath.Join(RecipeDir(root, recipeID), "scope.md")
		os.WriteFile(scopePath, []byte("# Scope\n### Status: CONFIRMED\n"), 0644)

		ws, err := BuildWorkState(root)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ws.ScopeStatus != "CONFIRMED" {
			t.Errorf("ScopeStatus: got %s, want CONFIRMED", ws.ScopeStatus)
		}
	})

	t.Run("changelog integration", func(t *testing.T) {
		root := setupWorkStateRoot(t)
		recipeID := "r-4000"
		saveTestRecipe(t, root, recipeID, "verify")

		// Write changelog entries
		for _, action := range []string{"research", "draft", "verify"} {
			AppendChangelog(root, recipeID, &ChangelogEntry{
				Action: action,
				Output: action + "-output",
			})
		}

		ws, err := BuildWorkState(root)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should have changelog entries in last actions
		hasChangelog := false
		for _, a := range ws.LastActions {
			if strings.Contains(a, "research") || strings.Contains(a, "draft") || strings.Contains(a, "verify") {
				hasChangelog = true
				break
			}
		}
		if !hasChangelog {
			t.Errorf("LastActions should include changelog entries: %v", ws.LastActions)
		}
	})
}

func TestWorkStatePath(t *testing.T) {
	got := WorkStatePath("/project")
	want := filepath.Join("/project", ".forge", "state", "work-state.json")
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
