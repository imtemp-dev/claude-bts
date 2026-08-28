package state

import (
	"os"
	"path/filepath"
	"time"
)

// DebateRoundState tracks the position of an in-flight debate so it can
// be resumed after compaction. Written by jig-debate skill flow, read by
// work-state builder.
type DebateRoundState struct {
	DebateID     string   `json:"debate_id"`
	Round        int      `json:"round"`
	TotalRounds  int      `json:"total_rounds"`
	NextPersona  string   `json:"next_persona,omitempty"`
	PendingNotes []string `json:"pending_notes,omitempty"`
	UpdatedAt    string   `json:"updated_at"`
}

// SimulateProgressState tracks the cursor into scenario-based simulation.
type SimulateProgressState struct {
	SimulateID     string `json:"simulate_id"`
	ScenarioIdx    int    `json:"scenario_idx"`
	TotalScenarios int    `json:"total_scenarios"`
	FoundGaps      int    `json:"found_gaps"`
	UpdatedAt      string `json:"updated_at"`
}

// DebateRoundPath returns the path to the debate round state file for a recipe.
func DebateRoundPath(root, recipeID string) string {
	return filepath.Join(RecipeDir(root, recipeID), "debate-state.json")
}

// SimulateProgressPath returns the path to the simulate progress file.
func SimulateProgressPath(root, recipeID string) string {
	return filepath.Join(RecipeDir(root, recipeID), "simulate-state.json")
}

// SaveDebateRound writes the debate round state atomically.
func SaveDebateRound(root, recipeID string, s *DebateRoundState) error {
	if s == nil {
		return nil
	}
	s.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return WriteJSON(DebateRoundPath(root, recipeID), s)
}

// LoadDebateRound reads the debate round state. Returns (nil, nil) if the
// file doesn't exist so callers can treat "no active debate" uniformly.
func LoadDebateRound(root, recipeID string) (*DebateRoundState, error) {
	var s DebateRoundState
	if err := ReadJSON(DebateRoundPath(root, recipeID), &s); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// SaveSimulateProgress writes the simulate progress state atomically.
func SaveSimulateProgress(root, recipeID string, s *SimulateProgressState) error {
	if s == nil {
		return nil
	}
	s.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return WriteJSON(SimulateProgressPath(root, recipeID), s)
}

// LoadSimulateProgress reads the simulate progress. Returns (nil, nil) on ENOENT.
func LoadSimulateProgress(root, recipeID string) (*SimulateProgressState, error) {
	var s SimulateProgressState
	if err := ReadJSON(SimulateProgressPath(root, recipeID), &s); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}
