package state

import (
	"path/filepath"
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
func ChangelogPath(btsRoot, recipeID string) string {
	return filepath.Join(RecipeDir(btsRoot, recipeID), "changelog.jsonl")
}

// AppendChangelog adds an entry to the recipe's changelog.
func AppendChangelog(btsRoot, recipeID string, entry *ChangelogEntry) error {
	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	return AppendJSONL(ChangelogPath(btsRoot, recipeID), entry)
}
