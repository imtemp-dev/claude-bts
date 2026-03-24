package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupRoadmapRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(StatePath(root), 0755)
	return root
}

func writeRoadmap(t *testing.T, root, content string) {
	t.Helper()
	path := filepath.Join(StatePath(root), "roadmap.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write roadmap: %v", err)
	}
}

func TestRoadmapProgress(t *testing.T) {
	t.Run("confirmed with items", func(t *testing.T) {
		root := setupRoadmapRoot(t)
		writeRoadmap(t, root, `# Roadmap
Status: CONFIRMED

- [x] Auth module (recipe: r-1000)
- [x] User API (recipe: r-2000)
- [ ] Dashboard (recipe: r-3000)
- [ ] Reports
- [-] Legacy cleanup
`)

		done, total, next := RoadmapProgress(root)
		if done != 2 {
			t.Errorf("done: got %d, want 2", done)
		}
		if total != 5 {
			t.Errorf("total: got %d, want 5", total)
		}
		if next != "Dashboard" {
			t.Errorf("next: got %q, want %q", next, "Dashboard")
		}
	})

	t.Run("draft status returns zeros", func(t *testing.T) {
		root := setupRoadmapRoot(t)
		writeRoadmap(t, root, `# Roadmap
Status: DRAFT

- [ ] Feature A
- [ ] Feature B
`)

		done, total, next := RoadmapProgress(root)
		if done != 0 || total != 0 || next != "" {
			t.Errorf("DRAFT should return (0,0,\"\"): got (%d,%d,%q)", done, total, next)
		}
	})

	t.Run("file not found returns zeros", func(t *testing.T) {
		root := t.TempDir()
		done, total, next := RoadmapProgress(root)
		if done != 0 || total != 0 || next != "" {
			t.Errorf("missing file should return (0,0,\"\"): got (%d,%d,%q)", done, total, next)
		}
	})

	t.Run("strips recipe annotation from next item", func(t *testing.T) {
		root := setupRoadmapRoot(t)
		writeRoadmap(t, root, `Status: CONFIRMED

- [ ] Build pipeline (recipe: r-5000)
`)

		_, _, next := RoadmapProgress(root)
		if next != "Build pipeline" {
			t.Errorf("got %q, want %q", next, "Build pipeline")
		}
	})

	t.Run("all done", func(t *testing.T) {
		root := setupRoadmapRoot(t)
		writeRoadmap(t, root, `Status: CONFIRMED

- [x] A
- [x] B
`)

		done, total, next := RoadmapProgress(root)
		if done != 2 || total != 2 {
			t.Errorf("got (%d,%d), want (2,2)", done, total)
		}
		if next != "" {
			t.Errorf("next should be empty when all done: got %q", next)
		}
	})
}

func TestMarkRoadmapItemDone(t *testing.T) {
	t.Run("marks matching item", func(t *testing.T) {
		root := setupRoadmapRoot(t)
		writeRoadmap(t, root, `# Roadmap
Status: CONFIRMED

- [x] Auth (recipe: r-1000)
- [ ] Dashboard (recipe: r-2000)
- [ ] Reports (recipe: r-3000)
`)

		MarkRoadmapItemDone(root, "r-2000")

		data, _ := os.ReadFile(filepath.Join(StatePath(root), "roadmap.md"))
		content := string(data)
		if !strings.Contains(content, "- [x] Dashboard (recipe: r-2000)") {
			t.Error("item should be marked done")
		}
		if !strings.Contains(content, "Progress: 2/3") {
			t.Errorf("Progress line not updated correctly in:\n%s", content)
		}
	})

	t.Run("no-op for missing file", func(t *testing.T) {
		root := t.TempDir()
		// Should not panic
		MarkRoadmapItemDone(root, "r-1000")
	})

	t.Run("no-op for non-matching id", func(t *testing.T) {
		root := setupRoadmapRoot(t)
		writeRoadmap(t, root, `Status: CONFIRMED

- [ ] Feature (recipe: r-1000)
`)

		MarkRoadmapItemDone(root, "r-9999")

		data, _ := os.ReadFile(filepath.Join(StatePath(root), "roadmap.md"))
		if strings.Contains(string(data), "[x]") {
			t.Error("no item should be marked")
		}
	})

	t.Run("inserts progress line after status", func(t *testing.T) {
		root := setupRoadmapRoot(t)
		writeRoadmap(t, root, `Status: CONFIRMED

- [ ] Only item (recipe: r-1000)
`)

		MarkRoadmapItemDone(root, "r-1000")

		data, _ := os.ReadFile(filepath.Join(StatePath(root), "roadmap.md"))
		content := string(data)
		if !strings.Contains(content, "Progress: 1/1") {
			t.Errorf("Progress line should be inserted:\n%s", content)
		}
	})
}

func TestVisionExists(t *testing.T) {
	root := setupRoadmapRoot(t)

	if VisionExists(root) {
		t.Error("should return false when no vision.md")
	}

	os.WriteFile(filepath.Join(StatePath(root), "vision.md"), []byte("# Vision"), 0644)
	if !VisionExists(root) {
		t.Error("should return true when vision.md exists")
	}
}

func TestRoadmapExists(t *testing.T) {
	root := setupRoadmapRoot(t)

	if RoadmapExists(root) {
		t.Error("should return false when no roadmap.md")
	}

	os.WriteFile(filepath.Join(StatePath(root), "roadmap.md"), []byte("# Roadmap"), 0644)
	if !RoadmapExists(root) {
		t.Error("should return true when roadmap.md exists")
	}
}
