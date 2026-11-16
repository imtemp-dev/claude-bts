package engine

import (
	"path/filepath"
	"strings"
)

// A modify scope lists the names a task may touch.  Two forms are special
// (trial problems #137, #133):
//
//   - "-name" declares a name the task removes: it is not looked for in the
//     file, it counts as in scope, and a declaration of it still in the
//     file is reported as removal_pending;
//   - "__doc__" stands for the file's module docstring or leading comment,
//     which no identifier spells, so it is never looked for either.
const scopeDocToken = "__doc__"

func isScopeRemoval(s string) bool {
	s = strings.TrimSpace(s)
	return len(s) > 1 && s[0] == '-'
}

// scopeNames are the scope's names with the removal mark dropped.
func scopeNames(scope []string) []string {
	out := make([]string, 0, len(scope))
	for _, s := range scope {
		if s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "-")); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// presentScope are the names the task's file must still contain: neither
// declared removals nor __doc__.
func presentScope(scope []string) []string {
	var out []string
	for _, s := range scope {
		s = strings.TrimSpace(s)
		if s == "" || s == scopeDocToken || isScopeRemoval(s) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// removalScope are the names the task declares it removes.
func removalScope(scope []string) []string {
	var out []string
	for _, s := range scope {
		if isScopeRemoval(s) {
			out = append(out, strings.TrimSpace(strings.TrimSpace(s)[1:]))
		}
	}
	return out
}

// declaresName reports whether a file's source still declares name: a
// top-level declaration of it, or a member of a top-level class, read the
// way the scope report reads them.  A file in a language that report does
// not read falls back to a whole-word match.
func declaresName(file, content, name string) bool {
	lang := scopeLangs[strings.ToLower(filepath.Ext(file))]
	if lang == nil {
		return ContainsSymbol(content, name)
	}
	for _, d := range parseScopeDecls(lang, content) {
		if d.name == name || d.isClass && lang.member(d.text, name) {
			return true
		}
		if i := strings.LastIndex(d.name, "."); i >= 0 && d.name[i+1:] == name {
			return true // a Go method
		}
	}
	return false
}
