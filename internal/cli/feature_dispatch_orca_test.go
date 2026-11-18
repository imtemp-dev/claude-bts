package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/feature"
	"github.com/spf13/pflag"
)

// runFeatureDispatch runs `bts feature dispatch` in root with fresh flags
// and returns what it printed, its error included.
func runFeatureDispatch(t *testing.T, root string, args ...string) string {
	t.Helper()
	t.Chdir(root)
	featureDispatchCmd.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	rootCmd.SetArgs(append([]string{"feature", "dispatch"}, args...))
	runErr := rootCmd.Execute()
	_ = w.Close()
	os.Stdout = orig
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, rerr := r.Read(buf)
		sb.Write(buf[:n])
		if rerr != nil {
			break
		}
	}
	if runErr != nil {
		sb.WriteString(runErr.Error())
	}
	return sb.String()
}

func confirmedOrcaFeature(t *testing.T) (dir, id, item string) {
	t.Helper()
	t.Setenv(feature.StoreEnv, t.TempDir())
	dir, f, _, err := feature.Create(feature.CreateOptions{Title: "orca flow", Repo: &feature.Repo{Key: "local/orca-1"}, Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, feature.IntentFile), []byte("# Intent\n\nOne thing.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	title, typ := "one", "build"
	item, err = feature.AddItem(dir, feature.ItemSpec{Title: &title, Type: &typ})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := feature.Confirm(dir, "go"); err != nil {
		t.Fatal(err)
	}
	return dir, f.ID, item
}

// orca.launch ask shows the workers that would start and the --yes
// command, starting nothing; off refuses; and a real start outside an Orca
// terminal is refused, because nothing would ever wake the session.
func TestFeatureDispatchViaOrca_Policy(t *testing.T) {
	calls := fakeOrca(t)
	_, fid, item := confirmedOrcaFeature(t)
	t.Setenv("ORCA_PANE_KEY", "")
	t.Setenv("ORCA_AGENT_PANE", "")

	ask := editGoFixture(t, "r-fd1", "build", "draft", "")
	out := runFeatureDispatch(t, ask, fid, "--via", "orca")
	if len(*calls) != 0 || !strings.Contains(out, item+": would be started on main") ||
		!strings.Contains(out, "bts feature dispatch "+fid+" --via orca --yes") {
		t.Fatalf("ask must show the plan and the --yes command and start nothing, got %q, calls %q", out, *calls)
	}

	if out := runFeatureDispatch(t, ask, fid, "--via", "orca", "--yes"); !strings.Contains(out, "Orca terminal") || len(*calls) != 0 {
		t.Fatalf("outside Orca a real start must be refused, got %q, calls %q", out, *calls)
	}

	off := editGoFixture(t, "r-fd2", "build", "draft", "orca:\n  launch: off\n")
	if out := runFeatureDispatch(t, off, fid, "--via", "orca", "--yes"); !strings.Contains(out, "orca.launch is off") {
		t.Fatalf("off must refuse, got %q", out)
	}

	if out := runFeatureDispatch(t, ask, fid, "--via", "jira"); !strings.Contains(out, "--via takes multica or orca") {
		t.Fatalf("an unknown backend must be refused, got %q", out)
	}
}
