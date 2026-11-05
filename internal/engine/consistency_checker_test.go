package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// Trial problem #4: on r-003 and true-shark most domain-verify criticals were
// path-handling errors: a leading dot dropped (`.bts/...`), `~/` resolved
// under the project, a recipe-relative `research/v1.md` looked up from the
// root, and a backticked "(new)" path or a wireframe create row read as
// missing.  Only a really missing project file is critical.
func TestCheckCodeReferences_PathHandling(t *testing.T) {
	root := t.TempDir()
	docDir := filepath.Join(root, ".bts", "specs", "recipes", "r-001-x")
	for _, rel := range []string{".bts/specs/recipes/r-001-x/research/v1.md", "src/app.cs"} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0755)
		_ = os.WriteFile(p, []byte("x"), 0644)
	}
	_ = os.WriteFile(filepath.Join(docDir, "wireframe.md"), []byte("## File Structure\n\n| File | Action | Depends On | Responsibility |\n|---|---|---|---|\n| src/planned.cs | create | - | new |\n"), 0644)
	doc := "See `.bts/specs/recipes/r-001-x/research/v1.md` and `research/v1.md`.\n" +
		"Deploy to `~/Library/LaunchAgents/com.x.plist`.\n" +
		"Edit `src/app.cs`; add `src/new.cs` (new); the wireframe adds `src/planned.cs`.\n" +
		"And `src/missing.cs` is referenced.\n"
	result := &VerifyResult{}
	checkCodeReferences(doc, root, docDir, result)
	var critical []string
	for _, is := range result.Issues {
		if is.Category == "file_ref" && is.Severity == SeverityCritical {
			critical = append(critical, is.Claim)
		}
	}
	if len(critical) != 1 || critical[0] != "src/missing.cs" {
		t.Fatalf("only the really missing file may be critical, got %v", critical)
	}
}
