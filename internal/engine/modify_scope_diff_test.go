package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// scopeRepo commits rel with body in a fresh repository and returns the
// project root and a recipe directory inside it.
func scopeRepo(t *testing.T, rel, body string) (root, recipeDir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root = t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init", "-q")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")
	writeScopeFile(t, root, rel, body)
	git("add", "-A")
	git("commit", "-q", "-m", "base")
	recipeDir = filepath.Join(root, ".bts", "specs", "recipes", "r-047")
	if err := os.MkdirAll(recipeDir, 0755); err != nil {
		t.Fatal(err)
	}
	return root, recipeDir
}

func writeScopeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	_ = os.MkdirAll(filepath.Dir(p), 0755)
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

// scopeViolations returns the symbol each scope_violation names, sorted.
func scopeViolations(t *testing.T, findings []state.StructureFinding) []string {
	t.Helper()
	var out []string
	for _, f := range findings {
		if f.Category != "scope_violation" {
			continue
		}
		if f.Severity != "major" {
			t.Errorf("a scope violation is a report (major), got %s: %s", f.Severity, f.Detail)
		}
		start := strings.Index(f.Detail, "symbol '") + len("symbol '")
		end := strings.Index(f.Detail[start:], "'")
		out = append(out, f.Detail[start:start+end])
	}
	sort.Strings(out)
	return out
}

const rulesBefore = `"""Rules for the relay."""
import os

_EXISTING = os.environ.get("X")


def validate(text):
    return bool(text)


def render(text):
    return text


class Session:
    def refresh(self):
        return 1

    def close(self):
        return 2
`

// Trial problem #47: a modify task may add a private module-level helper
// or constant that only its scoped symbols use (the r-003 worker inlined a
// regex to avoid one, and the reviewer flagged the inline). bts check task
// now reports what the old rule only asked for: a public symbol outside
// the scope that changed, and a private one that something outside the
// scope uses.
func TestCheckTaskStructure_ReportsChangesOutsideTheModifyScope(t *testing.T) {
	root, recipeDir := scopeRepo(t, "relay/rules.py", rulesBefore)
	task := &state.Task{ID: "t-003", File: "relay/rules.py", Action: "modify", ModifyScope: []string{"validate"}}

	// In scope: a new private constant the scoped function uses, a new
	// import and an edited docstring. Nothing to report.
	inScope := strings.Replace(rulesBefore, `import os`, "import os\nimport re", 1)
	inScope = strings.Replace(inScope, `"""Rules for the relay."""`, `"""Rules for the relay, and issue keys."""`, 1)
	inScope = strings.Replace(inScope, "def validate(text):\n    return bool(text)",
		"_ISSUE_KEY = re.compile(r\"^[A-Z]+-\\d+$\")\n\n\ndef validate(text):\n    return bool(_ISSUE_KEY.match(text))", 1)
	writeScopeFile(t, root, "relay/rules.py", inScope)
	if got := scopeViolations(t, CheckTaskStructure(root, recipeDir, task)); len(got) != 0 {
		t.Fatalf("a private constant only the scope uses is allowed, got violations %v", got)
	}

	// Out of scope: a public function changed, a new public function, and
	// a private helper an unscoped function uses.
	outOfScope := strings.Replace(inScope, "def render(text):\n    return text",
		"def _upper(text):\n    return text.upper()\n\n\ndef render(text):\n    return _upper(text)\n\n\ndef summary(text):\n    return text[:10]", 1)
	writeScopeFile(t, root, "relay/rules.py", outOfScope)
	got := scopeViolations(t, CheckTaskStructure(root, recipeDir, task))
	if want := []string{"_upper", "render", "summary"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("violations = %v, want %v", got, want)
	}

	// A class is in scope when the scope names one of its members.
	classTask := &state.Task{ID: "t-004", File: "relay/rules.py", Action: "modify", ModifyScope: []string{"refresh"}}
	writeScopeFile(t, root, "relay/rules.py", strings.Replace(rulesBefore, "return 1", "return 3", 1))
	if got := scopeViolations(t, CheckTaskStructure(root, recipeDir, classTask)); len(got) != 0 {
		t.Fatalf("a scoped method's class is in scope, got %v", got)
	}

	// The legacy placeholder scope is not checked.
	legacy := &state.Task{ID: "t-005", File: "relay/rules.py", Action: "modify", ModifyScope: []string{"legacy"}}
	writeScopeFile(t, root, "relay/rules.py", outOfScope)
	if got := scopeViolations(t, CheckTaskStructure(root, recipeDir, legacy)); len(got) != 0 {
		t.Fatalf("a legacy scope is skipped, got %v", got)
	}
}

