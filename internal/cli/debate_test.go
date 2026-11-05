package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/spf13/pflag"
)

func runDebateLog(t *testing.T, args ...string) {
	t.Helper()
	debateLogCmd.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
	saved := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w
	rootCmd.SetArgs(append([]string{"debate", "log"}, args...))
	err := rootCmd.Execute()
	_ = w.Close()
	os.Stdout = saved
	if err != nil {
		t.Fatalf("debate log %v: %v", args, err)
	}
}

// Trial problem #44: `bts debate log` could not record a conclusion or the
// decided flag and wrote started_at as "", so r-001 and r-003 both edited
// debate.json by hand.
func TestDebateLog_RecordsStartConclusionAndDecided(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".bts", "specs"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	runDebateLog(t, "--topic", "group set: required or optional", "--round", "1", "--content", "positions")
	debates, _ := state.ListDebates(root)
	if len(debates) != 1 {
		t.Fatalf("want one debate, got %d", len(debates))
	}
	d := debates[0]
	if d.StartedAt == "" || d.Decided || d.Conclusion != "" {
		t.Fatalf("a new debate must record its start and be open, got %+v", d)
	}

	runDebateLog(t, "--id", d.ID, "--round", "2", "--content", "synthesis",
		"--conclusion", "Required argument; revisit if a caller needs a default", "--decided")
	got, err := state.LoadDebateState(root, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Decided || got.Conclusion == "" || got.StartedAt != d.StartedAt || got.Rounds != 2 {
		t.Fatalf("the last round must record the conclusion and decided, keeping the start, got %+v", got)
	}
}
