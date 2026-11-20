package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/feature"
)

// runFeature runs `bts feature <args>` and returns what it printed to
// stdout, its error included.
func runFeature(t *testing.T, args ...string) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	rootCmd.SetArgs(append([]string{"feature"}, args...))
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

// bts feature stack-depth prints and sets the feature's depth, refuses
// anything but a whole number of at least 1, and status names a depth that
// is not the default.
func TestFeatureStackDepthCommand(t *testing.T) {
	t.Setenv(feature.StoreEnv, t.TempDir())
	dir, f, _, err := feature.Create(feature.CreateOptions{Title: "deep", Repo: &feature.Repo{Key: "local/deep"}, Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, feature.IntentFile), []byte("# Intent\n\nx\n"), 0644); err != nil {
		t.Fatal(err)
	}
	title, typ := "one", "build"
	if _, err := feature.AddItem(dir, feature.ItemSpec{Title: &title, Type: &typ}); err != nil {
		t.Fatal(err)
	}
	if _, err := feature.Confirm(dir, "go"); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())

	if out := runFeature(t, "stack-depth", f.ID); strings.TrimSpace(out) != "2" {
		t.Fatalf("the default depth: %q", out)
	}
	if out := runFeature(t, "status", f.ID); strings.Contains(out, "Stack depth") {
		t.Fatalf("status names the depth only when it is not the default:\n%s", out)
	}
	// A bare -1 is refused by cobra as an unknown flag; after -- it reaches
	// the command.
	for _, bad := range [][]string{{"0"}, {"--", "-1"}, {"three"}, {"2.5"}, {""}} {
		if out := runFeature(t, append([]string{"stack-depth", f.ID}, bad...)...); !strings.Contains(out, "whole number of at least 1") {
			t.Fatalf("stack-depth %q must be refused: %q", bad, out)
		}
	}
	if out := runFeature(t, "stack-depth", f.ID, "3"); out != "" {
		t.Fatalf("stack-depth 3: %q", out)
	}
	if out := runFeature(t, "stack-depth", f.ID); strings.TrimSpace(out) != "3" {
		t.Fatalf("the depth after setting 3: %q", out)
	}
	out := runFeature(t, "status", f.ID)
	if !strings.Contains(out, "Stack depth: at most 3 unmerged levels (default 2)") || strings.Contains(out, "Pending") {
		t.Fatalf("status names the depth and nothing is pending:\n%s", out)
	}
}
