package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Trial problem #92: after true-shark completed, `bts verify` read the
// deleted WorkItemNumber.cs as "File to be created", because the wireframe
// still listed it as a create row.  In a completed recipe a planned file
// that is gone is drift.
func TestCheckCodeReferences_PlannedFileGoneAfterCompletion(t *testing.T) {
	for phase, want := range map[string]string{"complete": SeverityMajor, "falsify": SeverityInfo} {
		root := t.TempDir()
		dir := filepath.Join(root, ".bts", "specs", "recipes", "r-092")
		_ = os.MkdirAll(dir, 0755)
		_ = os.WriteFile(filepath.Join(dir, "recipe.json"), []byte(`{"id":"r-092","type":"build","phase":"`+phase+`"}`), 0644)
		_ = os.WriteFile(filepath.Join(dir, "wireframe.md"), []byte("## File Structure\n\n| File | Action | Depends On | Responsibility |\n|---|---|---|---|\n"+
			"| `src/WorkItemNumber.cs` | create | — | the number |\n"), 0644)
		result := &VerifyResult{}
		checkCodeReferences("The number lives in `src/WorkItemNumber.cs`.\n", root, dir, result)
		var got []string
		for _, is := range result.Issues {
			if is.Category == "file_ref" && is.Claim == "src/WorkItemNumber.cs" {
				got = append(got, is.Severity+": "+is.Detail)
			}
		}
		if len(got) != 1 || !strings.HasPrefix(got[0], want) {
			t.Errorf("phase %s: want one %s finding, got %v", phase, want, got)
		}
	}
}
