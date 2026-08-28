package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Issue represents a finding from consistency checking.
type Issue struct {
	Category string `json:"category"` // consistency, level, file_ref, symbol_ref
	Claim    string `json:"claim"`
	Severity string `json:"severity"` // critical, major, minor, info
	Detail   string `json:"detail"`
}

// LevelScore represents the assessed document level.
type LevelScore struct {
	Level     float64         `json:"level"`     // 0.0 ~ 3.0
	Checklist map[string]bool `json:"checklist"` // each criterion: met or not
	Missing   []string        `json:"missing"`   // what's needed for next level
}

// VerifyResult contains all verification results.
type VerifyResult struct {
	File    string     `json:"file"`
	Issues  []Issue    `json:"issues"`
	Level   LevelScore `json:"level"`
	Summary Summary    `json:"summary"`
}

// Summary counts issues by severity.
type Summary struct {
	Critical int `json:"critical"`
	Major    int `json:"major"`
	Minor    int `json:"minor"`
	Info     int `json:"info"`
	Checked  int `json:"checked"`
}

// Level criteria checklists
var level1Criteria = []string{
	"components_listed",       // 주요 컴포넌트 나열
	"relationships_described", // 컴포넌트 간 관계 설명
	"tech_stack_specified",    // 기술 스택 명시
}

var level2Criteria = []string{
	"data_flow_defined",      // 데이터 흐름 명시 (입력→처리→출력)
	"error_strategy_defined", // 에러 처리 전략
	"interfaces_described",   // 주요 인터페이스 설명
	"tech_choices_rationale", // 기술 선택 근거
}

// Level 3 is structural, not lexical.
//
// The old checklist scored by keyword presence: one code fence met
// "scaffolding_included", the substring "()" met "function_signatures",
// the word "test" met "test_scenarios". Every criterion was reachable by
// writing more text and by nothing else — and `bts verify` hands the
// unmet ones to /bts-assess, which turns each into an IMPROVE
// instruction. A 250-line skeleton therefore scored BELOW a 2,000-line
// transcription of the same design, and the loop was pointed at length.
//
// Measured consequence: one recipe's draft reached 2,184 lines and 17
// verify rounds against a budget of 3, and 46.5% of its findings arrived
// after round 4 — the round by which every finding that named the
// design's direction had already been raised. See
// docs/bts-flow-metrics.md indicators 15-19.
//
// These criteria ask for structure instead, and every threshold is
// BOUNDED: once three files are named, or every invariant has an owner,
// writing more cannot raise the score. That is the whole point. This
// file stops rewarding length; section_span_checker.go is what makes
// length cost something.
//
// What a Level 3 document owes its reader is the part code cannot cheaply
// falsify — what is always true and who keeps it true, what shape crosses
// a boundary, what cannot be undone, and what is still unknown. Function
// signatures, type definitions and scaffolding are not on the list
// because a compiler produces them for free and settles them faster than
// a verify round can argue about them.
var level3Criteria = []string{
	"file_paths_specified",   // the units this spec touches are named
	"invariants_owned",       // every invariant names the file that keeps it
	"boundary_contracts",     // what crosses a boundary has a declared shape
	"irreversible_order",     // ordered steps, and what undoes them
	"falsifiers_assigned",    // every invariant names what would prove it false
	"uncertainties_declared", // what is not yet known, and what would settle it
}

// Level predicates. Every criterion is judged on the document's shape.
//
// See level3_structural.go for why none of these is a keyword count: a
// threshold over an unbounded text is cleared by length, and the level
// score is what /bts-assess turns into IMPROVE instructions.
var structuralCriteria = map[string]func(string) bool{
	// Level 1 — is this an understanding of a system?
	// These have a canonical home upstream (wireframe.md, scope.md), so
	// naming it counts. See "Delegation" in level3_structural.go.
	"components_listed":       orDelegated(hasNamedComponents),
	"relationships_described": orDelegated(hasRelationships),
	"tech_stack_specified":    orDelegated(hasTechStack),

	// Level 2 — is this a design?
	// The flow and the recorded decision live in wireframe.md; error
	// disposition and the boundary shapes are the document's own.
	"data_flow_defined":      orDelegated(hasDataFlow),
	"error_strategy_defined": hasErrorStrategy,
	"interfaces_described":   hasInterfaces,
	"tech_choices_rationale": orDelegated(hasTechRationale),

	// Level 3 — is this a blueprint? See level3Criteria.
	"file_paths_specified":   hasNamedUnits,
	"invariants_owned":       func(c string) bool { return invariantsCarry(c, lineNamesOwner) },
	"falsifiers_assigned":    func(c string) bool { return invariantsCarry(c, lineNamesFalsifier) },
	"boundary_contracts":     hasBoundaryContract,
	"irreversible_order":     hasIrreversibleOrder,
	"uncertainties_declared": hasDeclaredUncertainties,
}

