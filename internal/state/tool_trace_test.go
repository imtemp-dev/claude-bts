package state

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func setupTraceRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".jig", "local"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return root
}

func TestToolTrace_AppendAndTail(t *testing.T) {
	root := setupTraceRoot(t)
	for i := 0; i < 5; i++ {
		e := &ToolTraceEntry{
			Phase:    "post",
			ToolName: "Read",
			File:     fmt.Sprintf("f%d.go", i),
		}
		if err := AppendToolTrace(root, e); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	got, err := TailToolTrace(root, 3)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 entries, got %d", len(got))
	}
	if got[0].File != "f2.go" || got[2].File != "f4.go" {
		t.Errorf("unexpected tail order: %v", got)
	}
	for _, e := range got {
		if e.Time == "" {
			t.Error("Time should be stamped on append")
		}
	}
}

func TestToolTrace_TailMissingFile(t *testing.T) {
	root := setupTraceRoot(t)
	got, err := TailToolTrace(root, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil tail, got %v", got)
	}
}

func TestToolTrace_TailZeroN(t *testing.T) {
	root := setupTraceRoot(t)
	_ = AppendToolTrace(root, &ToolTraceEntry{ToolName: "x"})
	got, err := TailToolTrace(root, 0)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got != nil {
		t.Errorf("n=0 should return nil, got %v", got)
	}
}

func TestToolTrace_Truncation(t *testing.T) {
	root := setupTraceRoot(t)
	// Append 2*max+5 to force rewrite
	total := ToolTraceMaxLines*2 + 5
	for i := 0; i < total; i++ {
		_ = AppendToolTrace(root, &ToolTraceEntry{ToolName: "T", File: fmt.Sprintf("%d", i)})
	}

	// File should now have at most ~max lines (not ~total)
	data, err := os.ReadFile(ToolTracePath(root))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := 0
	for _, c := range data {
		if c == '\n' {
			lines++
		}
	}
	// Truncation runs when file exceeds 2*max, so bound is 2*max.
	if lines > ToolTraceMaxLines*2 {
		t.Errorf("expected <=%d lines after truncation, got %d", ToolTraceMaxLines*2, lines)
	}
	if lines < ToolTraceMaxLines {
		t.Errorf("truncation should retain at least %d lines, got %d", ToolTraceMaxLines, lines)
	}

	// Tail should be the most recent files
	got, err := TailToolTrace(root, 3)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3, got %d", len(got))
	}
	if got[2].File != fmt.Sprintf("%d", total-1) {
		t.Errorf("last file should be %d, got %s", total-1, got[2].File)
	}
}

func TestToolTrace_SkipMalformedLine(t *testing.T) {
	root := setupTraceRoot(t)
	path := ToolTracePath(root)

	content := "{not-json\n" +
		`{"t":"2025-01-01T00:00:00Z","phase":"post","tool":"Read","file":"ok.go"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := TailToolTrace(root, 5)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 valid entry, got %d", len(got))
	}
	if got[0].File != "ok.go" {
		t.Errorf("unexpected: %v", got[0])
	}
}
