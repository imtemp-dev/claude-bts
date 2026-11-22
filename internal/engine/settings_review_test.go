package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// review.surface takes local, artifact or ask; anything else is the
// default, local.  An explicit artifact stays artifact.
func TestSettings_ReviewSurface(t *testing.T) {
	load := func(yaml string) string {
		t.Helper()
		root := t.TempDir()
		if yaml != "" {
			_ = os.MkdirAll(filepath.Join(root, ".bts", "config"), 0755)
			if err := os.WriteFile(filepath.Join(root, ".bts", "config", "settings.yaml"), []byte(yaml), 0644); err != nil {
				t.Fatal(err)
			}
		}
		s, err := LoadSettings(root)
		if err != nil {
			t.Fatal(err)
		}
		return s.Review.Surface
	}
	for yaml, want := range map[string]string{
		"":                               ReviewSurfaceLocal,
		"review:\n  surface: local\n":    ReviewSurfaceLocal,
		"review:\n  surface: Local\n":    ReviewSurfaceLocal,
		"review:\n  surface: artifact\n": ReviewSurfaceArtifact,
		"review:\n  surface: ask\n":      ReviewSurfaceAsk,
		"review:\n  surface: page\n":     ReviewSurfaceLocal,
	} {
		if got := load(yaml); got != want {
			t.Errorf("%q → %q, want %q", yaml, got, want)
		}
	}
}
