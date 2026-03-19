package state

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// RecipeState tracks the current state of a recipe execution.
type RecipeState struct {
	ID           string  `json:"id"`
	Type         string  `json:"type"`          // analyze, design, blueprint
	Topic        string  `json:"topic"`         // user's description
	Phase        string  `json:"phase"`         // scoping, research, draft, assess, improve, verify, debate, simulate, audit, finalize, cancelled, implement, test, sync, status, complete
	Iteration    int     `json:"iteration"`     // current verify iteration
	DraftVersion int     `json:"draft_version"` // current draft version number (v1, v2, ...)
	Level        float64 `json:"level"`         // assessed document level (0.0 ~ 3.0)
	StartedAt    string  `json:"started_at"`
	UpdatedAt    string  `json:"updated_at"`
	RefRecipe    string  `json:"ref_recipe,omitempty"` // referenced recipe ID (for fix recipes)
}

// TaskState represents the tasks.json file for implementation tracking.
type TaskState struct {
	RecipeID  string `json:"recipe_id"`
	StartedAt string `json:"started_at"`
	UpdatedAt string `json:"updated_at"`
	Tasks     []Task `json:"tasks"`
}

// Task represents a single implementation task.
type Task struct {
	ID          string   `json:"id"`
	File        string   `json:"file"`
	Action      string   `json:"action"`      // create, modify
	Status      string   `json:"status"`      // pending, in_progress, done, blocked, skipped
	Description string   `json:"description"`
	DependsOn   []string `json:"depends_on,omitempty"`
	RetryCount  int      `json:"retry_count,omitempty"` // persisted build retry count
	LastError   string   `json:"last_error,omitempty"`  // last build error for stagnation detection
}

// TestResults represents the test-results.json file.
type TestResults struct {
	RecipeID   string   `json:"recipe_id"`
	RunAt      string   `json:"run_at"`
	Framework  string   `json:"framework"`
	Iterations int      `json:"iterations"`
	Status     string   `json:"status"` // pass, fail
	Total      int      `json:"total"`
	Passed     int      `json:"passed"`
	Failed     int      `json:"failed"`
	Skipped    int      `json:"skipped"`
	TestFiles  []string `json:"test_files,omitempty"`
}

// LoadTaskState reads tasks.json from a recipe directory.
func LoadTaskState(btsRoot, recipeID string) (*TaskState, error) {
	path := filepath.Join(RecipeDir(btsRoot, recipeID), "tasks.json")
	var ts TaskState
	if err := ReadJSON(path, &ts); err != nil {
		return nil, err
	}
	return &ts, nil
}

// LoadTestResults reads test-results.json from a recipe directory.
func LoadTestResults(btsRoot, recipeID string) (*TestResults, error) {
	path := filepath.Join(RecipeDir(btsRoot, recipeID), "test-results.json")
	var tr TestResults
	if err := ReadJSON(path, &tr); err != nil {
		return nil, err
	}
	return &tr, nil
}

// IsImplementPhase returns true if the phase is part of the implementation lifecycle.
func IsImplementPhase(phase string) bool {
	switch phase {
	case "implement", "test", "sync", "status":
		return true
	}
	return false
}

// VerifyLogEntry records one iteration of the verification loop.
type VerifyLogEntry struct {
	Iteration int    `json:"iteration"`
	Critical  int    `json:"critical"`
	Major     int    `json:"major"`
	Minor     int    `json:"minor"`
	Status    string `json:"status"` // continue, converged, failed
	Timestamp string `json:"timestamp"`
}

// RecipeDir returns the directory for a recipe's state.
func RecipeDir(btsRoot, recipeID string) string {
	return filepath.Join(StatePath(btsRoot), "recipes", recipeID)
}

// LoadRecipeState reads the recipe state file.
func LoadRecipeState(btsRoot, recipeID string) (*RecipeState, error) {
	path := filepath.Join(RecipeDir(btsRoot, recipeID), "recipe.json")
	var state RecipeState
	if err := ReadJSON(path, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// SaveRecipeState writes the recipe state file atomically.
func SaveRecipeState(btsRoot string, state *RecipeState) error {
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	path := filepath.Join(RecipeDir(btsRoot, state.ID), "recipe.json")
	return WriteJSON(path, state)
}

// AppendVerifyLog appends a verification log entry.
func AppendVerifyLog(btsRoot, recipeID string, entry *VerifyLogEntry) error {
	entry.Timestamp = time.Now().UTC().Format(time.RFC3339)
	path := filepath.Join(RecipeDir(btsRoot, recipeID), "verify-log.jsonl")
	return AppendJSONL(path, entry)
}

// GetActiveRecipe finds the currently active recipe, if any.
func GetActiveRecipe(btsRoot string) (*RecipeState, error) {
	recipesDir := filepath.Join(StatePath(btsRoot), "recipes")
	entries, err := os.ReadDir(recipesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		state, err := LoadRecipeState(btsRoot, entry.Name())
		if err != nil {
			continue
		}
		if state.Phase != "finalize" && state.Phase != "complete" && state.Phase != "cancelled" && state.Phase != "" {
			return state, nil
		}
	}

	return nil, nil
}

// GetFinalizedRecipe finds a recipe in "finalize" phase (ready for implementation).
func GetFinalizedRecipe(btsRoot string) (*RecipeState, error) {
	recipesDir := filepath.Join(StatePath(btsRoot), "recipes")
	entries, err := os.ReadDir(recipesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		state, err := LoadRecipeState(btsRoot, entry.Name())
		if err != nil {
			continue
		}
		if state.Phase == "finalize" {
			return state, nil
		}
	}

	return nil, nil
}

// ListRecipes returns all recipe states.
func ListRecipes(btsRoot string) ([]*RecipeState, error) {
	recipesDir := filepath.Join(StatePath(btsRoot), "recipes")
	entries, err := os.ReadDir(recipesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var recipes []*RecipeState
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		state, err := LoadRecipeState(btsRoot, entry.Name())
		if err != nil {
			continue
		}
		recipes = append(recipes, state)
	}

	return recipes, nil
}

// NewRecipeID generates a simple recipe ID.
func NewRecipeID() string {
	return fmt.Sprintf("r-%d", time.Now().UnixMilli())
}
