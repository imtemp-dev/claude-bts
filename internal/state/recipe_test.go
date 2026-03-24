package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupRecipeRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".forge", "state", "recipes"), 0755)
	return root
}

func TestSaveAndLoadRecipeState(t *testing.T) {
	root := setupRecipeRoot(t)
	recipeID := "r-1000"
	os.MkdirAll(RecipeDir(root, recipeID), 0755)

	original := &RecipeState{
		ID:        recipeID,
		Type:      "blueprint",
		Topic:     "OAuth2 authentication",
		Phase:     "draft",
		Iteration: 2,
		Level:     1.5,
		StartedAt: "2026-01-01T00:00:00Z",
	}

	if err := SaveRecipeState(root, original); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := LoadRecipeState(root, recipeID)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if loaded.ID != original.ID {
		t.Errorf("ID: got %s, want %s", loaded.ID, original.ID)
	}
	if loaded.Type != original.Type {
		t.Errorf("Type: got %s, want %s", loaded.Type, original.Type)
	}
	if loaded.Topic != original.Topic {
		t.Errorf("Topic: got %s, want %s", loaded.Topic, original.Topic)
	}
	if loaded.Phase != original.Phase {
		t.Errorf("Phase: got %s, want %s", loaded.Phase, original.Phase)
	}
	if loaded.Iteration != original.Iteration {
		t.Errorf("Iteration: got %d, want %d", loaded.Iteration, original.Iteration)
	}
	if loaded.UpdatedAt == "" {
		t.Error("UpdatedAt should be auto-set by SaveRecipeState")
	}
}

func TestLoadRecipeState_NotFound(t *testing.T) {
	root := setupRecipeRoot(t)
	_, err := LoadRecipeState(root, "r-nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetActiveRecipe(t *testing.T) {
	t.Run("no recipes dir", func(t *testing.T) {
		root := t.TempDir()
		os.MkdirAll(filepath.Join(root, ".forge", "state"), 0755)

		recipe, err := GetActiveRecipe(root)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if recipe != nil {
			t.Error("expected nil recipe")
		}
	})

	t.Run("finds active recipe", func(t *testing.T) {
		root := setupRecipeRoot(t)

		// Create a completed recipe
		saveTestRecipe(t, root, "r-1000", "complete")
		// Create an active recipe
		saveTestRecipe(t, root, "r-2000", "draft")

		recipe, err := GetActiveRecipe(root)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if recipe == nil {
			t.Fatal("expected active recipe, got nil")
		}
		if recipe.Phase != "draft" {
			t.Errorf("got phase %s, want draft", recipe.Phase)
		}
	})

	t.Run("all completed", func(t *testing.T) {
		root := setupRecipeRoot(t)
		saveTestRecipe(t, root, "r-1000", "complete")
		saveTestRecipe(t, root, "r-2000", "cancelled")

		recipe, err := GetActiveRecipe(root)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if recipe != nil {
			t.Errorf("expected nil, got recipe in phase %s", recipe.Phase)
		}
	})
}

func TestGetFinalizedRecipe(t *testing.T) {
	root := setupRecipeRoot(t)
	saveTestRecipe(t, root, "r-1000", "complete")
	saveTestRecipe(t, root, "r-2000", "finalize")

	recipe, err := GetFinalizedRecipe(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if recipe == nil {
		t.Fatal("expected finalized recipe, got nil")
	}
	if recipe.ID != "r-2000" {
		t.Errorf("got %s, want r-2000", recipe.ID)
	}
}

func TestListRecipes(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		root := setupRecipeRoot(t)
		recipes, err := ListRecipes(root)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(recipes) != 0 {
			t.Errorf("got %d recipes, want 0", len(recipes))
		}
	})

	t.Run("multiple recipes", func(t *testing.T) {
		root := setupRecipeRoot(t)
		saveTestRecipe(t, root, "r-1000", "complete")
		saveTestRecipe(t, root, "r-2000", "draft")
		saveTestRecipe(t, root, "r-3000", "verify")

		recipes, err := ListRecipes(root)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(recipes) != 3 {
			t.Errorf("got %d recipes, want 3", len(recipes))
		}
	})

	t.Run("skips corrupted recipe files", func(t *testing.T) {
		root := setupRecipeRoot(t)
		saveTestRecipe(t, root, "r-1000", "draft")

		// Create a corrupted recipe
		badDir := filepath.Join(StatePath(root), "recipes", "r-bad")
		os.MkdirAll(badDir, 0755)
		os.WriteFile(filepath.Join(badDir, "recipe.json"), []byte("{invalid"), 0644)

		recipes, err := ListRecipes(root)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(recipes) != 1 {
			t.Errorf("got %d recipes, want 1 (should skip corrupted)", len(recipes))
		}
	})
}

