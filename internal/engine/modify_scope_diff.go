package engine

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Modify-scope report (trial problem #47). A modify task declares the
// symbols it may touch (modify_scope), and the rule used to forbid every
// other module-level symbol, so on r-003 the worker wrote a regex inline
// to stay in scope and the mid-run reviewer flagged it, because every
// other pattern in that module is a module-level constant. Meanwhile
// nothing compared the code with the scope: `bts check task` reported
// only a scoped symbol that went missing.
//
// The rule is now: a modify task changes its scoped symbols, and may add
// or change private module-level helpers and constants that only those
// symbols use. checkScopeChanges compares the task's file with its
// committed version (git HEAD) and reports, as major structure findings
// (a report the mid-run and final reviews read, not a block, as every
// MINI-CHECK finding below critical is):
//
//   - a public top-level symbol outside the scope that was added, removed
//     or changed;
//   - a private top-level symbol that was added or changed and is used by
//     something outside the scope, directly or through other private
//     symbols.
//
// It reads top-level declarations line by line for Python, Go and
// TypeScript/JavaScript; other languages, a file git does not have at
// HEAD, and the `legacy` scope are skipped. A class counts as in scope
// when the scope names it or a member defined in it, so a change to
// another member of that class is not separated out.

// scopeDecl is one top-level declaration, or a run of top-level code that
// declares nothing (name "").
type scopeDecl struct {
	name    string
	public  bool
	isClass bool
	text    string // normalised: no blank or comment-only lines
}

var classHeadRe = regexp.MustCompile(`^(?:export\s+)?(?:default\s+)?(?:declare\s+)?(?:abstract\s+)?class\s`)

type scopeLang struct {
	comment  string
	quotes   []string // delimiters that may open a multi-line literal
	declName func(line string) (name string, ok bool)
	// member reports whether a declaration's text defines member m.
	member func(text, m string) bool
	// public reports whether a declaration name is public; exported holds
	// names a separate export statement made public (TypeScript).
	public func(name string, line string, exported map[string]bool) bool
}

var (
	pyDeclRe     = regexp.MustCompile(`^(?:async\s+def|def|class)\s+([A-Za-z_]\w*)`)
	pyAssignRe   = regexp.MustCompile(`^([A-Za-z_]\w*)\s*(?::[^=]*)?=[^=]`)
	goFuncRe     = regexp.MustCompile(`^func\s+(?:\(\s*(?:\w+\s+)?\*?\s*(\w+)(?:\[[^\]]*\])?\s*\)\s*)?([A-Za-z_]\w*)`)
	goDeclRe     = regexp.MustCompile(`^(?:type|var|const)\s+([A-Za-z_]\w*)`)
	goGroupRe    = regexp.MustCompile(`^(?:type|var|const)\s*\(\s*$`)
	goGroupItem  = regexp.MustCompile(`^\t([A-Za-z_]\w*)`)
	tsDeclRe     = regexp.MustCompile(`^(export\s+)?(?:default\s+)?(?:declare\s+)?(?:abstract\s+)?(?:async\s+)?(?:function\*?|class|interface|type|enum|const|let|var|namespace)\s+([A-Za-z_$][\w$]*)`)
	tsDefaultRe  = regexp.MustCompile(`^export\s+default\b`)
	tsExportList = regexp.MustCompile(`(?m)^export\s*\{([^}]*)\}\s*;?\s*$`)
)

var scopeLangs = map[string]*scopeLang{
	".py": {
		comment: "#",
		quotes:  []string{`"""`, `'''`},
		declName: func(line string) (string, bool) {
			if m := pyDeclRe.FindStringSubmatch(line); m != nil {
				return m[1], true
			}
			if m := pyAssignRe.FindStringSubmatch(line); m != nil {
				return m[1], true
			}
			return "", false
		},
		member: func(text, m string) bool {
			re := regexp.MustCompile(`(?m)^\s+(?:(?:async\s+)?def\s+` + regexp.QuoteMeta(m) + `\b|` + regexp.QuoteMeta(m) + `\s*(?::[^=\n]*)?=[^=])`)
			return re.MatchString(text)
		},
		public: func(name, _ string, _ map[string]bool) bool {
			// A dunder (__all__, __version__) is module API, not a helper.
			return !strings.HasPrefix(name, "_") || (strings.HasPrefix(name, "__") && strings.HasSuffix(name, "__"))
		},
	},
	".go": {
		comment: "//",
		quotes:  []string{"`"},
		declName: func(line string) (string, bool) {
			if m := goFuncRe.FindStringSubmatch(line); m != nil {
				if m[1] != "" {
					return m[1] + "." + m[2], true
				}
				return m[2], true
			}
			if m := goDeclRe.FindStringSubmatch(line); m != nil {
				return m[1], true
			}
			return "", false
		},
		member: func(string, string) bool { return false },
		public: func(name, _ string, _ map[string]bool) bool {
			if i := strings.LastIndex(name, "."); i >= 0 {
				name = name[i+1:]
			}
			r := []rune(name)
			return len(r) > 0 && unicode.IsUpper(r[0])
		},
	},
}

func init() {
	ts := &scopeLang{
		comment: "//",
		quotes:  []string{"`"},
		declName: func(line string) (string, bool) {
			if m := tsDeclRe.FindStringSubmatch(line); m != nil {
				return m[2], true
			}
			if tsDefaultRe.MatchString(line) {
				return "default", true
			}
			return "", false
		},
		member: func(text, m string) bool {
			re := regexp.MustCompile(`(?m)^\s+(?:(?:public|private|protected|static|readonly|async|override|get|set)\s+)*` +
				regexp.QuoteMeta(m) + `\s*[(<:=?!]`)
			return re.MatchString(text)
		},
		public: func(name, line string, exported map[string]bool) bool {
			return strings.HasPrefix(line, "export") || exported[name]
		},
	}
	for _, ext := range []string{".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs"} {
		scopeLangs[ext] = ts
	}
}

// parseScopeDecls splits source into top-level declarations. A new block
// starts at every line in column 0 that is not blank, a comment, a closing
// bracket or inside a multi-line literal; a decorator line opens the block
// of the declaration after it. Go's grouped declarations (`const (`) yield
// one block per entry.
func parseScopeDecls(lang *scopeLang, src string) []scopeDecl {
	exported := map[string]bool{}
	for _, m := range tsExportList.FindAllStringSubmatch(src, -1) {
		for _, part := range strings.Split(m[1], ",") {
			if f := strings.Fields(part); len(f) > 0 {
				exported[f[0]] = true
			}
		}
	}
	type block struct {
		name, head string
		lines      []string
	}
	var blocks []block
	var cur *block
	pendingDecorator := false
	inQuote := ""
	inGroup := false
	start := func(name, head string, line string) {
		if pendingDecorator && cur != nil && cur.name == "" {
			cur.name, cur.head = name, head
		} else {
			blocks = append(blocks, block{name: name, head: head})
			cur = &blocks[len(blocks)-1]
		}
		pendingDecorator = false
		cur.lines = append(cur.lines, line)
	}
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		boundary := inQuote == "" && line != "" && !unicode.IsSpace(rune(line[0])) &&
			!strings.HasPrefix(trimmed, lang.comment) && !strings.HasPrefix(trimmed, "/*") &&
			!strings.ContainsAny(trimmed[:1], ")]}")
		if inGroup && inQuote == "" && strings.HasPrefix(line, ")") {
			inGroup = false
		}
		switch {
		case inGroup && inQuote == "" && goGroupItem.MatchString(line):
			name := goGroupItem.FindStringSubmatch(line)[1]
			blocks = append(blocks, block{name: name, head: line})
			cur = &blocks[len(blocks)-1]
			cur.lines = append(cur.lines, line)
		case boundary:
			inGroup = false
			if strings.HasPrefix(trimmed, "@") {
				if !pendingDecorator {
					blocks = append(blocks, block{})
					cur = &blocks[len(blocks)-1]
					pendingDecorator = true
				}
				cur.lines = append(cur.lines, line)
				break
			}
			if lang == scopeLangs[".go"] && goGroupRe.MatchString(line) {
				inGroup = true
				blocks = append(blocks, block{})
				cur = &blocks[len(blocks)-1]
				break
			}
			if name, ok := lang.declName(line); ok {
				start(name, line, line)
				break
			}
			// Top-level code that declares nothing: consecutive lines of it
			// are one block, so the header (docstring, imports) is block 0.
			if cur == nil || cur.name != "" || pendingDecorator {
				blocks = append(blocks, block{})
				cur = &blocks[len(blocks)-1]
			}
			pendingDecorator = false
			cur.lines = append(cur.lines, line)
		default:
			if cur == nil {
				blocks = append(blocks, block{})
				cur = &blocks[len(blocks)-1]
			}
			cur.lines = append(cur.lines, line)
		}
		if inQuote == "" && strings.HasPrefix(trimmed, lang.comment) {
			continue // a quote in a comment opens nothing
		}
		for _, q := range lang.quotes {
			if strings.Count(line, q)%2 == 1 {
				switch inQuote {
				case "":
					inQuote = q
				case q:
					inQuote = ""
				}
			}
		}
	}
	out := make([]scopeDecl, 0, len(blocks))
	for _, b := range blocks {
		d := scopeDecl{name: b.name, text: normaliseScopeText(lang, b.lines)}
		if b.name != "" {
			d.public = lang.public(b.name, b.head, exported)
			d.isClass = classHeadRe.MatchString(b.head)
		}
		out = append(out, d)
	}
	return out
}

