package engine

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// CheckTaskStructure runs the Phase 10 "MINI-CHECK" on a single task:
// deterministic, ~1-second checks that run right after the task's
// build passes. Findings are recorded on tasks.json and surface later
// in the mid-run review (Phase 11) and the final bts-review.
//
// The checks covered (all advisory — none block the task itself; the
// caller decides whether a critical finding should flip the task back
// to blocked):
//
//  1. Import delta — does the file's import set stay within the
//     component boundaries declared by wireframe.md?
//  2. Symbol presence — for create tasks, every symbol named in final.md
//     must exist in the file; for modify tasks, every ModifyScope symbol
//     must still be present (did we accidentally delete one?), and a
//     public symbol outside the scope must not have changed since the
//     file's committed version (scope_violation, a major report).
//  3. Invariant owner delta — when this file is the module domain.md § 2
//     names as an invariant's owner, the member its Enforcement point
//     names (Owner.member()) must still be declared in the file.
//
// projectRoot is the working directory used to resolve the task's file
// path. recipeDir is where wireframe.md / domain.md / final.md live.
// Either may be empty to skip the corresponding check.
func CheckTaskStructure(projectRoot, recipeDir string, task *state.Task) []state.StructureFinding {
	if task == nil {
		return nil
	}
	var findings []state.StructureFinding

	filePath := task.File
	if !filepath.IsAbs(filePath) && projectRoot != "" {
		filePath = filepath.Join(projectRoot, task.File)
	}

	// File may not exist yet (e.g. blocked before IMPLEMENT). Skip
	// everything that needs the source bytes rather than producing
	// noise.
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}
	fileContent := string(fileData)

	// 1. Import delta.
	if f := checkImportDelta(task, filePath, recipeDir); f != nil {
		findings = append(findings, *f)
	}

	// 2. Symbol presence.
	findings = append(findings, checkSymbolPresence(task, fileContent)...)

	// 2b. Modify scope: what the task changed outside its scope, against
	// the file as committed (modify_scope_diff.go, trial problem #47).
	findings = append(findings, checkScopeChanges(projectRoot, recipeDir, task, fileContent)...)

	// 3. Invariant owner delta.
	if f := checkOwnerDelta(task, fileContent, recipeDir); f != nil {
		findings = append(findings, *f)
	}

	return findings
}

// checkImportDelta reuses the existing import_graph.go parsers to grab
// the task file's actual imports, then compares the set to the import
// slice implied by the wireframe's component diagram — any module the
// wireframe does NOT list as a neighbor of this component surfaces as
// import_drift (major).
//
// When recipeDir or wireframe.md is missing, returns nil. This is a
// best-effort check; absent upstream artifacts fall back silently.
func checkImportDelta(task *state.Task, filePath, recipeDir string) *state.StructureFinding {
	if recipeDir == "" {
		return nil
	}
	wireframePath := filepath.Join(recipeDir, "wireframe.md")
	wireframeData, err := os.ReadFile(wireframePath)
	if err != nil {
		return nil
	}

	graph, err := ExtractImportGraph([]string{filePath})
	if err != nil {
		return nil
	}
	imports, ok := graph[filePath]
	if !ok || len(imports) == 0 {
		return nil
	}

	// Build the allowed-module set from wireframe mermaid labels.
	// Each node in the diagram corresponds to a file or module path;
	// we collect the DIRECTORY of each path. An import is "authorized"
	// when its own directory (or any ancestor) is in the set.
	allowed := allowedModuleDirsFromWireframe(string(wireframeData))
	if len(allowed) == 0 {
		return nil
	}

	var drifts []string
	for _, imp := range imports {
		if !looksLocalImport(imp) {
			continue
		}
		if isDirAllowed(moduleDir(imp), allowed) {
			continue
		}
		drifts = append(drifts, imp)
	}
	if len(drifts) == 0 {
		return nil
	}
	sort.Strings(drifts)
	return &state.StructureFinding{
		TaskID:   task.ID,
		Category: "import_drift",
		Severity: "major",
		Detail:   "Task " + task.ID + " imports module(s) not listed as neighbors in wireframe.md: " + strings.Join(drifts, ", ") + ". Either update the wireframe or remove the import.",
	}
}

var wireframeNodeLabelRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*\[["]([^"\n]+)["]\]`)

// allowedModuleDirsFromWireframe pulls path-looking tokens out of the
// wireframe's component node labels, then records the directory part
// of each. An import whose module directory matches any recorded dir
// is considered authorized.
func allowedModuleDirsFromWireframe(wireframe string) map[string]bool {
	out := map[string]bool{}
	for _, m := range wireframeNodeLabelRe.FindAllStringSubmatch(wireframe, -1) {
		if len(m) < 2 {
			continue
		}
		label := m[1]
		// Node labels look like `src/auth/oauth.ts\n(authenticates users)`.
		// Grab the first whitespace/newline-separated token.
		first := strings.Fields(strings.ReplaceAll(label, `\n`, " "))
		if len(first) == 0 {
			continue
		}
		dir := moduleDir(first[0])
		if dir == "" {
			// Bare module name (e.g. "fmt") — record as-is so stdlib-
			// style imports can match.
			out[first[0]] = true
			continue
		}
		out[dir] = true
	}
	return out
}

// moduleDir returns the directory part of an import path or node label.
// "src/auth/oauth.ts" → "src/auth"; "./foo" → "" (no directory —
// treat as bare module); "fmt" → "".
func moduleDir(p string) string {
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "../")
	idx := strings.LastIndex(p, "/")
	if idx < 0 {
		return ""
	}
	return p[:idx]
}

// isDirAllowed returns true when dir (or any ancestor above it) is a
// member of the allowed set. This allows `src/auth/utils/helpers.ts`
// to match an allowed dir of `src/auth`.
func isDirAllowed(dir string, allowed map[string]bool) bool {
	if dir == "" {
		return true // bare modules fall through — third-party handling
	}
	for {
		if allowed[dir] {
			return true
		}
		idx := strings.LastIndex(dir, "/")
		if idx < 0 {
			return false
		}
		dir = dir[:idx]
	}
}

// looksLocalImport distinguishes relative/in-repo imports from standard-
// library or third-party ones. Heuristic: contains a slash OR starts
// with "./" or "../".
func looksLocalImport(imp string) bool {
	if strings.HasPrefix(imp, "./") || strings.HasPrefix(imp, "../") {
		return true
	}
	// Go-style path with a dot-separated TLD is usually third-party
	// ("github.com/..."); keep our check to slashy names without dots
	// in the first segment.
	if strings.Contains(imp, "/") && !strings.Contains(strings.Split(imp, "/")[0], ".") {
		return true
	}
	return false
}

// checkSymbolPresence enforces that the spec's promised symbols are
// actually present in the file.
func checkSymbolPresence(task *state.Task, fileContent string) []state.StructureFinding {
	var findings []state.StructureFinding

	// For modify tasks, the scope symbols must still exist in the file.
	// CheckModifyScope covers this at validation time, but that check
	// only runs with filesystem access to the project root — per-task
	// check is the right place to catch a symbol that survived
	// declaration but got deleted during implementation.
	if task.Action == "modify" && !isLegacyScope(task.ModifyScope) {
		for _, sym := range presentScope(task.ModifyScope) {
			if !ContainsSymbol(fileContent, sym) {
				findings = append(findings, state.StructureFinding{
					TaskID:   task.ID,
					Category: "symbol_missing",
					Severity: "critical",
					Detail: "Task " + task.ID + " declares modify_scope symbol '" + sym + "' but " + task.File + " no longer contains it. " +
						"If the task removes it, declare it as `-" + sym + "` in scope= and modify_scope; otherwise restore the symbol, update the scope, or mark the task blocked.",
				})
			}
		}
		// A declared removal still declared in the file: the task said it
		// removes the name.  A report, as every finding below critical is.
		for _, sym := range removalScope(task.ModifyScope) {
			if declaresName(task.File, fileContent, sym) {
				findings = append(findings, state.StructureFinding{
					TaskID:   task.ID,
					Category: "removal_pending",
					Severity: "major",
					Detail:   "Task " + task.ID + " declares that it removes '" + sym + "' (`-" + sym + "` in its scope), but " + task.File + " still declares it. Remove it, or take the removal out of the scope.",
				})
			}
		}
	}
	return findings
}

