package state

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DebateState tracks the current state of a debate.
type DebateState struct {
	ID         string `json:"id"`
	Topic      string `json:"topic"`
	Rounds     int    `json:"rounds"`
	Conclusion string `json:"conclusion,omitempty"`
	Decided    bool   `json:"decided"`
	StartedAt  string `json:"started_at"`
	UpdatedAt  string `json:"updated_at"`
}

// DebateDir returns the directory for a debate's state.
func DebateDir(root, debateID string) string {
	return filepath.Join(StatePath(root), "debates", debateID)
}

// LoadDebateState reads the debate state file.
func LoadDebateState(root, debateID string) (*DebateState, error) {
	path := filepath.Join(DebateDir(root, debateID), "debate.json")
	var state DebateState
	if err := ReadJSON(path, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// SaveDebateState writes the debate state file atomically.
func SaveDebateState(root string, state *DebateState) error {
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	path := filepath.Join(DebateDir(root, state.ID), "debate.json")
	return WriteJSON(path, state)
}

// ListDebates returns all debate states.
func ListDebates(root string) ([]*DebateState, error) {
	debatesDir := filepath.Join(StatePath(root), "debates")
	entries, err := os.ReadDir(debatesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var debates []*DebateState
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		ds, err := LoadDebateState(root, entry.Name())
		if err != nil {
			continue
		}
		debates = append(debates, ds)
	}

	return debates, nil
}

// NewDebateID generates a simple debate ID.
func NewDebateID() string {
	return fmt.Sprintf("d-%d", time.Now().UnixMilli())
}
