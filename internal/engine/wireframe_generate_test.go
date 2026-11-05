package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// testGenerateWireframe has two hand-written rows and two `generate` rows,
// one of which names no generator (trial problem #66).
const testGenerateWireframe = "## File Structure\n\n| File | Action | Depends On | Responsibility |\n|---|---|---|---|\n" +
	"| `api/WorkController.cs` | modify | - | the list endpoint |\n" +
	"| `openapi/api-v1.json` | generate | WorkController.cs | the API contract (generator: `just openapi \\| tee .verify/openapi.log`) |\n" +
	"| `ui/types/Work.ts` | generate | openapi/api-v1.json | the client types |\n" +
	"| `api/Tests/WorkTests.cs` | create | - | the tests |\n"

// Trial problem #66: generated files had no action, so true-shark wrote
// `generated`, which passed as a non-task row and recorded nothing.  A
// `generate` row is still not a task, but it must carry its generator, and
// `bts verify wireframe.md` says so at design time.
func TestVerifyDocument_GenerateRowNeedsItsGenerator(t *testing.T) {
	dir := t.TempDir()
	wf := filepath.Join(dir, "wireframe.md")
	_ = os.WriteFile(wf, []byte(testGenerateWireframe), 0644)
	result, err := VerifyDocument(wf, "", true)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	var found []string
	for _, is := range result.Issues {
		if strings.Contains(is.Claim, "generate_row_without_generator") {
			found = append(found, is.Claim+" "+is.Severity)
		}
	}
	if len(found) != 1 || !strings.Contains(found[0], "ui/types/Work.ts") || !strings.Contains(found[0], "major") {
		t.Fatalf("the generate row with no generator must be one major finding, got %v", found)
	}
	for _, k := range ParseWireframeFileTable(testGenerateWireframe) {
		if k.Action == "generate" {
			t.Fatalf("a generate row is never a task anchor: %v", k)
		}
	}
}

// A generator may write a file that does not exist yet; a reference to it
// is planned, like a create row's.
func TestCheckCodeReferences_GenerateRowIsPlanned(t *testing.T) {
	root := t.TempDir()
	docDir := filepath.Join(root, ".bts", "specs", "recipes", "r-001-x")
	_ = os.MkdirAll(docDir, 0755)
	_ = os.WriteFile(filepath.Join(docDir, "wireframe.md"), []byte(testGenerateWireframe), 0644)
	result := &VerifyResult{}
	checkCodeReferences("The generator writes `ui/types/Work.ts`.\n", root, docDir, result)
	for _, is := range result.Issues {
		if is.Category == "file_ref" && is.Severity == SeverityCritical {
			t.Fatalf("a generate row's file is planned, not missing: %v", is)
		}
	}
}

func TestParseWireframeGenerateRows(t *testing.T) {
	rows := ParseWireframeGenerateRows(testGenerateWireframe)
	if len(rows) != 2 {
		t.Fatalf("want 2 generate rows, got %+v", rows)
	}
	// An escaped pipe in the cell is the command's own pipe.
	if rows[0].Path != "openapi/api-v1.json" || rows[0].Command != "just openapi | tee .verify/openapi.log" {
		t.Fatalf("first row: %+v", rows[0])
	}
	if rows[1].Path != "ui/types/Work.ts" || rows[1].Command != "" {
		t.Fatalf("second row names no generator: %+v", rows[1])
	}
}

func TestGeneratedFiles_DirectoryAndAbsent(t *testing.T) {
	root := t.TempDir()
	per, all, err := GeneratedFiles(root, []string{"gen/", "gen/api.json"})
	if err != nil || per["gen/"] != "absent" || per["gen/api.json"] != "absent" {
		t.Fatalf("missing paths are absent: %v %v", per, err)
	}
	_ = os.MkdirAll(filepath.Join(root, "gen", "types"), 0755)
	_ = os.WriteFile(filepath.Join(root, "gen", "api.json"), []byte("{}"), 0644)
	_ = os.WriteFile(filepath.Join(root, "gen", "types", "a.ts"), []byte("type A = 1"), 0644)
	per2, all2, _ := GeneratedFiles(root, []string{"gen/", "gen/api.json"})
	if per2["gen/"] == "absent" || all2 == all {
		t.Fatalf("a directory row stands for the files under it: %v", per2)
	}
	_ = os.WriteFile(filepath.Join(root, "gen", "types", "a.ts"), []byte("type A = 2"), 0644)
	per3, all3, _ := GeneratedFiles(root, []string{"gen/", "gen/api.json"})
	if per3["gen/"] == per2["gen/"] || per3["gen/api.json"] != per2["gen/api.json"] || all3 == all2 {
		t.Fatal("a file changed under a directory row changes that row's hash only")
	}
}

func TestCheckGeneratedCurrent(t *testing.T) {
	rows := []GenerateRow{{Path: "gen/api.json", Command: "make api"}, {Path: "gen/types.ts", Command: "make api"}}
	ok := func() *state.GeneratedRun {
		return &state.GeneratedRun{
			Runs:      []*state.CommandRun{{Command: "make api", ExitCode: 0}},
			Files:     []string{"gen/types.ts", "gen/api.json"},
			FilesHash: "f1", CodeHash: "c1",
		}
	}
	if got := CheckGeneratedCurrent(nil, nil, "c1", "f1", "r-1"); got != "" {
		t.Fatalf("no generate rows, no gate: %q", got)
	}
	cases := []struct {
		name string
		g    *state.GeneratedRun
		rows []GenerateRow
		code string
		file string
		want string // substring; "" means pass
	}{
		{"current", ok(), rows, "c1", "f1", ""},
		{"nothing recorded", nil, rows, "c1", "f1", "no generated check is recorded"},
		{"generator failed", func() *state.GeneratedRun { g := ok(); g.Runs[0].ExitCode = 2; return g }(), rows, "c1", "f1", `"make api" exited 2`},
		{"file not written", func() *state.GeneratedRun { g := ok(); g.Missing = []string{"gen/types.ts"}; return g }(), rows, "c1", "f1", "not written: gen/types.ts"},
		{"rows changed", ok(), append(rows, GenerateRow{Path: "gen/more.ts", Command: "make more"}), "c1", "f1", "rows changed after"},
		{"code changed", ok(), rows, "c2", "f1", "code changed after the generators"},
		{"hand edit", ok(), rows, "c1", "f2", "never edited by hand"},
		{"row without generator", ok(), []GenerateRow{{Path: "gen/api.json"}}, "c1", "f1", "name no generator"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CheckGeneratedCurrent(c.g, c.rows, c.code, c.file, "r-1")
			if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}