func TestNewRecipeID(t *testing.T) {
	root := setupRecipeRoot(t)

	t.Run("first recipe", func(t *testing.T) {
		id := NewRecipeID(root, "OAuth2 auth")
		if id != "r-001-oauth2-auth" {
			t.Errorf("got %s, want r-001-oauth2-auth", id)
		}
	})

	t.Run("sequential numbering", func(t *testing.T) {
		saveTestRecipe(t, root, "r-001-oauth2", "draft")
		id := NewRecipeID(root, "MCP Server")
		if id != "r-002-mcp-server" {
			t.Errorf("got %s, want r-002-mcp-server", id)
		}
	})

	t.Run("coexists with old format", func(t *testing.T) {
		saveTestRecipe(t, root, "r-1774323037", "complete")
		// Old timestamp format should be ignored in sequence calculation
		id := NewRecipeID(root, "Peer discovery")
		// Should still be based on max from r-001, r-002 (not the timestamp)
		if !strings.HasPrefix(id, "r-") {
			t.Errorf("got %s, want prefix r-", id)
		}
	})

	t.Run("empty topic", func(t *testing.T) {
		id := NewRecipeID(root, "")
		if !strings.Contains(id, "recipe") {
			t.Errorf("empty topic should use fallback 'recipe': got %s", id)
		}
	})
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"OAuth2 authentication", "oauth2"},
		{"MCP Server", "mcp-server"},
		{"Fix bcrypt hash", "fix-bcrypt-hash"},
		{"Claude Code P2P direct communication MCP server", "claude-code-p2p"},
		{"한국어 테스트", ""},
		{"", ""},
		{"a--b  c", "a-b-c"},
		{"short", "short"},
		{"This is a very long topic", "this-is-a-very-long"},
		{"add OAuth2 auth", "add-oauth2-auth"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := Slugify(tt.input)
			if got != tt.want {
				t.Errorf("Slugify(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsImplementPhase(t *testing.T) {
	tests := []struct {
		phase string
		want  bool
	}{
		{"implement", true},
		{"test", true},
		{"review", true},
		{"sync", true},
		{"status", true},
		{"draft", false},
		{"verify", false},
		{"complete", false},
		{"finalize", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.phase, func(t *testing.T) {
			if got := IsImplementPhase(tt.phase); got != tt.want {
				t.Errorf("IsImplementPhase(%q) = %v, want %v", tt.phase, got, tt.want)
			}
		})
	}
}

func TestAppendVerifyLog(t *testing.T) {
	root := setupRecipeRoot(t)
	recipeID := "r-1000"
	os.MkdirAll(RecipeDir(root, recipeID), 0755)

	entries := []*VerifyLogEntry{
		{Iteration: 1, Critical: 2, Major: 3, Minor: 1, Status: "continue"},
		{Iteration: 2, Critical: 0, Major: 1, Minor: 0, Status: "continue"},
		{Iteration: 3, Critical: 0, Major: 0, Minor: 0, Status: "converged"},
	}

	for _, e := range entries {
		if err := AppendVerifyLog(root, recipeID, e); err != nil {
			t.Fatalf("append failed: %v", err)
		}
	}

	// Verify timestamps were set
	for _, e := range entries {
		if e.Timestamp == "" {
			t.Error("Timestamp should be auto-set")
		}
	}

	// Read back and verify
	path := filepath.Join(RecipeDir(root, recipeID), "verify-log.jsonl")
	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}

	var last VerifyLogEntry
	json.Unmarshal([]byte(lines[2]), &last)
	if last.Status != "converged" {
		t.Errorf("last status: got %s, want converged", last.Status)
	}
}

func TestRecipeDir(t *testing.T) {
	got := RecipeDir("/project", "r-1000")
	want := filepath.Join("/project", ".forge", "state", "recipes", "r-1000")
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestLoadTaskState(t *testing.T) {
	root := setupRecipeRoot(t)
	recipeID := "r-1000"
	dir := RecipeDir(root, recipeID)
	os.MkdirAll(dir, 0755)

	ts := &TaskState{
		RecipeID: recipeID,
		Tasks: []Task{
			{ID: "t-1", File: "src/main.go", Action: "create", Status: "done"},
			{ID: "t-2", File: "src/auth.go", Action: "modify", Status: "pending"},
		},
	}
	WriteJSON(filepath.Join(dir, "tasks.json"), ts)

	loaded, err := LoadTaskState(root, recipeID)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(loaded.Tasks) != 2 {
		t.Errorf("got %d tasks, want 2", len(loaded.Tasks))
	}
	if loaded.Tasks[0].Status != "done" {
		t.Errorf("task 0 status: got %s, want done", loaded.Tasks[0].Status)
	}
}

func TestLoadTestResults(t *testing.T) {
	root := setupRecipeRoot(t)
	recipeID := "r-1000"
	dir := RecipeDir(root, recipeID)
	os.MkdirAll(dir, 0755)

	tr := &TestResults{
		RecipeID: recipeID,
		Status:   "pass",
		Total:    10,
		Passed:   9,
		Failed:   0,
		Skipped:  1,
	}
	WriteJSON(filepath.Join(dir, "test-results.json"), tr)

	loaded, err := LoadTestResults(root, recipeID)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded.Status != "pass" {
		t.Errorf("status: got %s, want pass", loaded.Status)
	}
	if loaded.Total != 10 {
		t.Errorf("total: got %d, want 10", loaded.Total)
	}
}

// saveTestRecipe is a helper to create a recipe in a given phase.
func saveTestRecipe(t *testing.T, root, id, phase string) {
	t.Helper()
	dir := RecipeDir(root, id)
	os.MkdirAll(dir, 0755)
	r := &RecipeState{
		ID:        id,
		Type:      "blueprint",
		Topic:     "test topic",
		Phase:     phase,
		StartedAt: "2026-01-01T00:00:00Z",
	}
	if err := SaveRecipeState(root, r); err != nil {
		t.Fatalf("save recipe %s failed: %v", id, err)
	}
}
