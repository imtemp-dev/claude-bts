package engine

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// ValidationError represents one schema violation.
type ValidationError struct {
	File    string `json:"file"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e ValidationError) String() string {
	return fmt.Sprintf("%s: %s — %s", e.File, e.Field, e.Message)
}

// ValidateRecipeDir checks all JSON files in a recipe directory for schema compliance.
func ValidateRecipeDir(recipeDir string) ([]ValidationError, error) {
	var errors []ValidationError

	// 1. recipe.json
	recipePath := filepath.Join(recipeDir, "recipe.json")
	if errs := validateRecipeJSON(recipePath); len(errs) > 0 {
		errors = append(errors, errs...)
	}

	// 2. manifest.json
	manifestPath := filepath.Join(recipeDir, "manifest.json")
	if errs := validateManifestJSON(manifestPath); len(errs) > 0 {
		errors = append(errors, errs...)
	}

	// 3. changelog.jsonl
	changelogPath := filepath.Join(recipeDir, "changelog.jsonl")
	if errs := validateChangelogJSONL(changelogPath); len(errs) > 0 {
		errors = append(errors, errs...)
	}

	// 4. debate state files
	//
	// This used to look for `meta.json` under the recipe. Nothing has
	// ever written that name: `bts debate log` writes `debate.json`, and
	// it writes it under .bts/specs/debates/, not under the recipe. So
	// the adjudicate_every_debate gate was checking a filename that does
	// not exist, at a path the writer does not use, and passed every
	// project by finding nothing. Both locations are walked now, because
	// real projects have both — the CLI writes the project-level tree
	// and /bts-debate writes its round markdown beside the recipe.
	if errs := validateDebates(recipeDir); len(errs) > 0 {
		errors = append(errors, errs...)
	}

	// 5. tasks.json (optional — only exists after /implement)
	tasksPath := filepath.Join(recipeDir, "tasks.json")
	if errs := validateTasksJSON(tasksPath); len(errs) > 0 {
		errors = append(errors, errs...)
	}

	// 6. test-results.json (optional — only exists after /test)
	testResultsPath := filepath.Join(recipeDir, "test-results.json")
	if errs := validateTestResultsJSON(testResultsPath); len(errs) > 0 {
		errors = append(errors, errs...)
	}

	// 7. verification.md — required <bts-findings> block when present.
	verifyDocPath := filepath.Join(recipeDir, "verification.md")
	if errs := validateVerificationMd(verifyDocPath); len(errs) > 0 {
		errors = append(errors, errs...)
	}

	// 7a. Sprint 9 P22 — cross-check verification.md counts against
	// verify-log.jsonl last entry. Runs only when both files exist;
	// migrate-seeded blocks (source: migrated-from-verify-log) are
	// labelled so operators can spot migration artefacts quickly.
	verifyLogPath := filepath.Join(recipeDir, "verify-log.jsonl")
	if _, err := os.Stat(verifyDocPath); err == nil {
		if _, err := os.Stat(verifyLogPath); err == nil {
			errors = append(errors, validateVerificationLogConsistency(verifyDocPath, verifyLogPath)...)
		}
	}

	// 7b. deviation.md — Phase 16 machine-readable schema. Runs only
	// when deviation.md exists so recipes still in blueprint/verify
	// phase are not flagged.
	deviationPath := filepath.Join(recipeDir, "deviation.md")
	if _, err := os.Stat(deviationPath); err == nil {
		for _, issue := range CheckDeviationSchema(deviationPath) {
			errors = append(errors, ValidationError{
				File:    "deviation.md",
				Field:   issue.Category,
				Message: issue.Claim + " — " + issue.Detail,
			})
		}
		// 7c. Phase 12 — every simulation DEVIATION must land in
		// deviation.md with a matching simulate:{id} Driver.
		for _, issue := range CheckSimDeviationConsumption(recipeDir) {
			errors = append(errors, ValidationError{
				File:    "simulations/ → deviation.md",
				Field:   issue.Category,
				Message: issue.Claim + " — " + issue.Detail,
			})
		}
	}

	// 7d. Phase 13 — test ↔ simulate scenario coverage. Runs when
	// test-results.json exists (tests have been executed).
	if _, err := os.Stat(filepath.Join(recipeDir, "test-results.json")); err == nil {
		for _, issue := range CheckTestScenarioCoverage(recipeDir) {
			errors = append(errors, ValidationError{
				File:    "simulations/ → tests",
				Field:   issue.Category,
				Message: issue.Claim + " — " + issue.Detail,
			})
		}
	}

	// 8. simulations/*.md — cross-boundary ratio + illegal-cell coverage
	// (Phase 6.1 and 6.2).
	simsDir := filepath.Join(recipeDir, "simulations")
	if entries, err := os.ReadDir(simsDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			simPath := filepath.Join(simsDir, entry.Name())
			for _, issue := range CheckSimulationScenarios(simPath, DefaultCrossBoundaryRatio) {
				errors = append(errors, ValidationError{File: "simulations/" + entry.Name(), Field: issue.Category, Message: issue.Claim + " — " + issue.Detail})
			}
		}
	}

	// Illegal-cell coverage is per-recipe (compares domain.md to all
	// simulation files together).
	domainPath := filepath.Join(recipeDir, "domain.md")
	if _, err := os.Stat(domainPath); err == nil {
		for _, issue := range CheckIllegalCellCoverage(domainPath, recipeDir) {
			errors = append(errors, ValidationError{File: "domain.md → simulations/", Field: issue.Category, Message: issue.Claim + " — " + issue.Detail})
		}
	}

	return errors, nil
}

// structuredVerifyResultRe matches the canonical "critical=N major=N ..."
// shape emitted by the CLI. Free-form prose (narrative summaries, mixed
// language commentary) does not match and is passed through.
var structuredVerifyResultRe = regexp.MustCompile(`^\s*critical=\d+\s+major=\d+\b`)

// isStructuredVerifyResult returns true when the result string begins with
// the structured count format. This is intentionally strict — prose that
// merely mentions "critical=N" mid-sentence should NOT be treated as the
// split-required form.
func isStructuredVerifyResult(result string) bool {
	return structuredVerifyResultRe.MatchString(result)
}

// FindingsCounts carries the counts parsed from a document's single
// <bts-findings> block. Used by `bts recipe log --from-verification`
// to write verify-log entries atomically from verification.md, so the
// two sources can never drift (the failure class Phase 22 detects).
type FindingsCounts struct {
	Critical        int
	Major           int
	MinorResolvable int
	MinorDeferred   int
	Info            int

	// Findings is the optional per-finding array. When present it feeds
	// the findings ledger (findings.jsonl), which is what gives findings
	// identity across rounds. Absent on verification.md files written
	// before v0.10 and on migrate-seeded blocks — callers degrade to
	// counts-only rather than failing, but lose stagnation detection.
	Findings []state.ReportedFinding
}

// Total returns the number of findings the counts describe. Used to
// cross-check the findings array against the counts.
func (c *FindingsCounts) Total() int {
	return c.Critical + c.Major + c.MinorResolvable + c.MinorDeferred + c.Info
}

// validFindingSeverities is the enum accepted in the findings array.
// It mirrors the count field names so a findings entry maps 1:1 onto
// the count it contributes to.
var validFindingSeverities = map[string]bool{
	"critical": true, "major": true, "minor_resolvable": true,
	"minor_deferred": true, "info": true,
}

// ParseFindingsBlock extracts the single <bts-findings> JSON block.
// Errors on a missing, duplicated, or malformed block, and on any
// absent required count — callers must fail fast rather than log
// drifted numbers.
func ParseFindingsBlock(data []byte) (*FindingsCounts, error) {
	matches := findingsBlockRe.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("no <bts-findings> block found")
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("expected exactly 1 <bts-findings> block, found %d", len(matches))
	}
	var raw struct {
		Critical        *int                    `json:"critical"`
		Major           *int                    `json:"major"`
		MinorResolvable *int                    `json:"minor_resolvable"`
		MinorDeferred   *int                    `json:"minor_deferred"`
		Info            int                     `json:"info"`
		Findings        []state.ReportedFinding `json:"findings"`
	}
	if err := json.Unmarshal(matches[0][1], &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON in findings block: %w", err)
	}
	required := map[string]*int{
		"critical":         raw.Critical,
		"major":            raw.Major,
		"minor_resolvable": raw.MinorResolvable,
		"minor_deferred":   raw.MinorDeferred,
	}
	for _, name := range []string{"critical", "major", "minor_resolvable", "minor_deferred"} {
		if required[name] == nil {
			return nil, fmt.Errorf("findings block missing required count %q", name)
		}
	}
	out := &FindingsCounts{
		Critical:        *raw.Critical,
		Major:           *raw.Major,
		MinorResolvable: *raw.MinorResolvable,
		MinorDeferred:   *raw.MinorDeferred,
		Info:            raw.Info,
		Findings:        raw.Findings,
	}

	// The array is optional, but a present array must be consistent with
	// the counts — a mismatch means the ledger would silently disagree
	// with the gate, which is the exact drift class Phase 22 exists to
	// prevent. Fail fast instead of logging a half-truth.
	if len(out.Findings) > 0 {
		for i, f := range out.Findings {
			if !validFindingSeverities[f.Severity] {
				return nil, fmt.Errorf("findings[%d]: invalid severity %q (want critical|major|minor_resolvable|minor_deferred|info)", i, f.Severity)
			}
			if strings.TrimSpace(f.Title) == "" {
				return nil, fmt.Errorf("findings[%d]: empty title — titles are the identity key for the ledger", i)
			}
		}
		if len(out.Findings) != out.Total() {
			return nil, fmt.Errorf("findings array has %d entries but counts total %d (critical=%d major=%d minor_resolvable=%d minor_deferred=%d info=%d)",
				len(out.Findings), out.Total(), out.Critical, out.Major,
				out.MinorResolvable, out.MinorDeferred, out.Info)
		}
		per := map[string]int{}
		for _, f := range out.Findings {
			per[f.Severity]++
		}
		for name, want := range map[string]int{
			"critical": out.Critical, "major": out.Major,
			"minor_resolvable": out.MinorResolvable,
			"minor_deferred":   out.MinorDeferred, "info": out.Info,
		} {
			if per[name] != want {
				return nil, fmt.Errorf("findings array has %d %s entries but count says %d", per[name], name, want)
			}
		}
	}
	return out, nil
}

// findingsBlockRe matches <bts-findings>...</bts-findings> with JSON inside.
// Multi-line (dot matches newline) and non-greedy body.
var findingsBlockRe = regexp.MustCompile(`(?s)<bts-findings>\s*(\{.*?\})\s*</bts-findings>`)

// decisionBlockRe matches <bts-decision>...</bts-decision> similarly.
var decisionBlockRe = regexp.MustCompile(`(?s)<bts-decision>\s*(\{.*?\})\s*</bts-decision>`)

// validDecisionActions is the authoritative enum for <bts-decision>.action.
// Source: bts-assess/SKILL.md § Part A. Changes require updating both files.
var validDecisionActions = map[string]bool{
	"RESEARCH": true, "DEBATE": true, "ADJUDICATE": true, "SIMULATE": true,
	"AUDIT": true, "IMPROVE": true, "VERIFY": true, "SYNC_CHECK": true,
	"FINALIZE": true, "SCOPE_REOPEN": true, "WIREFRAME": true,
	"DOMAIN_MODEL": true, "ARCHITECT": true,
	"HALT_DECISION_REQUIRED": true, "HALT_CONVERGENCE_FAILED": true,
	"HALT_DEBATE_DEADLOCK": true,
}

// validateVerificationMd enforces that verification.md carries a structured
// findings block. The stop hook and downstream tooling rely on the counts
// embedded in this block.
func validateVerificationMd(path string) []ValidationError {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil // absence is handled by the stop hook, not by validator
	}
	if err != nil {
		return []ValidationError{{File: "verification.md", Field: "(file)", Message: err.Error()}}
	}

	matches := findingsBlockRe.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return []ValidationError{{File: "verification.md", Field: "<bts-findings>", Message: "missing structured findings block — add <bts-findings>{...}</bts-findings> per bts-verify skill spec"}}
	}
	if len(matches) > 1 {
		return []ValidationError{{File: "verification.md", Field: "<bts-findings>", Message: fmt.Sprintf("expected exactly 1 block, found %d", len(matches))}}
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(matches[0][1], &raw); err != nil {
		return []ValidationError{{File: "verification.md", Field: "<bts-findings>", Message: "invalid JSON in findings block: " + err.Error()}}
	}

	var errs []ValidationError
	for _, field := range []string{"critical", "major", "minor_resolvable", "minor_deferred"} {
		v, ok := raw[field]
		if !ok {
			errs = append(errs, ValidationError{File: "verification.md", Field: "<bts-findings>." + field, Message: "missing required count"})
			continue
		}
		// JSON numbers arrive as float64
		if _, isNum := v.(float64); !isNum {
			errs = append(errs, ValidationError{File: "verification.md", Field: "<bts-findings>." + field, Message: "must be a number"})
		}
	}
	return errs
}

// validateVerificationLogConsistency cross-checks the counts embedded
// in verification.md's <bts-findings> block against the last entry of
// verify-log.jsonl. Sprint 9 P22 — r-018 showed the two sources can
// drift when verify-log gets a new entry but verification.md isn't
// re-generated, or when manual log edits occur.
//
// Compares four fields: critical, major, minor_resolvable,
// minor_deferred. Each mismatch surfaces as a `major` ValidationError.
// Runs only when BOTH files exist (checked at the caller); vacuously
// returns no errors when the block is missing (validateVerificationMd
// already flags that) or when last verify entry can't be parsed.
//
// Migrate-seeded blocks (`"source": "migrated-from-verify-log"`) get
// a `[migrate-seeded]` prefix on the detail so operators can tell
// migration artefacts from real drift. Severity stays major — the
// artefact is still a real inconsistency that needs a re-verify.
func validateVerificationLogConsistency(verificationPath, verifyLogPath string) []ValidationError {
	vData, err := os.ReadFile(verificationPath)
	if err != nil {
		return nil
	}
	matches := findingsBlockRe.FindAllSubmatch(vData, -1)
	if len(matches) != 1 {
		// validateVerificationMd already emits the right error; avoid
		// double-reporting and skip if the block is missing/duplicated.
		return nil
	}
	var mdCounts map[string]interface{}
	if err := json.Unmarshal(matches[0][1], &mdCounts); err != nil {
		return nil // validateVerificationMd flags malformed JSON
	}

	lastEntry, err := readLastVerifyEntryFile(verifyLogPath)
	if err != nil {
		// No entries (empty log) or unreadable — treat the cross-check
		// as vacuous. Other validators cover log-level issues.
		return nil
	}

	migrateSeeded := false
	if src, ok := mdCounts["source"].(string); ok {
		if strings.Contains(src, "migrated-from-verify-log") {
			migrateSeeded = true
		}
	}

	type check struct {
		key      string
		expected int
	}
	checks := []check{
		{"critical", lastEntry.Critical},
		{"major", lastEntry.Major},
		// Pre-split verify-log entries only have `minor`; map it to
		// resolvable via the same conservative fallback the stop hook
		// uses (stop.go EffectiveResolvable).
		{"minor_resolvable", lastEntry.effectiveResolvable()},
		{"minor_deferred", lastEntry.MinorDeferred},
	}

	var errs []ValidationError
	for _, c := range checks {
		raw, ok := mdCounts[c.key]
		if !ok {
			continue // missing keys are validateVerificationMd's responsibility
		}
		num, isNum := raw.(float64)
		if !isNum {
			continue
		}
		if int(num) == c.expected {
			continue
		}
		prefix := ""
		if migrateSeeded {
			prefix = "[migrate-seeded] "
		}
		errs = append(errs, ValidationError{
			File:  "verification.md ↔ verify-log.jsonl",
			Field: "verification_log_mismatch." + c.key,
			Message: fmt.Sprintf(
				"%sverification.md says %s=%d but verify-log.jsonl last entry (iteration=%d) says %s=%d — re-run /bts-verify on draft.md to refresh.",
				prefix, c.key, int(num), lastEntry.Iteration, c.key, c.expected,
			),
		})
	}
	return errs
}

// verifyLogLite is an engine-internal subset of state.VerifyLogEntry,
// defined here so the validator can read verify-log.jsonl without
// importing state (which would create an import cycle).
type verifyLogLite struct {
	Iteration       int `json:"iteration"`
	Critical        int `json:"critical"`
	Major           int `json:"major"`
	Minor           int `json:"minor,omitempty"`
	MinorResolvable int `json:"minor_resolvable,omitempty"`
	MinorDeferred   int `json:"minor_deferred,omitempty"`
}

// effectiveResolvable mirrors state.VerifyLogEntry.EffectiveResolvable()
// so pre-Phase-2.4 legacy entries (only `minor` set) still map to
// resolvable for the cross-check.
func (v *verifyLogLite) effectiveResolvable() int {
	if v.MinorResolvable == 0 && v.MinorDeferred == 0 && v.Minor > 0 {
		return v.Minor
	}
	return v.MinorResolvable
}

// readLastVerifyEntryFile scans verify-log.jsonl, returning the most
// recent parseable entry. Used by the cross-check in this file; the
// hook package has its own copy taking a different signature.
func readLastVerifyEntryFile(path string) (*verifyLogLite, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var last verifyLogLite
	found := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var entry verifyLogLite
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		last = entry
		found = true
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("empty verify-log")
	}
	return &last, nil
}

// ValidateAssessDecisionBlock parses and validates a <bts-decision> block.
// Returns the parsed action or an error if the block is malformed. Callers
// that embed decisions in changelog or external outputs can use this to
// verify conformance before writing.
func ValidateAssessDecisionBlock(content string) (string, []ValidationError) {
	matches := decisionBlockRe.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return "", []ValidationError{{File: "(assess output)", Field: "<bts-decision>", Message: "missing decision block"}}
	}
	if len(matches) > 1 {
		return "", []ValidationError{{File: "(assess output)", Field: "<bts-decision>", Message: fmt.Sprintf("expected 1 block, found %d", len(matches))}}
	}

	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(matches[0][1]), &raw); err != nil {
		return "", []ValidationError{{File: "(assess output)", Field: "<bts-decision>", Message: "invalid JSON: " + err.Error()}}
	}

	var errs []ValidationError
	for _, field := range []string{"level", "action", "phase", "reason"} {
		if _, ok := raw[field]; !ok {
			errs = append(errs, ValidationError{File: "(assess output)", Field: "<bts-decision>." + field, Message: "missing required field"})
		}
	}
	action, _ := raw["action"].(string)
	if action != "" && !validDecisionActions[action] {
		errs = append(errs, ValidationError{File: "(assess output)", Field: "<bts-decision>.action", Message: fmt.Sprintf("invalid action '%s' — not in enum", action)})
	}
	return action, errs
}

func validateRecipeJSON(path string) []ValidationError {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []ValidationError{{File: "recipe.json", Field: "(file)", Message: "missing — create recipe.json at recipe start"}}
	}
	if err != nil {
		return []ValidationError{{File: "recipe.json", Field: "(file)", Message: err.Error()}}
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return []ValidationError{{File: "recipe.json", Field: "(parse)", Message: "invalid JSON: " + err.Error()}}
	}

	var errs []ValidationError
	for _, field := range []string{"id", "type", "topic", "phase", "started_at", "updated_at"} {
		if _, ok := raw[field]; !ok {
			errs = append(errs, ValidationError{File: "recipe.json", Field: field, Message: "missing required field"})
		}
	}
	for _, field := range []string{"iteration", "level"} {
		if _, ok := raw[field]; !ok {
			errs = append(errs, ValidationError{File: "recipe.json", Field: field, Message: "missing required field"})
		}
	}

	// Validate type enum
	if t, ok := raw["type"].(string); ok {
		valid := map[string]bool{"analyze": true, "design": true, "blueprint": true, "fix": true, "debug": true}
		if !valid[t] {
			errs = append(errs, ValidationError{File: "recipe.json", Field: "type", Message: fmt.Sprintf("invalid value '%s', must be analyze/design/blueprint", t)})
		}
	}

	// Validate phase enum
	if p, ok := raw["phase"].(string); ok {
		valid := map[string]bool{
			"discovery": true, "scoping": true,
			"domain-model": true, "architect": true, "wireframe": true,
			"research": true, "draft": true, "assess": true, "improve": true,
			"verify": true, "debate": true, "simulate": true, "audit": true,
			"finalize": true, "cancelled": true,
			"implement": true, "test": true, "review": true, "sync": true, "status": true,
			"complete": true,
		}
		if !valid[p] {
			errs = append(errs, ValidationError{File: "recipe.json", Field: "phase", Message: fmt.Sprintf("invalid value '%s', must be a valid phase", p)})
		}
	}

	return errs
}

func validateManifestJSON(path string) []ValidationError {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []ValidationError{{File: "manifest.json", Field: "(file)", Message: "missing — create manifest.json to track documents"}}
	}
	if err != nil {
		return []ValidationError{{File: "manifest.json", Field: "(file)", Message: err.Error()}}
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return []ValidationError{{File: "manifest.json", Field: "(parse)", Message: "invalid JSON: " + err.Error()}}
	}

	var errs []ValidationError

	// Required fields
	if _, ok := raw["current_draft"]; !ok {
		errs = append(errs, ValidationError{File: "manifest.json", Field: "current_draft", Message: "missing required field"})
	}
	if _, ok := raw["level"]; !ok {
		errs = append(errs, ValidationError{File: "manifest.json", Field: "level", Message: "missing required field"})
	}

	// architect_decision is optional, but if present must be a string.
	// Empty string is not accepted — set it to omit via absence.
	if v, ok := raw["architect_decision"]; ok {
		if _, isString := v.(string); !isString {
			errs = append(errs, ValidationError{File: "manifest.json", Field: "architect_decision", Message: "must be a string naming the selected decomposition"})
		}
	}

	// documents must be a map of path → DocumentEntry (not categorized lists)
	if docs, ok := raw["documents"]; ok {
		docsMap, isMap := docs.(map[string]interface{})
		if !isMap {
			errs = append(errs, ValidationError{File: "manifest.json", Field: "documents", Message: "must be an object with file paths as keys, not categorized lists"})
		} else {
			for path, entry := range docsMap {
				entryMap, isObj := entry.(map[string]interface{})
				if !isObj {
					errs = append(errs, ValidationError{File: "manifest.json", Field: "documents." + path, Message: "must be a DocumentEntry object with 'type' and 'created_at'"})
					continue
				}
				if t, ok := entryMap["type"].(string); ok {
					validTypes := map[string]bool{
						"research": true, "draft": true, "debate": true,
						"simulation": true, "verification": true,
						"implementation": true, "test-result": true, "deviation": true,
						"review": true,
						// Lifecycle docs (intent/scope/final) carry their own doc type so
						// manifests can distinguish them from drafts. "finalize" is an
						// accepted alias kept for recipes created before the rename.
						"discover": true, "scope": true,
						"final": true, "finalize": true,
						"domain": true, "wireframe": true, "architect-decision": true,
					}
					if !validTypes[t] {
						errs = append(errs, ValidationError{File: "manifest.json", Field: "documents." + path + ".type", Message: fmt.Sprintf("invalid type '%s'", t)})
					}
				} else if _, exists := entryMap["type"]; !exists {
					errs = append(errs, ValidationError{File: "manifest.json", Field: "documents." + path + ".type", Message: "missing required field"})
				}
				if _, ok := entryMap["created_at"]; !ok {
					errs = append(errs, ValidationError{File: "manifest.json", Field: "documents." + path + ".created_at", Message: "missing required field"})
				}
			}
		}
	} else {
		errs = append(errs, ValidationError{File: "manifest.json", Field: "documents", Message: "missing required field"})
	}

	return errs
}

func validateChangelogJSONL(path string) []ValidationError {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil // changelog is optional early on
	}
	if err != nil {
		return []ValidationError{{File: "changelog.jsonl", Field: "(file)", Message: err.Error()}}
	}
	defer f.Close()

	var errs []ValidationError
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			errs = append(errs, ValidationError{File: "changelog.jsonl", Field: fmt.Sprintf("line %d", lineNum), Message: "invalid JSON"})
			continue
		}

		// Key must be "time", not "timestamp"
		if _, ok := raw["time"]; !ok {
			if _, hasTimestamp := raw["timestamp"]; hasTimestamp {
				errs = append(errs, ValidationError{File: "changelog.jsonl", Field: fmt.Sprintf("line %d", lineNum), Message: "use 'time' not 'timestamp' as key name"})
			} else {
				errs = append(errs, ValidationError{File: "changelog.jsonl", Field: fmt.Sprintf("line %d.time", lineNum), Message: "missing required field"})
			}
		}

		if action, ok := raw["action"].(string); ok {
			validActions := map[string]bool{
				"discover": true, "domain-model": true, "wireframe": true,
				"research": true, "draft": true, "improve": true, "verify": true,
				"debate": true, "simulate": true, "audit": true, "assess": true,
				"sync-check": true, "finalize": true,
				"implement": true, "test": true, "sync": true, "status": true,
				"adjudicate": true, "review": true, "architect": true,
				"resolve-uncertainties": true,
				"midrun-review":         true,
				"comment-apply":         true,
			}
			if !validActions[action] {
				errs = append(errs, ValidationError{File: "changelog.jsonl", Field: fmt.Sprintf("line %d.action", lineNum), Message: fmt.Sprintf("invalid action '%s'", action)})
			}

			// verify action result must carry split-minor counts when written
			// in the structured "critical=X major=Y ..." form. Free-form prose
			// results from older recipes (narrative summaries, multi-line
			// commentary) pass through — the structured counts live in
			// verify-log.jsonl, which is the authoritative gate input.
			if action == "verify" {
				if result, ok := raw["result"].(string); ok && result != "" {
					if isStructuredVerifyResult(result) &&
						!strings.Contains(result, "minor_resolvable=") &&
						!strings.Contains(result, "minor_deferred=") {
						errs = append(errs, ValidationError{File: "changelog.jsonl", Field: fmt.Sprintf("line %d.result", lineNum), Message: "structured verify result missing minor_resolvable=/minor_deferred= — use new split format"})
					}
				}
			}
		} else if _, exists := raw["action"]; !exists {
			errs = append(errs, ValidationError{File: "changelog.jsonl", Field: fmt.Sprintf("line %d.action", lineNum), Message: "missing required field"})
		}
	}

	return errs
}

func validateDebateMetaJSON(path string) []ValidationError {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return []ValidationError{{File: path, Field: "(file)", Message: err.Error()}}
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return []ValidationError{{File: path, Field: "(parse)", Message: "invalid JSON: " + err.Error()}}
	}

	fileName := filepath.Base(filepath.Dir(path)) + "/" + filepath.Base(path)
	var errs []ValidationError

	for _, field := range []string{"id", "topic"} {
		if _, ok := raw[field]; !ok {
			errs = append(errs, ValidationError{File: fileName, Field: field, Message: "missing required field"})
		}
	}
	if _, ok := raw["rounds"]; !ok {
		errs = append(errs, ValidationError{File: fileName, Field: "rounds", Message: "missing required field"})
	}
	if _, ok := raw["decided"]; !ok {
		errs = append(errs, ValidationError{File: fileName, Field: "decided", Message: "missing required field — use boolean, not 'status'"})
	}

	// conclusion must be string, not object
	if conclusion, ok := raw["conclusion"]; ok {
		if _, isString := conclusion.(string); !isString {
			errs = append(errs, ValidationError{File: fileName, Field: "conclusion", Message: "must be a string, not an object. Write structured conclusions as a single sentence."})
		}
	}

	for _, field := range []string{"started_at", "updated_at"} {
		if _, ok := raw[field]; !ok {
			errs = append(errs, ValidationError{File: fileName, Field: field, Message: "missing required field"})
		}
	}

	return errs
}

// debateDirs returns every directory that may hold debate state for a
// recipe: the project-level tree `bts debate` writes, and the
// recipe-level tree the manifest references and /bts-debate writes into.
func debateDirs(recipeDir string) []string {
	dirs := []string{filepath.Join(recipeDir, "debates")}
	// recipeDir is .bts/specs/recipes/{id}; the project tree is its
	// grandparent's "debates".
	if specs := filepath.Dir(filepath.Dir(recipeDir)); filepath.Base(specs) == "specs" {
		dirs = append(dirs, filepath.Join(specs, "debates"))
	}
	return dirs
}

// validateDebates validates each debate ONCE, across both trees.
//
// The two trees hold different halves of the same debate: `bts debate
// log` writes machine state to .bts/specs/debates/{id}/debate.json,
// while /bts-debate writes its round markdown beside the recipe. Neither
// half is wrong, so the unit of validation is the debate ID, not the
// directory — checking directories independently reports the normal
// arrangement (rounds here, state there) as a missing state file.
//
// Scope comes from the RECIPE tree. The project tree is shared, so
// taking IDs from it made every recipe answer for every debate in the
// project.
func validateDebates(recipeDir string) []ValidationError {
	dirs := debateDirs(recipeDir)
	if len(dirs) == 0 {
		return nil
	}
	recipeTree, otherTrees := dirs[0], dirs[1:]

	// Which debates are THIS recipe's: the ones under its own tree.
	// Nothing links a project-tree debate back to a recipe — DebateState
	// records no recipe id — so scanning that tree for IDs made every
	// recipe answer for every debate in the project, and `bts validate
	// r-002` reported a missing state file belonging to r-001.
	ids := map[string][]string{}
	entries, err := os.ReadDir(recipeTree)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if entry.IsDir() {
			ids[entry.Name()] = []string{filepath.Join(recipeTree, entry.Name())}
		}
	}
	// The project tree holds the other half of those same debates.
	for _, dir := range otherTrees {
		for id := range ids {
			path := filepath.Join(dir, id)
			if st, serr := os.Stat(path); serr == nil && st.IsDir() {
				ids[id] = append(ids[id], path)
			}
		}
	}

	names := make([]string, 0, len(ids))
	for id := range ids {
		names = append(names, id)
	}
	sort.Strings(names)

	var errs []ValidationError
	for _, id := range names {
		if statePath := findDebateState(ids[id]); statePath != "" {
			errs = append(errs, validateDebateMetaJSON(statePath)...)
			continue
		}
		if !anyDebateContent(ids[id]) {
			continue // empty directory; nothing was recorded here at all
		}
		errs = append(errs, ValidationError{
			File:    id,
			Field:   "debate.json",
			Message: "debate has rounds but no state file in any debate tree — `bts debate log` records the conclusion and whether it was decided",
		})
	}
	return errs
}

