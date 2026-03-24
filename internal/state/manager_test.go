package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindRoot(t *testing.T) {
	t.Run("found at cwd", func(t *testing.T) {
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, ".forge"), 0755)

		root, err := FindRoot(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if root != dir {
			t.Errorf("got %s, want %s", root, dir)
		}
	})

	t.Run("found in parent", func(t *testing.T) {
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, ".forge"), 0755)
		child := filepath.Join(dir, "a", "b", "c")
		os.MkdirAll(child, 0755)

		root, err := FindRoot(child)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if root != dir {
			t.Errorf("got %s, want %s", root, dir)
		}
	})

	t.Run("not found", func(t *testing.T) {
		dir := t.TempDir()
		_, err := FindRoot(dir)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), ".forge/ not found") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestStatePath(t *testing.T) {
	got := StatePath("/project")
	want := filepath.Join("/project", ".forge", "state")
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestReadJSON(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "data.json")
		os.WriteFile(path, []byte(`{"name":"test","value":42}`), 0644)

		var result struct {
			Name  string `json:"name"`
			Value int    `json:"value"`
		}
		if err := ReadJSON(path, &result); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Name != "test" || result.Value != 42 {
			t.Errorf("got %+v", result)
		}
	})

	t.Run("file not found", func(t *testing.T) {
		var result map[string]interface{}
		err := ReadJSON("/nonexistent/path.json", &result)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "bad.json")
		os.WriteFile(path, []byte(`{invalid`), 0644)

		var result map[string]interface{}
		err := ReadJSON(path, &result)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestWriteJSON(t *testing.T) {
	t.Run("success and roundtrip", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "out.json")

		data := map[string]string{"key": "value"}
		if err := WriteJSON(path, data); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var result map[string]string
		if err := ReadJSON(path, &result); err != nil {
			t.Fatalf("read back failed: %v", err)
		}
		if result["key"] != "value" {
			t.Errorf("got %v", result)
		}
	})

	t.Run("creates parent directories", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "a", "b", "c", "data.json")

		if err := WriteJSON(path, map[string]int{"n": 1}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("file not created: %v", err)
		}
	})

	t.Run("atomic write leaves no temp files", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "atomic.json")
		WriteJSON(path, map[string]int{"n": 1})

		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if strings.Contains(e.Name(), ".forge-") && strings.HasSuffix(e.Name(), ".tmp") {
				t.Errorf("temp file left behind: %s", e.Name())
			}
		}
	})

	t.Run("produces indented json", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "indent.json")
		WriteJSON(path, map[string]int{"n": 1})

		data, _ := os.ReadFile(path)
		if !strings.Contains(string(data), "\n") {
			t.Error("expected indented JSON output")
		}
	})
}

func TestAppendJSONL(t *testing.T) {
	t.Run("creates file and appends", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "log.jsonl")

		for i := 0; i < 3; i++ {
			if err := AppendJSONL(path, map[string]int{"i": i}); err != nil {
				t.Fatalf("append %d failed: %v", i, err)
			}
		}

		data, _ := os.ReadFile(path)
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) != 3 {
			t.Fatalf("got %d lines, want 3", len(lines))
		}
		for i, line := range lines {
			var m map[string]int
			if err := json.Unmarshal([]byte(line), &m); err != nil {
				t.Fatalf("line %d invalid JSON: %v", i, err)
			}
			if m["i"] != i {
				t.Errorf("line %d: got %d, want %d", i, m["i"], i)
			}
		}
	})

	t.Run("creates parent directories", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "sub", "dir", "log.jsonl")

		if err := AppendJSONL(path, map[string]int{"x": 1}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("file not created: %v", err)
		}
	})
}