// VerifyDocument checks a document for internal consistency and assesses its level.
// checkCode selects whether references into the codebase are resolved.
// It is separate from projectRoot because the two questions are separate:
// a from-scratch spec has no code to check against, but it still lives in
// a bts project whose settings apply. Folding them together — passing ""
// as the root to mean "skip code checks" — turned --no-code into a
// blanket "skip everything that needs the project", and the section-span
// check went with it, on exactly the from-scratch documents where span
// discipline matters most.
func VerifyDocument(docPath string, projectRoot string, checkCode bool) (*VerifyResult, error) {
	data, err := os.ReadFile(docPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", docPath, err)
	}

	content := string(data)
	result := &VerifyResult{File: docPath}

	// 1. Assess document level
	result.Level = assessLevel(content)

	// 2. Check internal consistency
	checkInternalConsistency(content, result)

	// 3. If project has code, optionally check file/symbol references
	if projectRoot != "" && checkCode {
		checkCodeReferences(content, projectRoot, result)
	}

	// 4. Domain-specific structural checks.
	// domain.md: every invariant must have exactly one owner (Phase 4.4).
	base := filepath.Base(docPath)
	if strings.EqualFold(base, "domain.md") {
		appendIssues(result, CheckInvariantOwnership(docPath))
	}

	// 5. Wireframe anchor matching (Phase 3.3) + YAGNI gate (Phase 5.4).
	// draft.md: every wireframe path-id must have a corresponding
	// `<!-- path: wireframe.md#id -->` anchor, and every draft anchor
	// must resolve to a wireframe id. Enforced 1:1.
	// Separately, single-implementation interfaces without
	// `// YAGNI-justified:` raise major.
	if strings.EqualFold(base, "draft.md") {
		appendIssues(result, WireframeAnchorsForDraft(docPath))
		appendIssues(result, CheckInterfaceJustification(docPath))
	}

	// 5a. Falsifier coverage (draft.md / final.md). Every invariant the
	// spec declares must name what would prove it false. Runs on final.md
	// too because that is the document /bts-implement reads.
	if strings.EqualFold(base, "draft.md") || strings.EqualFold(base, "final.md") {
		appendIssues(result, CheckFalsifierCoverage(docPath))
	}

	// 5b. Section span (draft.md / final.md). Findings scale with
	// section length at r=+0.95, so length is the one lever on the
	// loop's cost that is knowable before the loop runs. Settings are
	// read from the project the document belongs to; without a project
	// root there is nothing to read, so the check stands down.
	if strings.EqualFold(base, "draft.md") || strings.EqualFold(base, "final.md") {
		if projectRoot != "" {
			if st, serr := LoadSettings(projectRoot); serr == nil {
				appendIssues(result, CheckSectionSpan(docPath,
					st.Verify.MaxSectionLines, st.Verify.SectionSpanSeverity))
				appendIssues(result, CheckDocumentSpan(docPath,
					st.Verify.MaxDocumentLines, st.Verify.SectionSpanSeverity))
			}
		}
	}

	// 6. wireframe.md: responsibility line conjunction check (Phase 5.1).
	// Each node's responsibility must be a single-job sentence — "and",
	// "&", "및" signal two jobs that should split into two nodes.
	// Also check architect-decision header presence (Phase 5.3).
	if strings.EqualFold(base, "wireframe.md") {
		appendIssues(result, CheckWireframeResponsibilities(docPath))
		appendIssues(result, CheckArchitectDecisionHeader(docPath))
		appendIssues(result, CheckArchitectInvariantCoverage(docPath))
	}

	return result, nil
}

