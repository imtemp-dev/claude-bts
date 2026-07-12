package state

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ChangelogEntry records one action in the recipe evolution.
type ChangelogEntry struct {
	Timestamp    string   `json:"time"`
	Action       string   `json:"action"`               // research, draft, improve, verify, debate, simulate, audit, assess, sync-check, finalize, implement, test, sync, status, adjudicate, review
	Input        string   `json:"input,omitempty"`       // what was acted on
	Output       string   `json:"output,omitempty"`      // what was produced
	BasedOn      []string `json:"based_on,omitempty"`    // dependencies
	Incorporates []string `json:"incorporates,omitempty"` // debates/sims incorporated
	Resolves     []string `json:"resolves,omitempty"`     // gaps resolved
	Result       string   `json:"result,omitempty"`       // summary (e.g., "0 critical, 2 major")
	Level        float64  `json:"level,omitempty"`        // level after this action
}

// ChangelogPath returns the changelog file path for a recipe.
func ChangelogPath(root, recipeID string) string {
	return filepath.Join(RecipeDir(root, recipeID), "changelog.jsonl")
}

// AppendChangelog adds an entry to the recipe's changelog.
func AppendChangelog(root, recipeID string, entry *ChangelogEntry) error {
	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	return AppendJSONL(ChangelogPath(root, recipeID), entry)
}

// ReadChangelog returns all parseable changelog entries in file order.
// Malformed lines are skipped — the changelog is append-only and one
// corrupt line must not invalidate the history. Used by the stop hook's
// simulate / sync-check-ordering gates.
func ReadChangelog(root, recipeID string) ([]ChangelogEntry, error) {
	f, err := os.Open(ChangelogPath(root, recipeID))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []ChangelogEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e ChangelogEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, scanner.Err()
}