func normaliseScopeText(lang *scopeLang, lines []string) string {
	var keep []string
	for _, l := range lines {
		t := strings.TrimRight(l, " \t\r")
		if s := strings.TrimSpace(t); s == "" || strings.HasPrefix(s, lang.comment) {
			continue
		}
		keep = append(keep, t)
	}
	return strings.Join(keep, "\n")
}

// scopeCovers reports whether a declaration is one the scope lets the task
// touch: named in it, a class with a member named in it, or for Go a
// method whose type or method name is named.
func scopeCovers(lang *scopeLang, d scopeDecl, scope []string) bool {
	recv, method := "", d.name
	if i := strings.LastIndex(d.name, "."); i >= 0 {
		recv, method = d.name[:i], d.name[i+1:]
	}
	for _, s := range scope {
		s = strings.TrimSpace(s)
		switch {
		case s == d.name, recv != "" && (s == method || s == recv):
			return true
		case strings.HasPrefix(s, d.name+"."):
			return true
		case d.isClass && !strings.Contains(s, ".") && lang.member(d.text, s):
			return true
		}
	}
	return false
}

// checkScopeChanges is the modify-scope report described at the top of
// this file.
func checkScopeChanges(projectRoot, recipeDir string, task *state.Task, post string) []state.StructureFinding {
	if task.Action != "modify" || len(task.ModifyScope) == 0 || isLegacyScope(task.ModifyScope) || projectRoot == "" {
		return nil
	}
	lang := scopeLangs[strings.ToLower(filepath.Ext(task.File))]
	if lang == nil {
		return nil
	}
	pre, ok := headFile(projectRoot, task.File)
	if !ok {
		return nil
	}
	scope := scopeNames(fileScope(recipeDir, task))

	before := map[string]scopeDecl{}
	beforeList := parseScopeDecls(lang, pre)
	for _, d := range beforeList {
		if d.name != "" {
			before[d.name] = d
		}
	}
	after := parseScopeDecls(lang, post)
	now := map[string]scopeDecl{}
	for _, d := range after {
		if d.name != "" {
			now[d.name] = d
		}
	}

	// Which declarations may a scoped change reach: the scoped ones, and
	// each private one whose every user is itself allowed. Start from all
	// private ones and strike out until nothing changes, so two helpers
	// that call each other for a scoped symbol both stay allowed.
	allowed := map[string]bool{}
	users := map[string][]string{} // private, unscoped name -> the blocks that use it
	for name, d := range now {
		covered := scopeCovers(lang, d, scope)
		allowed[name] = covered || !d.public
		if covered || d.public {
			continue
		}
		re := symbolRegexp(name)
		for i, u := range after {
			if u.name == name || i == 0 && u.name == "" {
				continue // itself, or the file header (imports, docstring)
			}
			if re.MatchString(u.text) {
				label := u.name
				if label == "" {
					label = "top-level code"
				}
				users[name] = append(users[name], label)
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for name, us := range users {
			if !allowed[name] {
				continue
			}
			for _, u := range us {
				if !allowed[u] {
					allowed[name] = false
					changed = true
					break
				}
			}
		}
	}

	names := map[string]bool{}
	for n := range before {
		names[n] = true
	}
	for n := range now {
		names[n] = true
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)

	scopeText := joinSorted(scope)
	var findings []state.StructureFinding
	for _, name := range sorted {
		b, wasThere := before[name]
		a, isThere := now[name]
		how := ""
		switch {
		case wasThere && !isThere:
			how = "removed"
		case !wasThere && isThere:
			how = "added"
		case b.text != a.text:
			how = "changed"
		default:
			continue
		}
		d := a
		if !isThere {
			d = b
		}
		if scopeCovers(lang, d, scope) {
			continue
		}
		if d.public && how == "removed" && removedForScope(projectRoot, lang, task.File, name, beforeList, now, scope) {
			continue // a helper only the scope's own or removed code used
		}
		if d.public {
			findings = append(findings, state.StructureFinding{
				TaskID: task.ID, Category: "scope_violation", Severity: "major",
				Detail: "Task " + task.ID + " " + how + " public symbol '" + name + "' in " + task.File +
					", outside its modify_scope " + scopeText + ". If the task owns it, widen the task's scope= and " +
					"modify_scope with the user; otherwise undo that part of the change.",
			})
			continue
		}
		if !isThere || allowed[name] {
			continue // a private helper or constant only the scope uses
		}
		var outside []string
		for _, u := range users[name] {
			if !allowed[u] {
				outside = append(outside, u)
			}
		}
		findings = append(findings, state.StructureFinding{
			TaskID: task.ID, Category: "scope_violation", Severity: "major",
			Detail: "Task " + task.ID + " " + how + " private symbol '" + name + "' in " + task.File +
				", and it is used outside its modify_scope " + scopeText + " (by " + strings.Join(outside, ", ") +
				"). A private helper or constant may serve only the scoped symbols.",
		})
	}
	return findings
}

// removedForScope reports whether a public declaration the task removed
// served only the scope: at HEAD some declaration in the file used it,
// every one that did is in the scope or gone now, and no other file git
// tracks names it.  Test helpers that only removed tests used are the
// common case (trial problem #137).
func removedForScope(projectRoot string, lang *scopeLang, file, name string, beforeList []scopeDecl, now map[string]scopeDecl, scope []string) bool {
	re := symbolRegexp(name)
	used := false
	for i, u := range beforeList {
		if u.name == name || i == 0 && u.name == "" || !re.MatchString(u.text) {
			continue
		}
		if u.name == "" {
			return false // top-level code used it
		}
		if _, still := now[u.name]; still && !scopeCovers(lang, u, scope) {
			return false
		}
		used = true
	}
	return used && !namedElsewhere(projectRoot, file, name)
}

// namedElsewhere reports whether a file git tracks, other than file and
// bts's own, names name as a word.  It errs towards true when git cannot
// say.
func namedElsewhere(root, file, name string) bool {
	cmd := exec.Command("git", "-C", root, "grep", "-l", "-w", "-F", "-e", name, "--", ".",
		":(exclude)"+filepath.ToSlash(filepath.Clean(file)), ":(exclude).bts", ":(exclude).claude")
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		return !errors.As(err, &ee) || ee.ExitCode() != 1 // 1: no match
	}
	return strings.TrimSpace(string(out)) != ""
}

// fileScope is the scope of every modify task on the task's file: two
// tasks may each own part of one file, and one's symbols are not outside
// the other's change.
func fileScope(recipeDir string, task *state.Task) []string {
	scope := append([]string(nil), task.ModifyScope...)
	if recipeDir == "" {
		return scope
	}
	data, err := os.ReadFile(filepath.Join(recipeDir, "tasks.json"))
	if err != nil {
		return scope
	}
	var ts state.TaskState
	if json.Unmarshal(data, &ts) != nil {
		return scope
	}
	for _, t := range ts.Tasks {
		if t.ID == task.ID || t.Action != "modify" || filepath.Clean(t.File) != filepath.Clean(task.File) || isLegacyScope(t.ModifyScope) {
			continue
		}
		scope = append(scope, t.ModifyScope...)
	}
	return scope
}

// headFile is the file as committed at HEAD: the version before the
// recipe's change, since a recipe's work stays uncommitted until it is
// reviewed. ok=false outside git or for a file HEAD does not have.
func headFile(root, rel string) (string, bool) {
	rel = filepath.ToSlash(filepath.Clean(rel))
	if filepath.IsAbs(rel) {
		r, err := filepath.Rel(root, rel)
		if err != nil || strings.HasPrefix(r, "..") {
			return "", false
		}
		rel = filepath.ToSlash(r)
	}
	out, err := exec.Command("git", "-C", root, "show", "HEAD:"+rel).Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}
