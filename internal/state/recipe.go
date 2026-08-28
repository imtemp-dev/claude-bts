package state

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// RecipeState tracks the current state of a recipe execution.
type RecipeState struct {
	ID           string  `json:"id"`
	Type         string  `json:"type"`                    // analyze, design, blueprint
	Topic        string  `json:"topic"`                   // user's description
	Phase        string  `json:"phase"`                   // scoping, research, draft, assess, improve, verify, debate, simulate, audit, finalize, cancelled, implement, test, sync, status, complete
	Iteration    int     `json:"iteration"`               // current verify iteration
	DraftVersion int     `json:"draft_version,omitempty"` // deprecated: single draft.md, no versioning
	Level        float64 `json:"level"`                   // assessed document level (0.0 ~ 3.0)
	StartedAt    string  `json:"started_at"`
	UpdatedAt    string  `json:"updated_at"`
	RefRecipe    string  `json:"ref_recipe,omitempty"` // referenced recipe ID (for fix recipes)
}

// TaskState represents the tasks.json file for implementation tracking.
type TaskState struct {
	RecipeID  string `json:"recipe_id"`
	StartedAt string `json:"started_at"`
	UpdatedAt string `json:"updated_at"`
	Tasks     []Task `json:"tasks"`
}

// Task represents a single implementation task.
//
// Anchor carries the exact `<!-- task-anchor: {file} {action} -->` string
// minus the HTML comment wrapper — i.e. the "path action" body. Phase 9
// requires every Task to reference an anchor declared in final.md and
// `bts verify` enforces the 1:1 mapping (CheckTaskAnchors).
//
// ModifyScope (Phase 14) is required when Action=="modify": the list of
// symbol names the task is authorized to touch. The anchor comment
// records the same list after a `scope=` suffix (e.g.
// `<!-- task-anchor: foo.ts modify scope=a,b -->`). Changes that reach
// symbols outside this list raise `scope_violation` at `bts verify`
// time.
type Task struct {
	ID                string             `json:"id"`
	File              string             `json:"file"`
	Action            string             `json:"action"` // create, modify, delete
	Status            string             `json:"status"` // pending, in_progress, done, blocked, skipped
	Description       string             `json:"description"`
	Anchor            string             `json:"anchor,omitempty"`             // "path action" — matches <!-- task-anchor: path action -->
	ModifyScope       []string           `json:"modify_scope,omitempty"`       // required when Action=="modify"
	PreImageSha       string             `json:"pre_image_sha,omitempty"`      // sha256 of file before IMPLEMENT
	PostImageSha      string             `json:"post_image_sha,omitempty"`     // sha256 after VERIFY build pass
	StructureFindings []StructureFinding `json:"structure_findings,omitempty"` // per-task mini-check results (Phase 10)
	DependsOn         []string           `json:"depends_on,omitempty"`
	RetryCount        int                `json:"retry_count,omitempty"`      // persisted TOTAL build retry count (hard-cap budget)
	AttemptsInTier    int                `json:"attempts_in_tier,omitempty"` // Phase 15: attempts within the CURRENT tier — reset to 0 on every tier transition
	LastError         string             `json:"last_error,omitempty"`       // last build error for stagnation detection
	RetryTier         int                `json:"retry_tier,omitempty"`       // Phase 15 retry-ladder tier: 1..5
	EscalationNotes   []string           `json:"escalation_notes,omitempty"` // one entry per tier transition
}

// StructureFinding records one per-task structural issue surfaced by
// Phase 10's MINI-CHECK. Categories: import_drift, symbol_missing,
// owner_drift. Severity stays below the task-blocking threshold
// (major/minor) unless the category is a hard invariant breach
// (critical).
type StructureFinding struct {
	TaskID   string `json:"task_id"`
	Category string `json:"category"` // import_drift | symbol_missing | owner_drift
	Severity string `json:"severity"` // critical | major | minor
	Detail   string `json:"detail"`
}

// TestResults represents the test-results.json file.
type TestResults struct {
	RecipeID   string   `json:"recipe_id"`
	RunAt      string   `json:"run_at"`
	Framework  string   `json:"framework"`
	Iterations int      `json:"iterations"`
	Status     string   `json:"status"` // pass, fail
	Total      int      `json:"total"`
	Passed     int      `json:"passed"`
	Failed     int      `json:"failed"`
	Skipped    int      `json:"skipped"`
	TestFiles  []string `json:"test_files,omitempty"`
	// Machine-truth fields written by `bts test run`. RecordedBy=="bts"
	// means Status was derived from the actual exit code by the CLI, not
	// transcribed by the orchestrator. Legacy hand-written files lack
	// these; `bts doctor` flags them.
	ExitCode   int    `json:"exit_code"`
	Command    string `json:"command,omitempty"`
	RecordedBy string `json:"recorded_by,omitempty"`
}

