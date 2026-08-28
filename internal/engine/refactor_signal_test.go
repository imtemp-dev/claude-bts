package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeChangelog(t *testing.T, dir string, entries []map[string]string) {
	t.Helper()
	path := filepath.Join(dir, "changelog.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
}

// test → 3 implements across different modules → test_fix_cascade
func TestDetectRefactorSignals_TestFixCascade(t *testing.T) {
	dir := t.TempDir()
	writeChangelog(t, dir, []map[string]string{
		{"time": "t0", "action": "implement", "output": "pkg/a/x.go"},
		{"time": "t1", "action": "test", "result": "fail"},
		{"time": "t2", "action": "implement", "output": "pkg/a/x.go"},
		{"time": "t3", "action": "implement", "output": "pkg/b/y.go"},
		{"time": "t4", "action": "implement", "output": "pkg/c/z.go"},
	})

	signals, err := DetectRefactorSignals(dir)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if len(signals) == 0 {
		t.Fatal("expected test_fix_cascade signal")
	}
	found := false
	for _, s := range signals {
		if s.Kind == "test_fix_cascade" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected test_fix_cascade, got %+v", signals)
	}
}

// Churn: single module with 4+ implements → cross_module_churn
func TestDetectRefactorSignals_SingleModuleChurn(t *testing.T) {
	dir := t.TempDir()
	writeChangelog(t, dir, []map[string]string{
		{"time": "t0", "action": "implement", "output": "pkg/hot/a.go"},
		{"time": "t1", "action": "implement", "output": "pkg/hot/b.go"},
		{"time": "t2", "action": "implement", "output": "pkg/hot/c.go"},
		{"time": "t3", "action": "implement", "output": "pkg/hot/d.go"},
	})

	signals, err := DetectRefactorSignals(dir)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	found := false
	for _, s := range signals {
		if s.Kind == "cross_module_churn" {
			found = true
			if !strings.Contains(strings.Join(s.Evidence, " "), "pkg/hot") {
				t.Errorf("evidence should mention pkg/hot, got %v", s.Evidence)
			}
		}
	}
	if !found {
		t.Fatalf("expected cross_module_churn, got %+v", signals)
	}
}

// Clean recipe: one module with 2 implements — no signals.
func TestDetectRefactorSignals_Clean(t *testing.T) {
	dir := t.TempDir()
	writeChangelog(t, dir, []map[string]string{
		{"time": "t0", "action": "implement", "output": "pkg/a/x.go"},
		{"time": "t1", "action": "implement", "output": "pkg/a/y.go"},
		{"time": "t2", "action": "test", "result": "pass"},
	})
	signals, err := DetectRefactorSignals(dir)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if len(signals) != 0 {
		t.Fatalf("expected no signals, got %+v", signals)
	}
}

func TestModulePrefix(t *testing.T) {
	cases := map[string]string{
		"pkg/auth/handler.go":     "pkg/auth",
		"src/components/Card.tsx": "src/components",
		"main.go":                 "main.go",
		"./cmd/jig/main.go":       "cmd/jig",
		"":                        "",
	}
	for in, want := range cases {
		if got := modulePrefix(in); got != want {
			t.Errorf("modulePrefix(%q) = %q, want %q", in, got, want)
		}
	}
}
