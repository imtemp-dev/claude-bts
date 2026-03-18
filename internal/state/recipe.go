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
	Phase        string  `json:"phase"`         // research, draft, assess, improve, verify, debate, simulate, audit, finalize, cancelled
	Iteration    int     `json:"iteration"`     // current verify iteration
	DraftVersion int     `json:"draft_version"` // current draft version number (v1, v2, ...)
	Level        float64 `json:"level"`         // assessed document level (0.0 ~ 3.0)
	StartedAt    string  `json:"started_at"`
	UpdatedAt    string  `json:"updated_at"`
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
		if state.Phase != "finalize" && state.Phase != "" {
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
