package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	const artifact = "review:\n  surface: artifact\n"
	root := editGoFixture(t, "r-ro1", "build", "discovery", artifact)
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

	off := editGoFixture(t, "r-ro2", "build", "discovery", "orca:\n  launch: off\n"+artifact)
	runReviewAttach(t, off, "r-ro2", "--url", "https://claude.ai/artifact/C", "--point", "intent")
	t.Setenv("ORCA_PANE_KEY", "")
	outside := editGoFixture(t, "r-ro3", "build", "discovery", artifact)
	runReviewAttach(t, outside, "r-ro3", "--url", "https://claude.ai/artifact/D", "--point", "intent")
	if len(tabs) != 2 {
		t.Fatalf("off, or outside Orca, opens nothing: %q", tabs)
	}
}

// With review.surface local (the default) a claude.ai link cannot be
// attached: the page is served by bts, and attach says how.
func TestReviewAttach_RefusedUnderTheLocalSurface(t *testing.T) {
	root := editGoFixture(t, "r-rl1", "build", "discovery", "")
	t.Chdir(root)
	reviewAttachCmd.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
	rootCmd.SetArgs([]string{"review", "attach", "r-rl1", "--url", "https://claude.ai/artifact/X", "--point", "intent"})
	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "bts review open r-rl1 --point intent") {
		t.Fatalf("attach under local: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".bts", "specs", "recipes", "r-rl1", "review.json")); statErr == nil {
		t.Error("a refused attach still wrote review.json")
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
