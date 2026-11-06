package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Trial problem #110: a clean document's `bts verify` printed
// `"issues": null`, and neat-wolf's reader looping over d['issues'] failed
// twice in 25 minutes.  An empty list is `[]`.
func TestVerifyDocument_CleanDocumentListsNoIssuesAsAnEmptyArray(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "clean.md")
	_ = os.WriteFile(p, []byte("# Doc\n\nNothing to check here.\n"), 0644)
	result, err := VerifyDocument(p, "", true)
	if err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, `"issues":null`) || !strings.Contains(s, `"issues":[]`) {
		t.Errorf("issues must be [], got %s", s)
	}
	if strings.Contains(s, `"missing":null`) {
		t.Errorf("level.missing must be [] when empty, got %s", s)
	}
}
