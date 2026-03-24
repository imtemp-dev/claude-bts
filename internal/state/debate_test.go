package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupDebateRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".forge", "state", "debates"), 0755)
	return root
}

func TestSaveAndLoadDebateState(t *testing.T) {
	root := setupDebateRoot(t)
	debateID := "d-1000"
	os.MkdirAll(DebateDir(root, debateID), 0755)

	original := &DebateState{
		ID:         debateID,
		Topic:      "JWT vs session tokens",
		Rounds:     3,
		Conclusion: "Use JWT with refresh tokens",
		Decided:    true,
		StartedAt:  "2026-01-01T00:00:00Z",
	}

	if err := SaveDebateState(root, original); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := LoadDebateState(root, debateID)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if loaded.ID != original.ID {
		t.Errorf("ID: got %s, want %s", loaded.ID, original.ID)
	}
	if loaded.Topic != original.Topic {
		t.Errorf("Topic: got %s, want %s", loaded.Topic, original.Topic)
	}
	if loaded.Rounds != 3 {
		t.Errorf("Rounds: got %d, want 3", loaded.Rounds)
	}
	if !loaded.Decided {
		t.Error("Decided should be true")
	}
	if loaded.Conclusion != original.Conclusion {
		t.Errorf("Conclusion: got %s, want %s", loaded.Conclusion, original.Conclusion)
	}
	if loaded.UpdatedAt == "" {
		t.Error("UpdatedAt should be auto-set")
	}
}

func TestLoadDebateState_NotFound(t *testing.T) {
	root := setupDebateRoot(t)
	_, err := LoadDebateState(root, "d-nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListDebates(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		root := setupDebateRoot(t)
		debates, err := ListDebates(root)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(debates) != 0 {
			t.Errorf("got %d debates, want 0", len(debates))
		}
	})

	t.Run("multiple debates", func(t *testing.T) {
		root := setupDebateRoot(t)
		for _, id := range []string{"d-1000", "d-2000", "d-3000"} {
			dir := DebateDir(root, id)
			os.MkdirAll(dir, 0755)
			SaveDebateState(root, &DebateState{
				ID:        id,
				Topic:     "topic " + id,
				StartedAt: "2026-01-01T00:00:00Z",
			})
		}

		debates, err := ListDebates(root)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(debates) != 3 {
			t.Errorf("got %d debates, want 3", len(debates))
		}
	})

	t.Run("no debates dir", func(t *testing.T) {
		root := t.TempDir()
		os.MkdirAll(filepath.Join(root, ".forge", "state"), 0755)

		debates, err := ListDebates(root)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if debates != nil {
			t.Errorf("expected nil, got %v", debates)
		}
	})

	t.Run("skips corrupted debates", func(t *testing.T) {
		root := setupDebateRoot(t)

		// Good debate
		dir := DebateDir(root, "d-1000")
		os.MkdirAll(dir, 0755)
		SaveDebateState(root, &DebateState{ID: "d-1000", Topic: "good", StartedAt: "2026-01-01T00:00:00Z"})

		// Corrupted debate
		badDir := DebateDir(root, "d-bad")
		os.MkdirAll(badDir, 0755)
		os.WriteFile(filepath.Join(badDir, "debate.json"), []byte("{bad"), 0644)

		debates, err := ListDebates(root)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(debates) != 1 {
			t.Errorf("got %d debates, want 1", len(debates))
		}
	})
}

func TestNewDebateID(t *testing.T) {
	id := NewDebateID()
	if !strings.HasPrefix(id, "d-") {
		t.Errorf("got %s, want prefix d-", id)
	}
	if len(id) < 4 {
		t.Errorf("id too short: %s", id)
	}
}

func TestDebateDir(t *testing.T) {
	got := DebateDir("/project", "d-1000")
	want := filepath.Join("/project", ".forge", "state", "debates", "d-1000")
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
