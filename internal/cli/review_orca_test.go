package cli

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/orca"
	"github.com/spf13/pflag"
)

func runReviewAttach(t *testing.T, root string, args ...string) {
	t.Helper()
	t.Chdir(root)
	reviewAttachCmd.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	rootCmd.SetArgs(append([]string{"review", "attach"}, args...))
	err := rootCmd.Execute()
	_ = w.Close()
	os.Stdout = orig
	_, _ = io.Copy(io.Discard, r)
	if err != nil {
		t.Fatal(err)
	}
}

// A newly attached review page opens in Orca's browser beside the
// terminal when the session runs in Orca; attaching the same link again
// opens nothing; a new link opens again; outside Orca, or with orca.launch
// off, nothing opens.
func TestReviewAttach_OpensInOrca(t *testing.T) {
	var tabs [][]string
	t.Cleanup(orca.SetRunner(func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "tab" {
			tabs = append(tabs, args)
		}
		return []byte(`{"ok":true,"result":{"browserPageId":"p1"}}`), nil
	}))

	t.Setenv("ORCA_AGENT_PANE", "")
	t.Setenv("ORCA_PANE_KEY", "pk:1")
	root := editGoFixture(t, "r-ro1", "build", "discovery", "")
	runReviewAttach(t, root, "r-ro1", "--url", "https://claude.ai/artifact/A", "--point", "intent")
	if len(tabs) != 1 || flag(tabs[0], "--url") != "https://claude.ai/artifact/A" || flag(tabs[0], "--split") != "right" {
		t.Fatalf("the first attach opens one tab beside the terminal: %q", tabs)
	}
	runReviewAttach(t, root, "r-ro1", "--url", "https://claude.ai/artifact/A", "--point", "scope")
	if len(tabs) != 1 {
		t.Fatalf("the same link is not opened again: %q", tabs)
	}
	runReviewAttach(t, root, "r-ro1", "--url", "https://claude.ai/artifact/B", "--point", "scope")
	if len(tabs) != 2 {
		t.Fatalf("a new link opens again: %q", tabs)
	}

	off := editGoFixture(t, "r-ro2", "build", "discovery", "orca:\n  launch: off\n")
	runReviewAttach(t, off, "r-ro2", "--url", "https://claude.ai/artifact/C", "--point", "intent")
	t.Setenv("ORCA_PANE_KEY", "")
	outside := editGoFixture(t, "r-ro3", "build", "discovery", "")
	runReviewAttach(t, outside, "r-ro3", "--url", "https://claude.ai/artifact/D", "--point", "intent")
	if len(tabs) != 2 {
		t.Fatalf("off, or outside Orca, opens nothing: %q", tabs)
	}
}

// flag is the value after name in args.
func flag(args []string, name string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == name {
			return args[i+1]
		}
	}
	return ""
}
