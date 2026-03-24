package state

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WorkState captures a snapshot of current work for context recovery.
type WorkState struct {
	RecipeID    string    `json:"recipe_id"`
	Phase       string    `json:"phase"`
	Topic       string    `json:"topic"`
	CurrentTask *TaskInfo `json:"current_task,omitempty"`
	LastActions []string  `json:"last_actions"`
	Summary     string    `json:"summary"`
	ScopeStatus string    `json:"scope_status,omitempty"`
	SavedAt     string    `json:"saved_at"`
}

// TaskInfo captures the active task for context recovery.
type TaskInfo struct {
	ID         string `json:"id"`
	File       string `json:"file"`
	Status     string `json:"status"`
	RetryCount int    `json:"retry_count"`
	LastError  string `json:"last_error,omitempty"`
}

// WorkStatePath returns the path to the work state file.
func WorkStatePath(root string) string {
	return filepath.Join(StatePath(root), "work-state.json")
}

// SaveWorkState persists the work state snapshot.
func SaveWorkState(root string, ws *WorkState) error {
	ws.SavedAt = time.Now().UTC().Format(time.RFC3339)
	return WriteJSON(WorkStatePath(root), ws)
}

// LoadWorkState reads the work state file. Returns nil, err if not found.
func LoadWorkState(root string) (*WorkState, error) {
	var ws WorkState
	if err := ReadJSON(WorkStatePath(root), &ws); err != nil {
		return nil, err
	}
	return &ws, nil
}

// BuildWorkState aggregates recipe, tasks, changelog, and scope into a snapshot.
func BuildWorkState(root string) (*WorkState, error) {
	recipe, err := GetActiveRecipe(root)
	if err != nil || recipe == nil {
		// Also check finalized recipes
		recipe, err = GetFinalizedRecipe(root)
		if err != nil || recipe == nil {
			return nil, nil
		}
	}

	ws := &WorkState{
		RecipeID: recipe.ID,
		Phase:    recipe.Phase,
		Topic:    recipe.Topic,
	}

	// Find in-progress task if in implementation phase
	if IsImplementPhase(recipe.Phase) {
		ts, err := LoadTaskState(root, recipe.ID)
		if err == nil && ts != nil {
			for _, t := range ts.Tasks {
				if t.Status == "in_progress" || t.Status == "pending" {
					ws.CurrentTask = &TaskInfo{
						ID:         t.ID,
						File:       t.File,
						Status:     t.Status,
						RetryCount: t.RetryCount,
						LastError:  t.LastError,
					}
					break // first non-done task
				}
			}
			// Count progress
			done, total := 0, len(ts.Tasks)
			for _, t := range ts.Tasks {
				if t.Status == "done" || t.Status == "skipped" {
					done++
				}
			}
			if total > 0 {
				ws.LastActions = append(ws.LastActions, fmt.Sprintf("tasks: %d/%d done", done, total))
			}
		}
	}

	// Read last 5 changelog entries
	changelogPath := filepath.Join(RecipeDir(root, recipe.ID), "changelog.jsonl")
	if actions := readLastChangelog(changelogPath, 5); len(actions) > 0 {
		ws.LastActions = append(ws.LastActions, actions...)
	}

	// Check scope status
	scopePath := filepath.Join(RecipeDir(root, recipe.ID), "scope.md")
	if data, err := os.ReadFile(scopePath); err == nil {
		content := string(data)
		if strings.Contains(content, "### Status: CONFIRMED") {
			ws.ScopeStatus = "CONFIRMED"
		} else if strings.Contains(content, "### Status: DRAFT") {
			ws.ScopeStatus = "DRAFT"
		}
	}

	// Build human-readable summary
	ws.Summary = buildSummary(ws, recipe)

	return ws, nil
}

func buildSummary(ws *WorkState, recipe *RecipeState) string {
	var parts []string

	// Header
	parts = append(parts, fmt.Sprintf("Recipe %s (%s) \"%s\" — phase: %s.",
		recipe.ID, recipe.Type, recipe.Topic, recipe.Phase))

	// Current task
	if ws.CurrentTask != nil {
		taskDesc := fmt.Sprintf("Task %s (%s) %s", ws.CurrentTask.ID, ws.CurrentTask.File, ws.CurrentTask.Status)
		if ws.CurrentTask.RetryCount > 0 {
			taskDesc += fmt.Sprintf(", retry %d/5", ws.CurrentTask.RetryCount)
		}
		if ws.CurrentTask.LastError != "" {
			taskDesc += fmt.Sprintf(", error: \"%s\"", ws.CurrentTask.LastError)
		}
		parts = append(parts, taskDesc+".")
	}

	// Last actions
	if len(ws.LastActions) > 0 {
		parts = append(parts, "Last: "+strings.Join(ws.LastActions, " → ")+".")
	}

	// Scope
	if ws.ScopeStatus != "" {
		parts = append(parts, "Scope: "+ws.ScopeStatus+".")
	}

	return strings.Join(parts, " ")
}

func readLastChangelog(path string, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var entries []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		action, _ := raw["action"].(string)
		output, _ := raw["output"].(string)
		result, _ := raw["result"].(string)

		desc := action
		if output != "" {
			desc += " " + output
		}
		if result != "" {
			desc += " (" + result + ")"
		}
		entries = append(entries, desc)
	}

	// Keep last N
	if len(entries) > n {
		entries = entries[len(entries)-n:]
	}
	return entries
}
