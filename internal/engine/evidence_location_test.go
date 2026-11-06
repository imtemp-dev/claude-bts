package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Trial problem #89: on all three trials of 11-06 the L4 probes behind
// Known Uncertainties ran from the session scratchpad
// (/private/tmp/claude-501/.../scratchpad/probes/), and on cool-crow the
// Opens-with named "the session scratchpad's socket probe directory" with
// no path at all.  Evidence a reviewer, a later session or the prove stage
// cannot rerun is not evidence; the skills told agents to put probes
// "outside the project".
func TestCheckEvidenceLocation(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".bts", "specs", "recipes", "r-089")
	_ = os.MkdirAll(dir, 0755)
	doc := filepath.Join(dir, "domain.md")
	_ = os.WriteFile(doc, []byte("## Known Uncertainties\n\n"+
		"### U-001: Does extract_text name the pages it covers?\n"+
		"Resolved [L4]: no.\n"+
		"Opens-with: `cd agent_server && uv run python /private/tmp/claude-501/-Users-x/d9bd3e9d/scratchpad/probe-q2q3/probe.py`\n\n"+
		"### U-002: Can activity be read without the lock?\n"+
		"Resolved [L4]: yes.\n"+
		"Opens-with: `.venv/bin/python -I probe.py probe.sqlite3` in the session scratchpad's SQLite probe directory\n\n"+
		"### U-003: Which staff are providers?\n"+
		"Resolved [L4]: the NPI.\n"+
		"Opens-with: `python3 .bts/specs/recipes/r-089/probes/providers.py` (from the repo root)\n\n"+
		"### U-004: The same, absolute but inside the project\n"+
		"Opens-with: `python3 "+filepath.Join(dir, "probes", "q4.py")+"`\n\n"+
		"### U-005: System paths a rerun finds too\n"+
		"Opens-with: `/usr/bin/env python3 probes/q5.py 2>/dev/null`\n"), 0644)

	issues := CheckEvidenceLocation(doc, root)
	var claims []string
	for _, is := range issues {
		claims = append(claims, is.Claim+" ["+is.Severity+"]")
	}
	got := strings.Join(claims, "; ")
	if len(issues) != 2 || !strings.Contains(got, "U-001") || !strings.Contains(got, "U-002") || !strings.Contains(got, "[major]") {
		t.Fatalf("U-001 (a scratchpad path) and U-002 (a scratchpad with no path) are majors; U-003 and U-004 live in the project: got %s", got)
	}
}