// checkOwnerDelta flags an invariant whose enforcement left its owner:
// domain.md § 2 names this file's module as the owner, its Enforcement
// point names a member of it (`Arrangement.place()`), and the file no
// longer declares that member.
//
// The owner is matched to the file by module name, the basename without
// extension, ignoring case, "_" and "-" (`ReportEngine` is
// report_engine.py).  The file is then that module, so nothing more is
// asked of its prose: the check used to require the owner's name as a
// word in the source, and a correct engine.py whose source never spelled
// "Engine" was recorded critical (trial problem #134).  An Enforcement
// point that names no member of the owner ("derived, no independent
// store") is not checked.
func checkOwnerDelta(task *state.Task, fileContent, recipeDir string) *state.StructureFinding {
	if recipeDir == "" {
		return nil
	}
	domainPath := filepath.Join(recipeDir, "domain.md")
	if _, err := os.Stat(domainPath); err != nil {
		return nil
	}
	inv, err := parseInvariantsTable(domainPath)
	if err != nil || len(inv) == 0 {
		return nil
	}
	module := moduleKey(strings.TrimSuffix(filepath.Base(task.File), filepath.Ext(task.File)))
	for _, i := range inv {
		if i.Owner == "" || moduleKey(i.Owner) != module {
			continue
		}
		member := enforcementMember(i.Owner, i.Enforcement)
		if member == "" || declaresName(task.File, fileContent, member) {
			continue
		}
		return &state.StructureFinding{
			TaskID:   task.ID,
			Category: "owner_drift",
			Severity: "critical",
			Detail: "domain.md §2 makes '" + i.Owner + "' the owner of " + i.ID + ", enforced at '" + i.Owner + "." + member +
				"', and this task's file is that module, but " + task.File + " no longer declares '" + member +
				"'. Restore it, or, if the enforcement moved, update domain.md and its falsifier.",
		}
	}
	return nil
}

// moduleKey is a module or owner name compared without case, "_" or "-".
func moduleKey(s string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "", "`", "").Replace(strings.TrimSpace(s)))
}

// qualifiedNameRe is a dotted pair, Owner.member, in an Enforcement cell.
var qualifiedNameRe = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\.([A-Za-z_][A-Za-z0-9_]*)`)

// fileExtensions are suffixes a dotted pair can be a file name with
// (`engine.py`), never a member.
var fileExtensions = map[string]bool{
	"py": true, "pyi": true, "go": true, "ts": true, "tsx": true, "js": true, "jsx": true,
	"mjs": true, "cjs": true, "rs": true, "java": true, "kt": true, "cs": true, "rb": true,
	"swift": true, "php": true, "md": true, "json": true, "yaml": true, "yml": true, "toml": true,
}

// enforcementMember is the member an Enforcement point names on its
// owner: `place` in "Arrangement.place()".  "" when it names none.
func enforcementMember(owner, cell string) string {
	key := moduleKey(owner)
	for _, m := range qualifiedNameRe.FindAllStringSubmatchIndex(cell, -1) {
		if m[2] > 0 && strings.ContainsRune("/.", rune(cell[m[2]-1])) {
			continue // a path segment, not an owner
		}
		left, right := cell[m[2]:m[3]], cell[m[4]:m[5]]
		if moduleKey(left) == key && !fileExtensions[strings.ToLower(right)] {
			return right
		}
	}
	return ""
}