// SaveTestResults persists test-results.json for a recipe.
func SaveTestResults(root, recipeID string, tr *TestResults) error {
	return WriteJSON(filepath.Join(RecipeDir(root, recipeID), "test-results.json"), tr)
}

// ReadVerifyLog returns all verify-log entries in file order.
// Malformed lines are skipped; a missing file returns nil, nil.
func ReadVerifyLog(root, recipeID string) ([]VerifyLogEntry, error) {
	f, err := os.Open(filepath.Join(RecipeDir(root, recipeID), "verify-log.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []VerifyLogEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e VerifyLogEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, scanner.Err()
}

// VerifyEntriesForDoc narrows a verify-log to one document's history.
//
// Entries carrying no Doc are legacy (pre-v0.10), when every document
// shared a single stream. If the log has no doc-scoped entries at all,
// the whole stream is returned unchanged so legacy recipes keep the
// convergence verdict they already had rather than suddenly reading as
// unverified. Once any entry records a Doc, only matching entries count
// for that document — that is the point of the field.
//
// docBase is matched on basename, so callers may pass either
// "draft.md" or ".bts/specs/recipes/r-001/draft.md". An empty docBase
// means "no document in particular" and returns the whole stream —
// without this guard filepath.Base("") would be "." and match nothing.
func VerifyEntriesForDoc(entries []VerifyLogEntry, docBase string) []VerifyLogEntry {
	if docBase == "" {
		return entries
	}
	scoped := false
	for i := range entries {
		if entries[i].Doc != "" {
			scoped = true
			break
		}
	}
	if !scoped {
		return entries
	}
	want := filepath.Base(docBase)
	var out []VerifyLogEntry
	for i := range entries {
		if entries[i].Doc == want {
			out = append(out, entries[i])
		}
	}
	return out
}

// LastVerifyEntryForDoc returns the most recent entry describing docBase,
// or nil when that document has never been verified. A nil result with a
// nil error means "no history for this doc" — distinct from a read error.
func LastVerifyEntryForDoc(root, recipeID, docBase string) (*VerifyLogEntry, error) {
	entries, err := ReadVerifyLog(root, recipeID)
	if err != nil {
		return nil, err
	}
	scoped := VerifyEntriesForDoc(entries, docBase)
	if len(scoped) == 0 {
		return nil, nil
	}
	last := scoped[len(scoped)-1]
	return &last, nil
}

// LoadTaskState reads tasks.json from a recipe directory.
func LoadTaskState(root, recipeID string) (*TaskState, error) {
	path := filepath.Join(RecipeDir(root, recipeID), "tasks.json")
	var ts TaskState
	if err := ReadJSON(path, &ts); err != nil {
		return nil, err
	}
	return &ts, nil
}

// LoadTestResults reads test-results.json from a recipe directory.
func LoadTestResults(root, recipeID string) (*TestResults, error) {
	path := filepath.Join(RecipeDir(root, recipeID), "test-results.json")
	var tr TestResults
	if err := ReadJSON(path, &tr); err != nil {
		return nil, err
	}
	return &tr, nil
}

// IsImplementPhase returns true if the phase is part of the implementation lifecycle.
func IsImplementPhase(phase string) bool {
	switch phase {
	case "implement", "test", "review", "sync", "status":
		return true
	}
	return false
}

// VerifyLogEntry records one iteration of the verification loop.
//
// Minor findings are split into [resolvable] (fixable in the spec, block
// completion) and [deferred] (runtime-observable, do not block). CRITICAL
// and MAJOR never block-split — they always block. INFO is tracked for
// telemetry only.
//
// Legacy entries written before the split carry only the Minor field.
// Readers should treat Minor as MinorResolvable when MinorResolvable and
// MinorDeferred are both zero (see stop hook and CLI log handler).
// Doc scopes an entry to the document that was verified. Entries written
// before v0.10 carry no Doc: every document shared one iteration counter
// and one convergence verdict, so verifying wireframe.md could satisfy
// (or reopen) draft.md's gate. Readers must treat "" as the legacy
// undifferentiated stream — see VerifyEntriesForDoc.
//
// FullPass records whether the round re-verified the whole document or
// only the changed sections plus their reference closure. Delta rounds
// are cheap but are not sufficient evidence for finalization; the stop
// hook requires the last entry to be a full pass.
type VerifyLogEntry struct {
	Iteration       int    `json:"iteration"`
	Critical        int    `json:"critical"`
	Major           int    `json:"major"`
	Minor           int    `json:"minor,omitempty"` // legacy pre-split count
	MinorResolvable int    `json:"minor_resolvable,omitempty"`
	MinorDeferred   int    `json:"minor_deferred,omitempty"`
	Info            int    `json:"info,omitempty"`
	Doc             string `json:"doc,omitempty"`       // basename of the verified document
	FullPass        bool   `json:"full_pass,omitempty"` // whole-document round (vs. scoped delta)
	// Dimensions names the semantic passes that produced this round's
	// counts: "verify" (logical consistency), "audit" (completeness),
	// "simulate" (scenario coverage). Empty means "written before this
	// field existed" — see StrengthClass.
	//
	// Without it the log records HOW MUCH of the document was read
	// (FullPass) but not WHICH INSTRUMENTS read it, and the convergence
	// budget compared the two as if they were the same measurement. A
	// verify-only round finds less than a verify+audit+simulate round on
	// identical text — not because the text improved, but because one
	// instrument was pointed at it instead of three. A measured recipe
	// set its best triple at (0,0,2) on a verify-only round, ran its
	// first audit fifteen rounds later and got 13 findings including 4
	// majors on that same text, and then had every subsequent
	// multi-dimension round judged "no progress" against a number one
	// dimension had produced. The operator raised verify.max_iterations
	// twice to work around a verdict that was an artefact of this
	// missing field.
	Dimensions []string `json:"dimensions,omitempty"`
	Status     string   `json:"status"` // continue, converged, failed
	// Budget is the verify.max_iterations value in effect when this round
	// was judged. Without it the log is not self-describing: the
	// convergence verdict is recomputed over the WHOLE history using
	// whatever settings.yaml says today (cli/recipe.go, recipe_findings.go),
	// so lowering or raising the budget silently re-judges every past
	// round, and a stored Status:"failed" written under one budget can
	// disagree with a fresh evaluation under another. Recording the
	// budget per round makes that regime change visible instead of
	// silent. 0 means "written before this field existed".
	Budget int `json:"budget,omitempty"`
	// AgentEvidence records whether any subagent finished between the
	// previous round and this one: "observed", "none", or "" for rounds
	// written before the field existed.
	//
	// bts's central claim is that verification runs in a context that
	// does not share the writing session's blind spots. The gate skills
	// carry `context: fork` and spawn their own agent, so the isolation
	// is harness-enforced — but the ORCHESTRATOR is what writes
	// verification.md and runs `bts recipe log`, and nothing tied that
	// write to a fork having run. This field is that tie.
	//
	// It is evidence, not proof, and deliberately not a gate: a missing
	// signal can mean the harness does not emit subagent events here, not
	// that anyone cheated. `bts doctor` only reports it when the same
	// project has ALSO produced rounds with evidence, which is the only
	// case where absence is informative.
	AgentEvidence string `json:"agent_evidence,omitempty"`
	// DocHash is the content hash of the verified document, and
	// VerificationHash the content hash of the recipe's verification.md,
	// both as of the moment this round was recorded.
	//
	// These exist because the two rule-3 gates used to compare against
	// state that does not survive a checkout. Dirty-doc detection read
	// .bts/local/verify-snapshots/, which is gitignored, so the gate was
	// silently inert in every worktree and fresh clone. The
	// unrecorded-verification gate compared verification.md's mtime
	// against this timestamp, and `git checkout` stamps mtime with the
	// checkout time, so it fired on every fresh worktree of a recipe that
	// had already been verified.
	//
	// A hash in the tracked verify-log fixes both: it travels with the
	// branch, it is independent of mtime, and it answers the question the
	// gates actually ask — is the file on disk the one that was verified.
	// Empty means "written before these fields existed"; both gates fall
	// back rather than manufacture a verdict from a missing hash.
	DocHash string `json:"doc_hash,omitempty"`
	// DocPath is the verified document's path relative to the project
	// root. Doc carries only the basename, and the rule-3 dirty check
	// looked the basename up under the RECIPE directory — so a --doc that
	// legitimately resolved elsewhere (`docs/api-spec.md` from the
	// project root) got a doc_hash recorded and then never re-checked:
	// FileContentHash returned ok=false on the wrong path and the check
	// skipped it. Empty means "written before this field existed" or "the
	// document lives in the recipe directory", which is the fallback.
	DocPath          string `json:"doc_path,omitempty"`
	VerificationHash string `json:"verification_hash,omitempty"`
	Timestamp        string `json:"timestamp"`
}

// Agent evidence values for VerifyLogEntry.AgentEvidence.
const (
	AgentEvidenceObserved = "observed"
	AgentEvidenceNone     = "none"
)

// BudgetDrift reports the most recent budget recorded in entries that
// differs from current. ok=false when every recorded budget matches, when
// none of the entries recorded one (legacy log), or when current is
// non-positive (budget disabled). Callers use it to surface a regime
// change at log time rather than letting the verdict shift silently.
func BudgetDrift(entries []VerifyLogEntry, current int) (int, bool) {
	if current <= 0 {
		return 0, false
	}
	for i := len(entries) - 1; i >= 0; i-- {
		if b := entries[i].Budget; b > 0 {
			if b != current {
				return b, true
			}
			return 0, false
		}
	}
	return 0, false
}

// NextIteration returns the round number to record next for a document's
// history.
//
// It is the MAXIMUM plus one, not the last entry's number plus one,
// because "last + 1" cannot recover from a log that already holds a bad
// value. A measured recipe recorded its seventeenth round as iteration 0
// (the numbering used to be skipped on one of the two logging paths),
// and the next round then numbered itself 1 — following the 0 rather
// than the sixteen rounds before it, so the document's history read as
// though it had restarted twice.
//
// An empty history starts at 1.
func NextIteration(entries []VerifyLogEntry) int {
	max := 0
	for i := range entries {
		if entries[i].Iteration > max {
			max = entries[i].Iteration
		}
	}
	return max + 1
}

// EffectiveResolvable returns the resolvable-minor count to use for gating,
// falling back to legacy Minor when the split fields are absent.
func (e *VerifyLogEntry) EffectiveResolvable() int {
	if e.MinorResolvable == 0 && e.MinorDeferred == 0 && e.Minor > 0 {
		return e.Minor
	}
	return e.MinorResolvable
}

// VerifyDimensions are the semantic passes a verify round may run. They
// mirror the three fork-context checks in
// bts-verification-protocol.md § Core Principle; the deterministic
// `bts verify` pass is not one of them because it is not a sample — it
// returns the same answer on the same bytes every time.
var VerifyDimensions = []string{"verify", "audit", "simulate"}

// NormalizeDimensions lowercases, de-duplicates and sorts a dimension
// list into its canonical form, rejecting names outside
// VerifyDimensions. A nil or empty input returns nil, which is the
// "not recorded" form legacy entries carry.
func NormalizeDimensions(dims []string) ([]string, error) {
	seen := make(map[string]bool, len(dims))
	var out []string
	for _, d := range dims {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" {
			continue
		}
		if !slices.Contains(VerifyDimensions, d) {
			return nil, fmt.Errorf("unknown dimension %q (want one of %s)",
				d, strings.Join(VerifyDimensions, ", "))
		}
		if seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	slices.Sort(out)
	return out, nil
}

// StrengthClass names the measurement a round performed: which
// instruments ran, over how much of the document. Two rounds are
// comparable — one can be said to have improved on the other — only
// when their classes match.
//
// Legacy rounds that recorded no dimensions form their own class
// ("?"), so an old log keeps behaving exactly as it did: every entry
// in it is comparable with every other, bucketed only by scope.
func (e *VerifyLogEntry) StrengthClass() string {
	dims := "?"
	if len(e.Dimensions) > 0 {
		dims = strings.Join(e.Dimensions, "+")
	}
	scope := "delta"
	if e.FullPass {
		scope = "full"
	}
	return dims + "/" + scope
}

// HasAllDimensions reports whether this round ran every semantic pass.
// The completion gate uses it: a clean triple from one instrument is
// not evidence that three instruments would agree.
func (e *VerifyLogEntry) HasAllDimensions() bool {
	for _, want := range VerifyDimensions {
		if !slices.Contains(e.Dimensions, want) {
			return false
		}
	}
	return true
}

// RecipeDir returns the directory for a recipe's state.
func RecipeDir(root, recipeID string) string {
	return filepath.Join(SpecsPath(root), "recipes", recipeID)
}

// LoadRecipeState reads the recipe state file.
func LoadRecipeState(root, recipeID string) (*RecipeState, error) {
	path := filepath.Join(RecipeDir(root, recipeID), "recipe.json")
	var state RecipeState
	if err := ReadJSON(path, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// SaveRecipeState writes the recipe state file atomically.
func SaveRecipeState(root string, state *RecipeState) error {
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	path := filepath.Join(RecipeDir(root, state.ID), "recipe.json")
	return WriteJSON(path, state)
}

// AppendVerifyLog appends a verification log entry.
func AppendVerifyLog(root, recipeID string, entry *VerifyLogEntry) error {
	entry.Timestamp = time.Now().UTC().Format(time.RFC3339)
	path := filepath.Join(RecipeDir(root, recipeID), "verify-log.jsonl")
	return AppendJSONL(path, entry)
}

// LastVerifyEntry returns the most recent verify-log entry for a recipe,
// or an error when the log is absent, unreadable, or has no valid
// entries. Shared by the stop hook (normalization on <bts>DONE</bts>)
// and `bts recipe reconcile` (manual recovery when DONE was never
// emitted). Previously the hook had its own private copy; centralising
// the parse keeps behaviour in lockstep.
func LastVerifyEntry(root, recipeID string) (*VerifyLogEntry, error) {
	path := filepath.Join(RecipeDir(root, recipeID), "verify-log.jsonl")
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var last VerifyLogEntry
	found := false
	sc := bufio.NewScanner(f)
	// verify-log entries can grow with long citations/evidence notes;
	// use a generous buffer so we never drop an entry silently.
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var entry VerifyLogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			// Skip malformed lines — they are diagnostic data and
			// shouldn't abort reconcile.
			continue
		}
		last = entry
		found = true
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("empty verify log: %s", path)
	}
	return &last, nil
}

// GetActiveRecipe finds the currently active recipe, if any.
func GetActiveRecipe(root string) (*RecipeState, error) {
	recipesDir := filepath.Join(SpecsPath(root), "recipes")
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
		state, err := LoadRecipeState(root, entry.Name())
		if err != nil {
			continue
		}
		if state.Phase != "finalize" && state.Phase != "complete" && state.Phase != "cancelled" && state.Phase != "" {
			return state, nil
		}
	}

	return nil, nil
}

// GetFinalizedRecipe finds a recipe in "finalize" phase (ready for implementation).
func GetFinalizedRecipe(root string) (*RecipeState, error) {
	recipesDir := filepath.Join(SpecsPath(root), "recipes")
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
		state, err := LoadRecipeState(root, entry.Name())
		if err != nil {
			continue
		}
		if state.Phase == "finalize" {
			return state, nil
		}
	}

	return nil, nil
}

