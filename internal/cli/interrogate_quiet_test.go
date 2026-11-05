package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// `bts interrogate verify --recheck` printed every repro's full output
// (pages of build warnings on true-shark) before the findings summary,
// which is the result (trial problem #87, Multica AGEN-72).  A run's
// output goes to its log; the terminal gets one line per finding.
func TestInterrogateRecheck_KeepsRunOutputInTheLog(t *testing.T) {
	root, id, run := newInterrogateFixture(t)
	a1, a2 := run.Adversaries[0], run.Adversaries[1]
	noisy := "for i in 1 2 3 4 5 6 7 8 9 10; do echo 'warning CS1591: BUILD-NOISE'; done; " + strings.TrimSpace(reproScript)
	_ = os.MkdirAll(filepath.Join(a1.Copy, "repro"), 0755)
	_ = os.WriteFile(filepath.Join(a1.Copy, "repro", "zero.sh"), []byte(noisy+"\n"), 0755)
	writeReport(t, root, id, a1, []map[string]any{
		{"title": "allow(0) is true", "command": "sh repro/zero.sh", "repro_files": []string{"repro/zero.sh"}},
	})
	writeReport(t, root, id, a2, []map[string]any{})
	if _, err := executeInterrogateVerify(root, id, 30, io.Discard); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if _, err := executeInterrogateRecheck(root, id, 30, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "BUILD-NOISE") {
		t.Fatalf("the run's output belongs in its log, not the terminal:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "F-001") || !strings.Contains(out.String(), "exit=") {
		t.Fatalf("the terminal names each finding and its exit code:\n%s", out.String())
	}
	logPath := filepath.Join(state.LocalPath(root), "recipes", id, "interrogate-F-001.log")
	if data, err := os.ReadFile(logPath); err != nil || !strings.Contains(string(data), "BUILD-NOISE") {
		t.Fatalf("the full output is kept in %s: %v", logPath, err)
	}
}
