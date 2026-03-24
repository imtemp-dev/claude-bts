package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendChangelog(t *testing.T) {
	root := t.TempDir()
	recipeID := "r-1000"
	os.MkdirAll(RecipeDir(root, recipeID), 0755)

	t.Run("auto-sets timestamp", func(t *testing.T) {
		entry := &ChangelogEntry{
			Action: "research",
			Output: "research.md",
		}
		if err := AppendChangelog(root, recipeID, entry); err != nil {
			t.Fatalf("append failed: %v", err)
		}
		if entry.Timestamp == "" {
			t.Error("Timestamp should be auto-set when empty")
		}
	})

	t.Run("preserves existing timestamp", func(t *testing.T) {
		entry := &ChangelogEntry{
			Timestamp: "2026-01-01T00:00:00Z",
			Action:    "draft",
		}
		if err := AppendChangelog(root, recipeID, entry); err != nil {
			t.Fatalf("append failed: %v", err)
		}
		if entry.Timestamp != "2026-01-01T00:00:00Z" {
			t.Errorf("Timestamp changed: got %s", entry.Timestamp)
		}
	})

	t.Run("accumulates entries", func(t *testing.T) {
		actions := []string{"improve", "verify", "assess"}
		for _, action := range actions {
			AppendChangelog(root, recipeID, &ChangelogEntry{Action: action})
		}

		data, _ := os.ReadFile(ChangelogPath(root, recipeID))
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		// 2 from previous sub-tests + 3 new
		if len(lines) != 5 {
			t.Errorf("got %d lines, want 5", len(lines))
		}

		// Verify last entry
		var last ChangelogEntry
		json.Unmarshal([]byte(lines[len(lines)-1]), &last)
		if last.Action != "assess" {
			t.Errorf("last action: got %s, want assess", last.Action)
		}
	})

	t.Run("with optional fields", func(t *testing.T) {
		entry := &ChangelogEntry{
			Action:       "verify",
			Input:        "draft.md",
			Output:       "verification.md",
			BasedOn:      []string{"draft.md"},
			Incorporates: []string{"debate-1.md"},
			Resolves:     []string{"gap-1.md"},
			Result:       "0 critical, 2 major",
			Level:        2.5,
		}
		if err := AppendChangelog(root, recipeID, entry); err != nil {
			t.Fatalf("append failed: %v", err)
		}

		// Read back last line
		data, _ := os.ReadFile(ChangelogPath(root, recipeID))
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		var last ChangelogEntry
		json.Unmarshal([]byte(lines[len(lines)-1]), &last)
		if last.Result != "0 critical, 2 major" {
			t.Errorf("Result: got %s", last.Result)
		}
		if last.Level != 2.5 {
			t.Errorf("Level: got %f, want 2.5", last.Level)
		}
	})
}

func TestChangelogPath(t *testing.T) {
	got := ChangelogPath("/project", "r-1000")
	want := filepath.Join("/project", ".forge", "state", "recipes", "r-1000", "changelog.jsonl")
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
