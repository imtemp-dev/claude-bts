package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Trial problem #95: swift-eagle's wireframe named loop/work_item_thread.py,
// meaning agent_server/loop/work_item_thread.py in the monorepo, and `bts
// verify` reported a critical "Referenced file does not exist".  The false
// critical made the session rewrite every Depends On cell.  A path that
// matches exactly one tracked file by its tail is that file.
func TestCheckCodeReferences_ResolvesAModuleRelativePath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	for _, p := range []string{"agent_server/loop/work_item_thread.py", "a/x/util.py", "b/x/util.py"} {
		full := filepath.Join(root, p)
		_ = os.MkdirAll(filepath.Dir(full), 0755)
		_ = os.WriteFile(full, []byte("# code\n"), 0644)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}

	result := &VerifyResult{}
	checkCodeReferences("Depends on `loop/work_item_thread.py`, `x/util.py` and `nope/missing.py`.\n", root, "", result)

	got := map[string]string{}
	for _, is := range result.Issues {
		if is.Category == "file_ref" {
			got[is.Claim] = is.Severity
		}
	}
	if s, ok := got["loop/work_item_thread.py"]; ok && s != SeverityInfo {
		t.Errorf("a unique tail match is that file, not a %s", s)
	}
	if got["x/util.py"] != SeverityMajor {
		t.Errorf("a tail two files share is ambiguous: want major, got %q", got["x/util.py"])
	}
	if got["nope/missing.py"] != SeverityCritical {
		t.Errorf("a path nothing matches stays critical, got %q", got["nope/missing.py"])
	}
}
