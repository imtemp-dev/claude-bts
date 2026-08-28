package engine

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ImportGraph maps source files to the modules/packages they import.
// Keys are the file paths as given; values are import specifiers in the
// language's native form (package path for Go, module specifier for TS,
// dotted name for Python).
type ImportGraph map[string][]string

// Language detection is path-based — we do not execute or compile the
// files. The regex families below cover the three languages jig targets
// today; others pass through as empty.
var (
	goImportLineRe     = regexp.MustCompile(`^\s*import\s+(?:\(\s*)?(?:([a-zA-Z_][\w]*)\s+)?"([^"]+)"`)
	goImportBlockStart = regexp.MustCompile(`^\s*import\s*\(\s*$`)
	goImportBlockItem  = regexp.MustCompile(`^\s*(?:[a-zA-Z_][\w]*\s+)?"([^"]+)"\s*$`)

	tsImportRe     = regexp.MustCompile(`(?m)^(?:\s*import\s+[^'"]+|\s*import\s*)['"]([^'"]+)['"]`)
	tsDynamicImport = regexp.MustCompile(`import\(\s*['"]([^'"]+)['"]\s*\)`)
	tsReExportRe    = regexp.MustCompile(`(?m)^\s*export\s+(?:\*|\{[^}]*\})\s+from\s+['"]([^'"]+)['"]`)

	pyImportRe     = regexp.MustCompile(`(?m)^\s*import\s+([\w\.]+)`)
	pyFromImportRe = regexp.MustCompile(`(?m)^\s*from\s+([\w\.]+)\s+import\b`)
)

// ExtractImportGraph parses import/require statements from each file
// and returns the resulting graph. Files whose extension does not
// map to a supported language are kept with empty import lists —
// callers can still detect their presence but won't get spurious
// "missing import" signals.
//
// Paths may be absolute or relative; we do not resolve them beyond
// reading the file at the given path.
func ExtractImportGraph(files []string) (ImportGraph, error) {
	graph := ImportGraph{}
	for _, path := range files {
		imports, err := extractFileImports(path)
		if err != nil {
			// Missing files are not fatal — the graph reflects what is
			// on disk. Callers pair the graph with tasks.json, which
			// already tracks file existence.
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("extract %s: %w", path, err)
		}
		graph[path] = imports
	}
	return graph, nil
}

func extractFileImports(path string) ([]string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return extractGoImports(path)
	case ".ts", ".tsx", ".mts", ".cts":
		return extractTSImports(path)
	case ".js", ".mjs", ".cjs", ".jsx":
		return extractTSImports(path) // same regex family works
	case ".py":
		return extractPyImports(path)
	default:
		// Unknown language — return an empty list rather than failing,
		// so the graph still records the file as present.
		return nil, nil
	}
}

func extractGoImports(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	seen := map[string]bool{}
	var out []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	inBlock := false
	for scanner.Scan() {
		line := scanner.Text()
		if inBlock {
			if strings.TrimSpace(line) == ")" {
				inBlock = false
				continue
			}
			if m := goImportBlockItem.FindStringSubmatch(line); len(m) >= 2 {
				addImport(&out, seen, m[1])
			}
			continue
		}
		if goImportBlockStart.MatchString(line) {
			inBlock = true
			continue
		}
		if m := goImportLineRe.FindStringSubmatch(line); len(m) >= 3 {
			addImport(&out, seen, m[2])
		}
	}
	return out, scanner.Err()
}

func extractTSImports(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := string(data)

	seen := map[string]bool{}
	var out []string
	for _, m := range tsImportRe.FindAllStringSubmatch(content, -1) {
		if len(m) >= 2 {
			addImport(&out, seen, m[1])
		}
	}
	for _, m := range tsDynamicImport.FindAllStringSubmatch(content, -1) {
		if len(m) >= 2 {
			addImport(&out, seen, m[1])
		}
	}
	for _, m := range tsReExportRe.FindAllStringSubmatch(content, -1) {
		if len(m) >= 2 {
			addImport(&out, seen, m[1])
		}
	}
	return out, nil
}

func extractPyImports(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := string(data)

	seen := map[string]bool{}
	var out []string
	for _, m := range pyImportRe.FindAllStringSubmatch(content, -1) {
		if len(m) >= 2 {
			addImport(&out, seen, m[1])
		}
	}
	for _, m := range pyFromImportRe.FindAllStringSubmatch(content, -1) {
		if len(m) >= 2 {
			addImport(&out, seen, m[1])
		}
	}
	return out, nil
}

func addImport(out *[]string, seen map[string]bool, spec string) {
	spec = strings.TrimSpace(spec)
	if spec == "" || seen[spec] {
		return
	}
	seen[spec] = true
	*out = append(*out, spec)
}

// RenderMermaid produces a mermaid flowchart of the graph. Files become
// nodes; each import becomes an edge from the file to a "module" node
// derived from the import specifier's last segment. Deterministic
// alphabetical ordering keeps diff noise low across runs.
func (g ImportGraph) RenderMermaid() string {
	var lines []string
	lines = append(lines, "```mermaid")
	lines = append(lines, "flowchart LR")

	files := make([]string, 0, len(g))
	for f := range g {
		files = append(files, f)
	}
	sort.Strings(files)

	// Collect distinct module nodes so we can emit them once.
	modules := map[string]bool{}
	for _, f := range files {
		for _, imp := range g[f] {
			modules[imp] = true
		}
	}
	modList := make([]string, 0, len(modules))
	for m := range modules {
		modList = append(modList, m)
	}
	sort.Strings(modList)

	for _, f := range files {
		lines = append(lines, fmt.Sprintf(`    %s["%s"]`, sanitizeNode(f), filepath.Base(f)))
	}
	for _, m := range modList {
		lines = append(lines, fmt.Sprintf(`    %s(["%s"])`, sanitizeNode("mod_"+m), m))
	}
	for _, f := range files {
		for _, imp := range g[f] {
			lines = append(lines, fmt.Sprintf("    %s --> %s",
				sanitizeNode(f), sanitizeNode("mod_"+imp)))
		}
	}
	lines = append(lines, "```")
	return strings.Join(lines, "\n")
}

// sanitizeNode turns arbitrary strings into safe mermaid identifiers.
var sanitizeNodeRe = regexp.MustCompile(`[^A-Za-z0-9_]`)

func sanitizeNode(s string) string {
	return sanitizeNodeRe.ReplaceAllString(s, "_")
}
