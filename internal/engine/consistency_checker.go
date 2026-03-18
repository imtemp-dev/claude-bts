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
	Level     float64            `json:"level"`      // 0.0 ~ 3.0
	Checklist map[string]bool    `json:"checklist"`   // each criterion: met or not
	Missing   []string           `json:"missing"`     // what's needed for next level
}

// VerifyResult contains all verification results.
type VerifyResult struct {
	File       string     `json:"file"`
	Issues     []Issue    `json:"issues"`
	Level      LevelScore `json:"level"`
	Summary    Summary    `json:"summary"`
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
	"components_listed",      // 주요 컴포넌트 나열
	"relationships_described", // 컴포넌트 간 관계 설명
	"tech_stack_specified",    // 기술 스택 명시
}

var level2Criteria = []string{
	"data_flow_defined",       // 데이터 흐름 명시 (입력→처리→출력)
	"error_strategy_defined",  // 에러 처리 전략
	"interfaces_described",    // 주요 인터페이스 설명
	"tech_choices_rationale",  // 기술 선택 근거
}

var level3Criteria = []string{
	"file_paths_specified",    // 모든 파일 경로 명시
	"function_signatures",     // 함수 시그니처 (이름, 파라미터, 리턴)
	"type_definitions",        // 데이터 타입/인터페이스 정의
	"connection_points",       // 컴포넌트 연결점 구체적
	"error_cases_enumerated",  // 에러 케이스 열거
	"edge_cases_listed",       // Edge case 명시
	"scaffolding_included",    // 코드 스캐폴딩 포함
	"test_scenarios",          // 테스트 시나리오
}

// Keyword indicators for each criterion
var criteriaKeywords = map[string][]string{
	"components_listed":      {"component", "module", "service", "layer", "package", "컴포넌트", "모듈", "서비스"},
	"relationships_described": {"depends on", "calls", "connects", "integrates", "imports from", "의존", "호출", "연결"},
	"tech_stack_specified":    {"typescript", "python", "go", "react", "node", "express", "django", "postgresql", "redis"},
	"data_flow_defined":       {"input", "output", "request", "response", "flow", "pipeline", "→", "데이터 흐름", "입력", "출력"},
	"error_strategy_defined":  {"error", "exception", "catch", "throw", "retry", "fallback", "에러", "예외", "실패"},
	"interfaces_described":    {"interface", "api", "endpoint", "method", "function", "인터페이스", "엔드포인트"},
	"tech_choices_rationale":  {"because", "reason", "rationale", "chose", "selected", "over", "이유", "선택", "근거"},
	"file_paths_specified":    {"/", ".ts", ".go", ".py", ".js", "src/", "internal/", "cmd/"},
	"function_signatures":     {"function", "func ", "def ", "export", "()", "params", "returns", "파라미터", "리턴"},
	"type_definitions":        {"type ", "interface ", "struct ", "class ", "enum ", "타입", "인터페이스"},
	"connection_points":       {"calls", "imports", "uses", "호출", "사용", "연결점"},
	"error_cases_enumerated":  {"400", "401", "403", "404", "500", "timeout", "invalid", "unauthorized", "에러 케이스"},
	"edge_cases_listed":       {"edge case", "empty", "null", "concurrent", "large", "엣지", "빈 값", "동시"},
	"scaffolding_included":    {"```", "skeleton", "scaffold", "골격", "스캐폴딩"},
	"test_scenarios":          {"test", "scenario", "happy path", "should", "expect", "테스트", "시나리오"},
}

// VerifyDocument checks a document for internal consistency and assesses its level.
func VerifyDocument(docPath string, projectRoot string) (*VerifyResult, error) {
	data, err := os.ReadFile(docPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", docPath, err)
	}

	content := string(data)
	contentLower := strings.ToLower(content)
	result := &VerifyResult{File: docPath}

	// 1. Assess document level
	result.Level = assessLevel(contentLower)

	// 2. Check internal consistency
	checkInternalConsistency(content, result)

	// 3. If project has code, optionally check file/symbol references
	if projectRoot != "" {
		checkCodeReferences(content, projectRoot, result)
	}

	return result, nil
}

// assessLevel evaluates the document against level criteria checklists.
func assessLevel(contentLower string) LevelScore {
	checklist := make(map[string]bool)
	var missing []string

	// Check Level 1 criteria
	l1Met := 0
	for _, c := range level1Criteria {
		met := checkCriterion(contentLower, c)
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
		met := checkCriterion(contentLower, c)
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
		met := checkCriterion(contentLower, c)
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

func checkCriterion(contentLower string, criterion string) bool {
	keywords, ok := criteriaKeywords[criterion]
	if !ok {
		return false
	}
	matchCount := 0
	for _, kw := range keywords {
		if strings.Contains(contentLower, strings.ToLower(kw)) {
			matchCount++
		}
	}
	// Need at least 2 keyword matches to consider criterion met
	return matchCount >= 2
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
