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

	// Extended fields for richer post-compact recovery
	Iteration       int              `json:"iteration,omitempty"`
	NextAction      string           `json:"next_action,omitempty"`
	PendingFindings int              `json:"pending_findings,omitempty"`
	SubState        *SubStateInfo    `json:"sub_state,omitempty"`
	RecentTools     []ToolTraceEntry `json:"recent_tools,omitempty"`
	OpenFiles       []string         `json:"open_files,omitempty"`
}

// SubStateInfo describes the position within a recipe sub-phase
// (debate, simulate, verify) so the session can resume precisely.
type SubStateInfo struct {
	Kind     string `json:"kind"`
	ID       string `json:"id,omitempty"`
	Position string `json:"position,omitempty"`
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
	return filepath.Join(LocalPath(root), "work-state.json")
}

// SaveWorkState persists the work state snapshot.
func SaveWorkState(root string, ws *WorkState) error {
	ws.SavedAt = time.Now().UTC().Format(time.RFC3339)
	capWorkState(ws)
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
		RecipeID:  recipe.ID,
		Phase:     recipe.Phase,
		Topic:     recipe.Topic,
		Iteration: recipe.Iteration,
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

	// Extended enrichments — all best-effort
	ws.NextAction = LoadAssessNextAction(root, recipe.ID)
	ws.PendingFindings = pendingFindingsFromVerifyLog(root, recipe.ID)
	ws.SubState = loadSubStateForRecipe(root, recipe)
	if tail, _ := TailToolTrace(root, 8); len(tail) > 0 {
		for _, e := range tail {
			if e != nil {
				ws.RecentTools = append(ws.RecentTools, *e)
			}
		}
		ws.OpenFiles = openFilesFromTrace(tail)
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

	// Sub-state (debate/simulate)
	if ws.SubState != nil {
		s := fmt.Sprintf("In %s", ws.SubState.Kind)
		if ws.SubState.Position != "" {
			s += ": " + ws.SubState.Position
		}
		parts = append(parts, s+".")
	}

	// Pending findings from verify loop
	if ws.PendingFindings > 0 {
		parts = append(parts, fmt.Sprintf("Pending findings: %d.", ws.PendingFindings))
	}

	// Last actions
	if len(ws.LastActions) > 0 {
		parts = append(parts, "Last: "+strings.Join(ws.LastActions, " → ")+".")
	}

	// Scope
	if ws.ScopeStatus != "" {
		parts = append(parts, "Scope: "+ws.ScopeStatus+".")
	}

	// Assess-driven next action
	if ws.NextAction != "" {
		parts = append(parts, "Next (assess): "+ws.NextAction+".")
	}

	// Most recent tool breadcrumb (1 entry)
	if len(ws.RecentTools) > 0 {
		last := ws.RecentTools[len(ws.RecentTools)-1]
		detail := last.ToolName
		if last.File != "" {
			detail += "(" + last.File + ")"
		}
		parts = append(parts, "Last tool: "+detail+".")
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

// LoadAssessNextAction reads .bts/specs/recipes/<id>/assess.json and returns
// the recommended next_action string. Returns "" if missing or malformed.
// The file is expected to be written by the bts-assess skill when it runs.
// Exported so callers (SessionStart) can fetch a fresh value instead of
// the possibly-stale one cached in work-state.
func LoadAssessNextAction(root, recipeID string) string {
	path := filepath.Join(RecipeDir(root, recipeID), "assess.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return ""
	}
	if s, ok := raw["next_action"].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// pendingFindingsFromVerifyLog reads the last entry of verify-log.jsonl and
// returns critical+major. Returns 0 if no log or on parse failure.
func pendingFindingsFromVerifyLog(root, recipeID string) int {
	path := filepath.Join(RecipeDir(root, recipeID), "verify-log.jsonl")
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	var lastLine string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lastLine = line
		}
	}
	if lastLine == "" {
		return 0
	}
	var entry VerifyLogEntry
	if err := json.Unmarshal([]byte(lastLine), &entry); err != nil {
		return 0
	}
	if entry.Status == "converged" {
		return 0
	}
	return entry.Critical + entry.Major
}

// loadSubStateForRecipe checks for an active debate or simulate state file
// under the recipe directory and returns a summary pointer.
//
// The recipe phase gates which sub-state to consult, so a leftover
// debate-state.json from an earlier phase does not pollute the hint once
// the recipe has moved on (e.g., to implement). Skills should still clean
// up their state files when done, but this filter is a safety net.
func loadSubStateForRecipe(root string, recipe *RecipeState) *SubStateInfo {
	if recipe == nil {
		return nil
	}
	if recipe.Phase == "debate" {
		if ds, _ := LoadDebateRound(root, recipe.ID); ds != nil {
			pos := fmt.Sprintf("round %d/%d", ds.Round, ds.TotalRounds)
			if ds.NextPersona != "" {
				pos += ", next: " + ds.NextPersona
			}
			return &SubStateInfo{Kind: "debate", ID: ds.DebateID, Position: pos}
		}
	}
	if recipe.Phase == "simulate" {
		if ss, _ := LoadSimulateProgress(root, recipe.ID); ss != nil {
			pos := fmt.Sprintf("scenario %d/%d", ss.ScenarioIdx, ss.TotalScenarios)
			if ss.FoundGaps > 0 {
				pos += fmt.Sprintf(", gaps: %d", ss.FoundGaps)
			}
			return &SubStateInfo{Kind: "simulate", ID: ss.SimulateID, Position: pos}
		}
	}
	return nil
}

// Work-state size caps.
//
// work-state.json is re-read at every session start and after every
// compaction, so its size is a recurring token cost paid on the way back
// into a task. A measured project's file reached 37KB — roughly 10k
// tokens — of which 17.8KB was `summary` and 17.6KB was five
// `last_actions` entries, one of them 8,352 characters. Nothing capped
// either: readLastChangelog copies a changelog `result` verbatim, and
// the model writes multi-paragraph results there on purpose.
//
// That prose is not lost by capping it. changelog.jsonl still holds it
// in full, and a resume hint that points at the record beats a resume
// hint that inlines it — especially since most of the length is a
// narrative about rounds that are already over.
const (
	maxActionChars  = 240
	maxSummaryChars = 2000
	maxCommandChars = 160
)

// clipMarker is long on purpose: a clipped entry has to tell the reader
// where the full text lives.
const clipMarker = "… (truncated; full text in changelog.jsonl)"

// TruncateRunes shortens s to at most max RUNES including a trailing "…",
// cutting only on a rune boundary. It never returns something longer than
// what it was given.
//
// Both properties were absent from the helpers this replaces. They tested
// the budget with len(s) — bytes — and then sliced []rune(s)[:max], so a
// Korean string sailed past a 2000-"char" cap at 4,935 bytes; and they
// appended the marker AFTER cutting to the full budget, so clipping a
// 250-byte action to a 240 cap produced 285 bytes. One of them sliced raw
// bytes at n-1 and returned invalid UTF-8 on any multi-byte input, and
// panicked outright at n <= 0.
func TruncateRunes(s string, max int) string { return truncateRunes(s, max, "…") }

func truncateRunes(s string, max int, marker string) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	m := []rune(marker)
	if len(m) >= max {
		// The budget cannot hold the marker; fall back to the shortest
		// one that still says "there is more than this".
		m = []rune("…")
	}
	if len(m) >= max {
		return string(r[:max])
	}
	return strings.TrimRight(string(r[:max-len(m)]), " \t\n") + string(m)
}

// clip truncates on a rune boundary and marks the cut, so a reader can
// tell an abridged entry from a short one and knows to go to the source.
// max is the total budget, marker included.
func clip(s string, max int) string { return truncateRunes(s, max, clipMarker) }

// capWorkState bounds every unbounded field before the snapshot is
// written. Applied at save time so it also shrinks state written by an
// older binary.
func capWorkState(ws *WorkState) {
	if ws == nil {
		return
	}
	for i := range ws.LastActions {
		ws.LastActions[i] = clip(ws.LastActions[i], maxActionChars)
	}
	ws.Summary = clip(ws.Summary, maxSummaryChars)
	for i := range ws.RecentTools {
		ws.RecentTools[i].Command = ClipCommand(ws.RecentTools[i].Command)
		ws.RecentTools[i].File = clip(ws.RecentTools[i].File, maxCommandChars)
	}
}

// ClipCommand trims a recorded command without leaving a fragment that
// reads as a whole one.
//
// The hooks cut at 100 bytes mid-token, which produced traces like
// `cd /Users/…/recipes/r-001 && ` — a command whose visible form is a
// shell prefix with its actual verb cut off. Trimming back to the last
// separator and marking the cut keeps the entry honest about being
// partial.
func ClipCommand(cmd string) string {
	cmd = strings.TrimRight(cmd, " \t\n")
	r := []rune(cmd)
	if len(r) <= maxCommandChars {
		return cmd
	}
	const marker = " …"
	keep := maxCommandChars - len([]rune(marker))
	cut := r[:keep]
	// Back up to the last whitespace, but only when that keeps most of
	// the budget — and measure that in RUNES. The byte index returned by
	// strings.LastIndexAny was being compared against a rune budget, so
	// on a Korean command the "keeps most of it" guard passed at a byte
	// offset worth a quarter of the allowance and threw the rest away.
	if i := lastIndexAnyRune(cut, " \t"); i > keep/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(strings.TrimRight(string(cut), " \t\n&|;"), " \t") + marker
}

func lastIndexAnyRune(r []rune, chars string) int {
	for i := len(r) - 1; i >= 0; i-- {
		if strings.ContainsRune(chars, r[i]) {
			return i
		}
	}
	return -1
}