// ListRecipes returns all recipe states.
func ListRecipes(root string) ([]*RecipeState, error) {
	recipesDir := filepath.Join(SpecsPath(root), "recipes")
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
		state, err := LoadRecipeState(root, entry.Name())
		if err != nil {
			continue
		}
		recipes = append(recipes, state)
	}

	return recipes, nil
}

// NewRecipeID generates a sequential recipe ID with topic slug.
// Format: r-NNN-slug (e.g., r-001-mcp-server, r-002-oauth2-auth)
func NewRecipeID(root, topic string) string {
	recipesDir := filepath.Join(SpecsPath(root), "recipes")
	entries, _ := os.ReadDir(recipesDir)

	maxSeq := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if len(name) < 3 || name[0] != 'r' || name[1] != '-' {
			continue
		}
		// Extract numeric part after "r-" (only short sequences like 001, not timestamps)
		numEnd := 2
		for numEnd < len(name) && name[numEnd] >= '0' && name[numEnd] <= '9' {
			numEnd++
		}
		numLen := numEnd - 2
		// Only count as sequence number if <= 4 digits (avoids old timestamp format)
		if numLen > 0 && numLen <= 4 && numEnd < len(name) && name[numEnd] == '-' {
			if n, err := strconv.Atoi(name[2:numEnd]); err == nil && n > maxSeq {
				maxSeq = n
			}
		}
	}

	slug := Slugify(topic)
	if slug == "" {
		slug = "recipe"
	}
	return fmt.Sprintf("r-%03d-%s", maxSeq+1, slug)
}

// Slugify converts a topic string to a URL-safe slug.
// Rules: ASCII lowercase + digits + hyphens, max 20 chars, trim at word boundary.
func Slugify(s string) string {
	s = strings.ToLower(s)

	// Keep only ASCII letters, digits, spaces
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else if r == ' ' || r == '-' || r == '_' {
			b.WriteRune(' ')
		}
	}
	s = b.String()

	// Split into words and join with hyphens
	words := strings.Fields(s)
	if len(words) == 0 {
		return ""
	}

	// Build slug, truncate to ~20 chars at word boundary
	result := words[0]
	for i := 1; i < len(words); i++ {
		next := result + "-" + words[i]
		if len(next) > 20 {
			break
		}
		result = next
	}

	return result
}