// findDebateState returns the first state file found across a debate's
// directories, accepting the name the CLI writes (debate.json) and the
// legacy one this validator used to look for (meta.json).
func findDebateState(dirs []string) string {
	for _, dir := range dirs {
		for _, name := range []string{"debate.json", "meta.json"} {
			path := filepath.Join(dir, name)
			if _, err := os.Stat(path); err == nil {
				return path
			}
		}
	}
	return ""
}

// anyDebateContent reports whether any of a debate's directories holds a file.
func anyDebateContent(dirs []string) bool {
	for _, dir := range dirs {
		if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
			return true
		}
	}
	return false
}

func validateTasksJSON(path string) []ValidationError {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil // tasks.json is optional — only exists after /implement
	}
	if err != nil {
		return []ValidationError{{File: "tasks.json", Field: "(file)", Message: err.Error()}}
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return []ValidationError{{File: "tasks.json", Field: "(parse)", Message: "invalid JSON: " + err.Error()}}
	}

	var errs []ValidationError

	for _, field := range []string{"recipe_id", "started_at", "updated_at"} {
		if _, ok := raw[field]; !ok {
			errs = append(errs, ValidationError{File: "tasks.json", Field: field, Message: "missing required field"})
		}
	}

	tasks, ok := raw["tasks"]
	if !ok {
		errs = append(errs, ValidationError{File: "tasks.json", Field: "tasks", Message: "missing required field"})
		return errs
	}

	tasksList, isList := tasks.([]interface{})
	if !isList {
		errs = append(errs, ValidationError{File: "tasks.json", Field: "tasks", Message: "must be an array"})
		return errs
	}

	validStatuses := map[string]bool{
		"pending": true, "in_progress": true, "done": true, "blocked": true, "skipped": true,
	}
	validActions := map[string]bool{"create": true, "modify": true, "delete": true}

	for i, task := range tasksList {
		taskMap, isObj := task.(map[string]interface{})
		if !isObj {
			errs = append(errs, ValidationError{File: "tasks.json", Field: fmt.Sprintf("tasks[%d]", i), Message: "must be an object"})
			continue
		}

		for _, field := range []string{"id", "file", "action", "status", "description"} {
			if _, ok := taskMap[field]; !ok {
				errs = append(errs, ValidationError{File: "tasks.json", Field: fmt.Sprintf("tasks[%d].%s", i, field), Message: "missing required field"})
			}
		}

		if action, ok := taskMap["action"].(string); ok {
			if !validActions[action] {
				errs = append(errs, ValidationError{File: "tasks.json", Field: fmt.Sprintf("tasks[%d].action", i), Message: fmt.Sprintf("invalid value '%s', must be create/modify/delete", action)})
			}
		}

		if status, ok := taskMap["status"].(string); ok {
			if !validStatuses[status] {
				errs = append(errs, ValidationError{File: "tasks.json", Field: fmt.Sprintf("tasks[%d].status", i), Message: fmt.Sprintf("invalid value '%s', must be pending/in_progress/done/blocked/skipped", status)})
			}
		}
	}

	// Phase 9: anchor contract — tasks.json ↔ final.md 1:1.
	// Only runs when tasks.json lives inside a recipe directory that
	// also has final.md. Missing final.md is silently skipped so that
	// loose test fixtures (e.g. standalone tasks.json) validate.
	finalPath := filepath.Join(filepath.Dir(path), "final.md")
	if _, err := os.Stat(finalPath); err == nil {
		for _, issue := range CheckTaskAnchors(finalPath, path) {
			errs = append(errs, ValidationError{
				File:    "tasks.json → final.md",
				Field:   issue.Category,
				Message: issue.Claim + " — " + issue.Detail,
			})
		}

		// Phase 14: modify scope. Runs after anchor check so missing
		// anchors are reported there first; modify scope adds the
		// scope=/ModifyScope consistency layer. projectRoot is passed
		// as "" to skip the scope_symbol_missing filesystem check in
		// static validation — the stop hook wires the real root later.
		for _, issue := range CheckModifyScope(finalPath, path, "") {
			errs = append(errs, ValidationError{
				File:    "tasks.json → final.md",
				Field:   issue.Category,
				Message: issue.Claim + " — " + issue.Detail,
			})
		}
	}

	return errs
}