// appendIssues merges issues into the VerifyResult, incrementing the
// summary counters so the CLI exit code (which gates on critical/major)
// reflects all checker outputs, not just consistency.
func appendIssues(result *VerifyResult, issues []Issue) {
	for _, issue := range issues {
		result.Issues = append(result.Issues, issue)
		switch issue.Severity {
		case "critical":
			result.Summary.Critical++
		case "major":
			result.Summary.Major++
		case "minor":
			result.Summary.Minor++
		case "info":
			result.Summary.Info++
		}
		result.Summary.Checked++
	}
}

// assessLevel evaluates the document against the level criteria.
//
// Levels 1 and 2 are lexical: they ask whether the document reads like an
// understanding or a design, and keyword presence is a fair proxy for
// that. Level 3 is structural (see level3Criteria), so it needs the
// original text — identifiers, paths and table rows do not survive
// lower-casing intact.
func assessLevel(content string) LevelScore {
	checklist := make(map[string]bool)
	var missing []string

	// Check Level 1 criteria
	l1Met := 0
	for _, c := range level1Criteria {
		met := checkCriterion(content, c)
		checklist[c] = met
		if met {
			l1Met++
		} else {
			missing = append(missing, fmt.Sprintf("[L1] %s", c))
		}
	}

	// Check Level 2 criteria
	l2Met := 0
	for _, c := range level2Criteria {
		met := checkCriterion(content, c)
		checklist[c] = met
		if met {
			l2Met++
		} else {
			missing = append(missing, fmt.Sprintf("[L2] %s", c))
		}
	}

	// Check Level 3 criteria
	l3Met := 0
	for _, c := range level3Criteria {
		met := checkCriterion(content, c)
		checklist[c] = met
		if met {
			l3Met++
		} else {
			missing = append(missing, fmt.Sprintf("[L3] %s", c))
		}
	}

	// Calculate level as weighted score
	l1Score := float64(l1Met) / float64(len(level1Criteria))
	l2Score := float64(l2Met) / float64(len(level2Criteria))
	l3Score := float64(l3Met) / float64(len(level3Criteria))

	level := l1Score
	if l1Score >= 0.7 {
		level = 1.0 + l2Score
	}
	if l1Score >= 0.7 && l2Score >= 0.7 {
		level = 2.0 + l3Score
	}

	return LevelScore{
		Level:     level,
		Checklist: checklist,
		Missing:   missing,
	}
}

// checkCriterion reports whether the document satisfies one criterion.
// An unknown criterion is not met: with no predicate there is nothing to
// judge it by, and answering "met" would drop it from the Missing list
// without anyone having addressed it.
func checkCriterion(content string, criterion string) bool {
	predicate, ok := structuralCriteria[criterion]
	if !ok {
		return false
	}
	return predicate(content)
}

// checkInternalConsistency finds contradictions within the document.
func checkInternalConsistency(content string, result *VerifyResult) {
	lines := strings.Split(content, "\n")

	// Check for term inconsistency: same concept called different names
	terms := extractDefinedTerms(content)
	for _, conflict := range findTermConflicts(terms) {
		result.Issues = append(result.Issues, Issue{
			Category: "consistency",
			Claim:    conflict,
			Severity: SeverityMajor,
			Detail:   "Same concept appears to be called different names",
		})
		result.Summary.Major++
		result.Summary.Checked++
	}

	// Check for duplicated sections
	headers := extractHeaders(lines)
	for _, dup := range findDuplicateHeaders(headers) {
		result.Issues = append(result.Issues, Issue{
			Category: "consistency",
			Claim:    dup,
			Severity: SeverityMinor,
			Detail:   "Duplicate section header",
		})
		result.Summary.Minor++
		result.Summary.Checked++
	}
}

