package state

import (
	"os"
	"path/filepath"
	"time"
)

// DebateRoundState tracks the position of an in-flight debate so it can
// be resumed after compaction. Written by bts-debate skill flow, read by
// work-state builder.
type DebateRoundState struct {
	DebateID     string   `json:"debate_id"`
	Round        int      `json:"round"`
	TotalRounds  int      `json:"total_rounds"`
	NextPersona  string   `json:"next_persona,omitempty"`
	PendingNotes []string `json:"pending_notes,omitempty"`
	UpdatedAt    string   `json:"updated_at"`
}

// DebateRoundPath returns the path to the debate round state file for a recipe.
func DebateRoundPath(root, recipeID string) string {
	return filepath.Join(RecipeDir(root, recipeID), "debate-state.json")
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
