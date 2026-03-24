package metrics

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/jlim/claude-forge/internal/state"
)

// ReadEvents reads all metric events from a JSONL file.
func ReadEvents(path string) ([]MetricsEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var events []MetricsEvent
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var event MetricsEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue // skip malformed lines
		}
		events = append(events, event)
	}
	return events, nil
}

// ReadRecipeEvents reads events for a specific recipe.
func ReadRecipeEvents(root, recipeID string) ([]MetricsEvent, error) {
	path := filepath.Join(state.RecipeDir(root, recipeID), "metrics.jsonl")
	return ReadEvents(path)
}

// ReadAllEvents reads the global metrics log.
func ReadAllEvents(root string) ([]MetricsEvent, error) {
	path := filepath.Join(state.StatePath(root), "metrics.jsonl")
	return ReadEvents(path)
}
