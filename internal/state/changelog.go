package state

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/imtemp-dev/claude-bts/pkg/version"
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
	// ImplHash is ImplHash over the task files when the action ran. Set
	// on test and review entries, so a review can be matched to the code
	// it judged and the tests that ran on it.
	ImplHash string `json:"impl_hash,omitempty"`
	// BtsVersion is the bts build that wrote the entry
	// (version.GetTemplateVersion), stamped by AppendChangelog. r-003
	// started on one build and finished on another because a session start
	// updated the templates mid-recipe, and nothing in its record said which
	// build ran which phase (trial problem #55). Absent on entries written
	// before the field existed.
	BtsVersion string `json:"bts_version,omitempty"`
}

// ChangelogPath returns the changelog file path for a recipe.
func ChangelogPath(root, recipeID string) string {
	return filepath.Join(RecipeDir(root, recipeID), "changelog.jsonl")
}

// AppendChangelog adds an entry to the recipe's changelog, stamped with
// the time and the bts build that wrote it.
func AppendChangelog(root, recipeID string, entry *ChangelogEntry) error {
	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	if entry.BtsVersion == "" {
		entry.BtsVersion = version.GetTemplateVersion()
	}
	return AppendJSONL(ChangelogPath(root, recipeID), entry)
}

// LogPhaseChange records a phase change in the changelog as action
// "phase", with the bts build that made it. Every place that moves a
// recipe's phase calls it beside its phase_change metric: the metric lives
// in .bts/local and is not committed, so without this entry the recipe's
// own history could not say which build ran which phase (trial problem
// #55). from is "" for a recipe's first phase.
func LogPhaseChange(root, recipeID, from, to string) error {
	// A move to the phase the recipe is already in is no transition: it
	// wrote "discovery → discovery" rows that `bts stats` printed as extra
	// 0 s stages (trial problem #94).
	if from == to {
		return nil
	}
	if PhaseObserver != nil {
		PhaseObserver(root, recipeID, from, to)
	}
	if from == "" {
		from = "(new)"
	}
	return AppendChangelog(root, recipeID, &ChangelogEntry{
		Action: "phase",
		Result: from + " → " + to,
	})
}

// PhaseObserver, when set, is told of every phase change LogPhaseChange
// records, which is every phase change: `bts recipe create`, `bts recipe
// log --phase`, reconcile and the Stop hook's finalize and complete all
// go through it.  from is "" for a new recipe.  The cli package sets it to
// mirror phases onto a Multica issue; it must not fail or block for long.
var PhaseObserver func(root, recipeID, from, to string)

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
