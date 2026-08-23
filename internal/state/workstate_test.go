package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupWorkStateRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".jig", "specs", "recipes"), 0755)
	os.MkdirAll(filepath.Join(root, ".jig", "local"), 0755)
	return root
}

func TestSaveAndLoadWorkState(t *testing.T) {
	root := setupWorkStateRoot(t)

	ws := &WorkState{
		RecipeID:    "r-1000",
		Phase:       "draft",
		Topic:       "OAuth2",
		LastActions: []string{"research done", "draft started"},
		Summary:     "Recipe r-1000 (spec) — phase: draft.",
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
	want := filepath.Join("/project", ".jig", "local", "work-state.json")
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestBuildWorkState_NextActionFromAssess(t *testing.T) {
	root := setupWorkStateRoot(t)
	recipeID := "r-5000"
	saveTestRecipe(t, root, recipeID, "draft")

	assessPath := filepath.Join(RecipeDir(root, recipeID), "assess.json")
	payload := `{"next_action":"Add data flow diagram to section 3."}`
	if err := os.WriteFile(assessPath, []byte(payload), 0644); err != nil {
		t.Fatalf("seed assess: %v", err)
	}

	ws, err := BuildWorkState(root)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if ws.NextAction != "Add data flow diagram to section 3." {
		t.Errorf("NextAction: %q", ws.NextAction)
	}
	if !strings.Contains(ws.Summary, "Next (assess):") {
		t.Errorf("Summary missing assess hint: %s", ws.Summary)
	}
}

func TestBuildWorkState_PendingFindings(t *testing.T) {
	root := setupWorkStateRoot(t)
	recipeID := "r-5100"
	saveTestRecipe(t, root, recipeID, "verify")

	logPath := filepath.Join(RecipeDir(root, recipeID), "verify-log.jsonl")
	lines := `{"iteration":1,"critical":1,"major":2,"minor":0,"status":"continue"}` + "\n" +
		`{"iteration":2,"critical":0,"major":3,"minor":1,"status":"continue"}` + "\n"
	if err := os.WriteFile(logPath, []byte(lines), 0644); err != nil {
		t.Fatalf("seed verify-log: %v", err)
	}

	ws, err := BuildWorkState(root)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if ws.PendingFindings != 3 {
		t.Errorf("PendingFindings: got %d, want 3 (0 critical + 3 major)", ws.PendingFindings)
	}
}

func TestBuildWorkState_PendingFindings_Converged(t *testing.T) {
	root := setupWorkStateRoot(t)
	recipeID := "r-5110"
	saveTestRecipe(t, root, recipeID, "verify")

	logPath := filepath.Join(RecipeDir(root, recipeID), "verify-log.jsonl")
	line := `{"iteration":3,"critical":0,"major":0,"minor":1,"status":"converged"}` + "\n"
	if err := os.WriteFile(logPath, []byte(line), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ws, err := BuildWorkState(root)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if ws.PendingFindings != 0 {
		t.Errorf("converged should yield 0, got %d", ws.PendingFindings)
	}
}

func TestBuildWorkState_SubStateDebate(t *testing.T) {
	root := setupWorkStateRoot(t)
	recipeID := "r-5200"
	saveTestRecipe(t, root, recipeID, "debate")

	_ = SaveDebateRound(root, recipeID, &DebateRoundState{
		DebateID:    "d-1",
		Round:       2,
		TotalRounds: 3,
		NextPersona: "security",
	})

	ws, err := BuildWorkState(root)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if ws.SubState == nil {
		t.Fatal("expected SubState")
	}
	if ws.SubState.Kind != "debate" || ws.SubState.ID != "d-1" {
		t.Errorf("SubState: %+v", ws.SubState)
	}
	if !strings.Contains(ws.SubState.Position, "round 2/3") || !strings.Contains(ws.SubState.Position, "security") {
		t.Errorf("Position: %q", ws.SubState.Position)
	}
	if !strings.Contains(ws.Summary, "In debate:") {
		t.Errorf("Summary missing sub-state: %s", ws.Summary)
	}
}

func TestBuildWorkState_SubStateIgnoredOutsidePhase(t *testing.T) {
	// Leftover debate-state.json from an earlier phase should NOT surface
	// once the recipe has moved on (e.g., to implement). Otherwise hints
	// would wrongly say "Resume debate" during implementation.
	root := setupWorkStateRoot(t)
	recipeID := "r-5210"
	saveTestRecipe(t, root, recipeID, "implement")

	_ = SaveDebateRound(root, recipeID, &DebateRoundState{
		DebateID: "d-stale", Round: 2, TotalRounds: 3, NextPersona: "security",
	})

	ws, err := BuildWorkState(root)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if ws.SubState != nil {
		t.Errorf("SubState should be nil in implement phase, got %+v", ws.SubState)
	}
	if strings.Contains(ws.Summary, "In debate") {
		t.Errorf("Summary should not mention debate: %s", ws.Summary)
	}
}

func TestBuildWorkState_SubStateSimulate(t *testing.T) {
	root := setupWorkStateRoot(t)
	recipeID := "r-5220"
	saveTestRecipe(t, root, recipeID, "simulate")

	_ = SaveSimulateProgress(root, recipeID, &SimulateProgressState{
		SimulateID: "s-1", ScenarioIdx: 3, TotalScenarios: 8, FoundGaps: 2,
	})

	ws, err := BuildWorkState(root)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if ws.SubState == nil {
		t.Fatal("expected simulate SubState")
	}
	if ws.SubState.Kind != "simulate" {
		t.Errorf("Kind: %s", ws.SubState.Kind)
	}
	if !strings.Contains(ws.SubState.Position, "scenario 3/8") {
		t.Errorf("Position: %q", ws.SubState.Position)
	}
	if !strings.Contains(ws.SubState.Position, "gaps: 2") {
		t.Errorf("gaps missing: %q", ws.SubState.Position)
	}
}

func TestBuildWorkState_RecentTools(t *testing.T) {
	root := setupWorkStateRoot(t)
	recipeID := "r-5300"
	saveTestRecipe(t, root, recipeID, "draft")

	_ = AppendToolTrace(root, &ToolTraceEntry{Phase: "post", ToolName: "Read", File: "draft.md"})
	_ = AppendToolTrace(root, &ToolTraceEntry{Phase: "post", ToolName: "Edit", File: "spec.md"})

	ws, err := BuildWorkState(root)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(ws.RecentTools) != 2 {
		t.Errorf("RecentTools: want 2, got %d", len(ws.RecentTools))
	}
	if len(ws.OpenFiles) != 2 {
		t.Errorf("OpenFiles: want 2, got %v", ws.OpenFiles)
	}
	if !strings.Contains(ws.Summary, "Last tool: Edit(spec.md)") {
		t.Errorf("Summary missing last tool: %s", ws.Summary)
	}
}

func TestBuildWorkState_Iteration(t *testing.T) {
	root := setupWorkStateRoot(t)
	recipeID := "r-5400"
	r := &RecipeState{
		ID: recipeID, Type: "spec", Topic: "x", Phase: "verify", Iteration: 3,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := SaveRecipeState(root, r); err != nil {
		t.Fatalf("save recipe: %v", err)
	}
	ws, err := BuildWorkState(root)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if ws.Iteration != 3 {
		t.Errorf("Iteration: got %d, want 3", ws.Iteration)
	}
}
