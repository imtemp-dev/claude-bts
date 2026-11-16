package feature

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// bts #147: the GitHub CLI runs under a deadline.
func TestGHRunTimesOut(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\nexec sleep 10\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	prev := ghTimeout
	ghTimeout = 300 * time.Millisecond
	t.Cleanup(func() { ghTimeout = prev })
	start := time.Now()
	_, err := runGH("", "", "pr", "view", "x")
	if err == nil || !strings.Contains(err.Error(), "no answer within") || time.Since(start) > 5*time.Second {
		t.Fatalf("gh must be stopped at its deadline: %v after %s", err, time.Since(start))
	}
}