// Two modify tasks on one file each own part of it: one's symbols are not
// outside the other's change.
func TestCheckTaskStructure_ScopeIsEveryModifyTaskOnTheFile(t *testing.T) {
	root, recipeDir := scopeRepo(t, "relay/rules.py", rulesBefore)
	tasks := `{"recipe_id":"r-047","tasks":[` +
		`{"id":"t-1","file":"relay/rules.py","action":"modify","status":"done","description":"x","modify_scope":["validate"]},` +
		`{"id":"t-2","file":"relay/rules.py","action":"modify","status":"done","description":"y","modify_scope":["render"]}]}`
	if err := os.WriteFile(filepath.Join(recipeDir, "tasks.json"), []byte(tasks), 0644); err != nil {
		t.Fatal(err)
	}
	writeScopeFile(t, root, "relay/rules.py", strings.Replace(rulesBefore, "return text\n", "return text.strip()\n", 1))
	task := &state.Task{ID: "t-1", File: "relay/rules.py", Action: "modify", ModifyScope: []string{"validate"}}
	if got := scopeViolations(t, CheckTaskStructure(root, recipeDir, task)); len(got) != 0 {
		t.Fatalf("render belongs to t-2's scope, got %v", got)
	}
}

// Go and TypeScript: exported names are public, methods match by type or
// method name, and a Go const group is read entry by entry.
func TestCheckTaskStructure_ScopeReportGoAndTypeScript(t *testing.T) {
	goBefore := "package relay\n\nimport \"strings\"\n\nconst (\n\tLimit = 10\n\tprefix = \"x\"\n)\n\n" +
		"type Server struct{}\n\nfunc (s *Server) Handle(in string) string {\n\treturn strings.TrimSpace(in)\n}\n\n" +
		"func Render(in string) string {\n\treturn in\n}\n"
	root, recipeDir := scopeRepo(t, "relay/relay.go", goBefore)
	task := &state.Task{ID: "t-1", File: "relay/relay.go", Action: "modify", ModifyScope: []string{"Handle"}}
	goAfter := strings.Replace(goBefore, "\tLimit = 10\n", "\tLimit = 20\n", 1)
	goAfter = strings.Replace(goAfter, "return strings.TrimSpace(in)", "return prefix + strings.TrimSpace(in)", 1)
	goAfter = strings.Replace(goAfter, "\tprefix = \"x\"\n", "\tprefix = \"y\"\n", 1)
	writeScopeFile(t, root, "relay/relay.go", goAfter)
	if got := scopeViolations(t, CheckTaskStructure(root, recipeDir, task)); strings.Join(got, ",") != "Limit" {
		t.Fatalf("Go: want only Limit reported (prefix is private and used by the scoped method), got %v", got)
	}

	tsBefore := "import { x } from \"./x\";\n\nconst SEP = \"/\";\n\nexport function join(a: string, b: string): string {\n  return a + SEP + b;\n}\n\n" +
		"export class Client {\n  get(path: string) {\n    return path;\n  }\n}\n\nfunction local() {\n  return x;\n}\n"
	root, recipeDir = scopeRepo(t, "src/client.ts", tsBefore)
	task = &state.Task{ID: "t-1", File: "src/client.ts", Action: "modify", ModifyScope: []string{"Client.get"}}
	tsAfter := strings.Replace(tsBefore, "return path;", "return encode(path);", 1)
	tsAfter = strings.Replace(tsAfter, "export class Client", "const encode = (p: string) => encodeURI(p);\n\nexport class Client", 1)
	tsAfter = strings.Replace(tsAfter, "return a + SEP + b;", "return [a, b].join(SEP);", 1)
	writeScopeFile(t, root, "src/client.ts", tsAfter)
	if got := scopeViolations(t, CheckTaskStructure(root, recipeDir, task)); strings.Join(got, ",") != "join" {
		t.Fatalf("TypeScript: want only join reported, got %v", got)
	}
}

// The line reader keeps a decorator with its function, a multi-line string
// at column 0 inside its function, and top-level code that declares
// nothing apart from the declarations around it.
func TestParseScopeDecls_PythonShapes(t *testing.T) {
	src := `"""Module doc."""
import re

@app.route("/x")
def handler():
    sql = """
SELECT 1
"""
    return sql


class _Cache:
    pass


if __name__ == "__main__":
    handler()

__all__ = ["handler"]
`
	var got []string
	for _, d := range parseScopeDecls(scopeLangs[".py"], src) {
		label := d.name
		if label == "" {
			label = "-"
		}
		if d.public {
			label += "+"
		}
		got = append(got, label)
	}
	if want := "- handler+ _Cache - __all__+"; strings.Join(got, " ") != want {
		t.Fatalf("blocks = %q, want %q", strings.Join(got, " "), want)
	}
	for _, d := range parseScopeDecls(scopeLangs[".py"], src) {
		if d.name == "handler" && (!strings.Contains(d.text, "@app.route") || !strings.Contains(d.text, "SELECT 1")) {
			t.Fatalf("the decorator and the SQL belong to handler: %q", d.text)
		}
	}
}