// checkCodeReferences verifies file/symbol references against actual code.
// Only runs when projectRoot has code files.
func checkCodeReferences(content string, projectRoot string, result *VerifyResult) {
	filePaths := extractFilePaths(content)
	for _, fp := range filePaths {
		result.Summary.Checked++
		absPath := filepath.Join(projectRoot, fp)
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			// Check if this is a "create" reference (new file to be made)
			if isCreateReference(content, fp) {
				result.Issues = append(result.Issues, Issue{
					Category: "file_ref",
					Claim:    fp,
					Severity: SeverityInfo,
					Detail:   "File to be created (not yet existing)",
				})
				result.Summary.Info++
			} else {
				result.Issues = append(result.Issues, Issue{
					Category: "file_ref",
					Claim:    fp,
					Severity: SeverityCritical,
					Detail:   fmt.Sprintf("Referenced file does not exist: %s", absPath),
				})
				result.Summary.Critical++
			}
		}
	}

	// Symbol references
	symbols := extractSymbolRefs(content)
	for _, sym := range symbols {
		if sym.File == "" {
			continue
		}
		result.Summary.Checked++
		absFile := filepath.Join(projectRoot, sym.File)
		if _, err := os.Stat(absFile); os.IsNotExist(err) {
			continue
		}
		if !grepSymbol(absFile, sym.Name) {
			result.Issues = append(result.Issues, Issue{
				Category: "symbol_ref",
				Claim:    fmt.Sprintf("%s in %s", sym.Name, sym.File),
				Severity: SeverityCritical,
				Detail:   fmt.Sprintf("Symbol '%s' not found in %s", sym.Name, sym.File),
			})
			result.Summary.Critical++
		}
	}
}

// isCreateReference checks if a file path is mentioned as "to be created" rather than existing.
func isCreateReference(content, filePath string) bool {
	createPatterns := []string{
		filePath + "를 생성",
		filePath + " 생성",
		"create " + filePath,
		"Create " + filePath,
		"생성:" + filePath,
		filePath + " (create)",
		filePath + " (new)",
		filePath + " action=\"create\"",
	}
	for _, p := range createPatterns {
		if strings.Contains(content, p) {
			return true
		}
	}
	return false
}

// --- Helper functions ---

var filePathRe = regexp.MustCompile(`(?:` + "`" + `)?([a-zA-Z0-9_][a-zA-Z0-9_./-]*\.[a-zA-Z0-9]{1,10})(?:` + "`" + `)?`)

type symbolRef struct {
	Name string
	File string
}

func extractFilePaths(content string) []string {
	matches := filePathRe.FindAllStringSubmatch(content, -1)
	seen := make(map[string]bool)
	var paths []string
	for _, m := range matches {
		fp := m[1]
		if !strings.Contains(fp, "/") {
			continue
		}
		if strings.HasPrefix(fp, "http") {
			continue
		}
		if strings.Contains(fp, ".com/") || strings.Contains(fp, ".io/") || strings.Contains(fp, ".in/") || strings.Contains(fp, ".org/") {
			continue
		}
		if !seen[fp] {
			seen[fp] = true
			paths = append(paths, fp)
		}
	}
	return paths
}

func extractSymbolRefs(content string) []symbolRef {
	colonRe := regexp.MustCompile("`([a-zA-Z0-9_./-]+\\.[a-zA-Z]+):([a-zA-Z_][a-zA-Z0-9_]*)`")
	var refs []symbolRef
	for _, line := range strings.Split(content, "\n") {
		for _, m := range colonRe.FindAllStringSubmatch(line, -1) {
			refs = append(refs, symbolRef{File: m[1], Name: m[2]})
		}
	}
	return refs
}

func extractDefinedTerms(content string) map[string][]int {
	// Simple: find **bold** terms and track line numbers
	boldRe := regexp.MustCompile(`\*\*([^*]+)\*\*`)
	terms := make(map[string][]int)
	for i, line := range strings.Split(content, "\n") {
		for _, m := range boldRe.FindAllStringSubmatch(line, -1) {
			term := strings.ToLower(strings.TrimSpace(m[1]))
			terms[term] = append(terms[term], i+1)
		}
	}
	return terms
}

func findTermConflicts(terms map[string][]int) []string {
	// Placeholder: in future, use edit distance to find similar but different terms
	return nil
}

func extractHeaders(lines []string) []string {
	var headers []string
	for _, line := range lines {
		if strings.HasPrefix(line, "#") {
			headers = append(headers, strings.TrimSpace(line))
		}
	}
	return headers
}

func findDuplicateHeaders(headers []string) []string {
	seen := make(map[string]bool)
	var dups []string
	for _, h := range headers {
		if seen[h] {
			dups = append(dups, h)
		}
		seen[h] = true
	}
	return dups
}

func grepSymbol(filePath, symbol string) bool {
	cmd := exec.Command("grep", "-q", symbol, filePath)
	return cmd.Run() == nil
}

// FormatResult formats verify result as human-readable JSON.
func FormatResult(result *VerifyResult) (string, error) {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
