package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// app_proven trusted a drive's exit code and its evidence files, so a
// drive that exited 0 passed whatever it found (trial problem #29, Multica
// AGEN-67).  jongyoon chose a verdict with its checks: the drive writes
// each check and whether it passed, and a journey passes only with exit
// 0, its evidence, at least one check and no failed one.  The verdict's
// shape is the one true-shark's drives wrote unprompted.
func TestProveRun_PassesOnItsChecksNotItsExitCode(t *testing.T) {
	root, id := newBuildFixture(t)
	if _, err := executeProvePlan(root, id, []string{"zero-is-refused"}, "", ""); err != nil {
		t.Fatal(err)
	}
	const verdict = ".verify/z/verdict.json"
	drive := func(body string) string {
		return `mkdir -p .verify/z && echo ok > .verify/z/out.txt && printf '%s' '` + body + `' > ` + verdict
	}
	run := func(cmd string) *state.Journey {
		t.Helper()
		j, err := executeProveRun(root, id, "zero-is-refused", cmd, []string{".verify/z/out.txt"}, verdict, 30, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		return j
	}

	if _, err := executeProveRun(root, id, "zero-is-refused", "true", nil, "", 30, io.Discard); err == nil || !strings.Contains(err.Error(), "--verdict") {
		t.Fatalf("a drive with no verdict file named must be refused, got %v", err)
	}
	for _, c := range []struct{ name, cmd, want string }{
		{"a failed check", drive(`{"checks":[{"check":"allow(0) is refused","pass":false},{"check":"allow(1)","pass":true}]}`), "allow(0) is refused"},
		{"no checks", drive(`{"journey":"zero-is-refused","pass":true,"checks":[]}`), ""},
		{"a top-level fail", drive(`{"pass":false,"checks":[{"name":"allow(1)","passed":true}]}`), "verdict: pass=false"},
		{"a check with no pass value", drive(`{"checks":[{"check":"allow(0) is refused"}]}`), "no pass value"},
		{"not JSON", drive(`all good`), "not JSON"},
	} {
		j := run(c.cmd)
		if j.Passed() {
			t.Errorf("%s: an exit-0 drive must not pass: %+v", c.name, j)
		}
		if got := strings.Join(j.ChecksFailed, ";") + j.VerdictError; c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%s: want %q in the record, got %q", c.name, c.want, got)
		}
	}

	pass := drive(`{"journey":"zero-is-refused","pass":true,"checks":[{"check":"allow(0) is refused","pass":true,"status":400}]}`)
	if j := run(pass); !j.Passed() || j.Checks != 1 {
		t.Fatalf("exit 0, evidence, and one passing check pass: %+v", j)
	}

	// A drive that stops writing its verdict cannot pass on the last one.
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(filepath.Join(root, verdict), old, old)
	if j := run(`echo ok > .verify/z/out.txt`); j.Passed() || !strings.Contains(j.VerdictError, "not written by this run") {
		t.Fatalf("a verdict left from an earlier run must not count: %+v", j)
	}

	// A journey recorded before verdicts were read still passes on its exit
	// code and evidence until it is driven again.
	legacy := &state.Journey{Run: &state.CommandRun{Command: "x", ExitCode: 0}}
	if !legacy.Passed() {
		t.Fatal("a record with no verdict predates the rule and keeps its result")
	}
	rec, _ := state.LoadProveRecord(root, id)
	hash, _ := engine.CodeHash(root, id)
	if r := engine.CheckAppProven(rec, hash, id); r == "" {
		t.Fatal("the last drive's stale verdict fails, so the gate must not be met")
	}
}
