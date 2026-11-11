package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/engine"
)

// Trial problem #130: `bts recipe confirm` (#88) wrote changelog action
// `confirm` and `bts recipe create --amends` (#90) wrote `amended`, and
// `bts validate` rejected both, since neither was added to the allowed set:
// warm-owl's r-003 carried four errors nothing on the recipe side could
// clear.  Every action the CLI and the hooks write is read from the source
// here and checked against the set, so the next new one cannot slip by.

// writtenChangelogActions reads the non-test Go files of dirs for every
// ChangelogEntry{Action: "..."} literal and appendEvidenceChangelog(...,
// "<action>", ...) call.
func writtenChangelogActions(t *testing.T, dirs ...string) map[string]string {
	t.Helper()
	written := map[string]string{}
	fset := token.NewFileSet()
	for _, dir := range dirs {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.CompositeLit:
					if !isChangelogEntry(x.Type) {
						return true
					}
					for _, el := range x.Elts {
						if kv, ok := el.(*ast.KeyValueExpr); ok {
							if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "Action" {
								if s, ok := stringLiteral(kv.Value); ok {
									written[s] = fset.Position(kv.Pos()).String()
								}
							}
						}
					}
				case *ast.CallExpr:
					if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "appendEvidenceChangelog" && len(x.Args) >= 3 {
						if s, ok := stringLiteral(x.Args[2]); ok {
							written[s] = fset.Position(x.Pos()).String()
						}
					}
				}
				return true
			})
		}
	}
	return written
}

func isChangelogEntry(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name == "ChangelogEntry"
	case *ast.SelectorExpr:
		return x.Sel.Name == "ChangelogEntry"
	}
	return false
}

func stringLiteral(e ast.Expr) (string, bool) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(bl.Value)
	return s, err == nil
}

func TestChangelogActions_EveryActionWrittenIsValid(t *testing.T) {
	written := writtenChangelogActions(t, ".", filepath.Join("..", "hook"), filepath.Join("..", "state"))
	if len(written) < 10 {
		t.Fatalf("found only %d changelog actions in the source; the scan is broken", len(written))
	}
	var bad []string
	for action, at := range written {
		if !engine.ChangelogActions[action] {
			bad = append(bad, action+" ("+at+")")
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Errorf("bts writes changelog actions that bts validate rejects: %s", strings.Join(bad, "; "))
	}
}

// The schema rule tells agents which actions they may log, and says it
// must match the validator's set; it had fallen nine actions behind.
func TestChangelogActions_TheSchemaRuleListsThemAll(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "template", "templates", ".claude", "rules", "bts-schema.md"))
	if err != nil {
		t.Fatal(err)
	}
	var line string
	for _, l := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(l, "- `action` (string): one of") {
			line = l
		}
	}
	if line == "" {
		t.Fatal("bts-schema.md has no changelog action list")
	}
	listed := map[string]bool{}
	for _, m := range regexp.MustCompile(`"([a-z-]+)"`).FindAllStringSubmatch(line, -1) {
		listed[m[1]] = true
	}
	var missing, extra []string
	for a := range engine.ChangelogActions {
		if !listed[a] {
			missing = append(missing, a)
		}
	}
	for a := range listed {
		if !engine.ChangelogActions[a] {
			extra = append(extra, a)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing)+len(extra) > 0 {
		t.Errorf("bts-schema.md's action list differs from engine.ChangelogActions: missing %v, not allowed %v", missing, extra)
	}
}