func validateTestResultsJSON(path string) []ValidationError {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil // test-results.json is optional — only exists after /test
	}
	if err != nil {
		return []ValidationError{{File: "test-results.json", Field: "(file)", Message: err.Error()}}
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return []ValidationError{{File: "test-results.json", Field: "(parse)", Message: "invalid JSON: " + err.Error()}}
	}

	var errs []ValidationError

	for _, field := range []string{"recipe_id", "run_at", "framework"} {
		if _, ok := raw[field]; !ok {
			errs = append(errs, ValidationError{File: "test-results.json", Field: field, Message: "missing required field"})
		}
	}

	for _, field := range []string{"iterations", "total", "passed", "failed", "skipped"} {
		if _, ok := raw[field]; !ok {
			errs = append(errs, ValidationError{File: "test-results.json", Field: field, Message: "missing required field"})
		}
	}

	if status, ok := raw["status"].(string); ok {
		if status != "pass" && status != "fail" {
			errs = append(errs, ValidationError{File: "test-results.json", Field: "status", Message: fmt.Sprintf("invalid value '%s', must be pass/fail", status)})
		}
	} else if _, exists := raw["status"]; !exists {
		errs = append(errs, ValidationError{File: "test-results.json", Field: "status", Message: "missing required field"})
	}

	return errs
}
