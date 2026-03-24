package state

import (
	"os"
	"path/filepath"
	"testing"
)

func setupManifestRoot(t *testing.T, recipeID string) string {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(RecipeDir(root, recipeID), 0755)
	return root
}

func TestLoadManifest_NotFound(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".forge", "state", "recipes", "r-1000"), 0755)

	m, err := LoadManifest(root, "r-1000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m == nil {
		t.Fatal("expected empty manifest, got nil")
	}
	if m.Documents == nil {
		t.Error("Documents map should be initialized")
	}
	if len(m.Documents) != 0 {
		t.Errorf("got %d documents, want 0", len(m.Documents))
	}
}

func TestSaveAndLoadManifest(t *testing.T) {
	recipeID := "r-1000"
	root := setupManifestRoot(t, recipeID)

	m := &Manifest{
		CurrentDraft: "draft.md",
		Level:        2.5,
		Documents: map[string]DocumentEntry{
			"draft.md": {Type: "draft", CreatedAt: "2026-01-01T00:00:00Z"},
			"research.md": {
				Type:      "research",
				CreatedAt: "2026-01-01T00:00:00Z",
				BasedOn:   []string{"source.md"},
			},
		},
	}

	if err := SaveManifest(root, recipeID, m); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := LoadManifest(root, recipeID)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded.CurrentDraft != "draft.md" {
		t.Errorf("CurrentDraft: got %s, want draft.md", loaded.CurrentDraft)
	}
	if loaded.Level != 2.5 {
		t.Errorf("Level: got %f, want 2.5", loaded.Level)
	}
	if len(loaded.Documents) != 2 {
		t.Errorf("got %d documents, want 2", len(loaded.Documents))
	}
}

func TestManifest_AddDocument(t *testing.T) {
	t.Run("new document", func(t *testing.T) {
		m := &Manifest{Documents: make(map[string]DocumentEntry)}
		m.AddDocument("draft.md", "draft", []string{"research.md"})

		entry, ok := m.Documents["draft.md"]
		if !ok {
			t.Fatal("document not added")
		}
		if entry.Type != "draft" {
			t.Errorf("Type: got %s, want draft", entry.Type)
		}
		if len(entry.BasedOn) != 1 || entry.BasedOn[0] != "research.md" {
			t.Errorf("BasedOn: got %v", entry.BasedOn)
		}
		if entry.CreatedAt == "" {
			t.Error("CreatedAt should be auto-set")
		}
	})

	t.Run("upsert preserves relationships", func(t *testing.T) {
		m := &Manifest{Documents: make(map[string]DocumentEntry)}
		m.AddDocument("draft.md", "draft", []string{"research.md"})

		// Set relationship fields
		m.SetIncorporates("draft.md", []string{"debate-1.md"})
		m.SetVerifiedBy("draft.md", "verify-1.md")

		// Upsert with new basedOn
		m.AddDocument("draft.md", "draft", []string{"research-v2.md"})

		entry := m.Documents["draft.md"]
		// basedOn should be updated
		if len(entry.BasedOn) != 1 || entry.BasedOn[0] != "research-v2.md" {
			t.Errorf("BasedOn should be updated: got %v", entry.BasedOn)
		}
		// Incorporates should be preserved
		if len(entry.Incorporates) != 1 || entry.Incorporates[0] != "debate-1.md" {
			t.Errorf("Incorporates should be preserved: got %v", entry.Incorporates)
		}
		// VerifiedBy should be preserved
		if entry.VerifiedBy != "verify-1.md" {
			t.Errorf("VerifiedBy should be preserved: got %s", entry.VerifiedBy)
		}
	})

	t.Run("upsert with empty basedOn preserves existing", func(t *testing.T) {
		m := &Manifest{Documents: make(map[string]DocumentEntry)}
		m.AddDocument("draft.md", "draft", []string{"research.md"})
		m.AddDocument("draft.md", "draft", nil) // empty basedOn

		entry := m.Documents["draft.md"]
		if len(entry.BasedOn) != 1 || entry.BasedOn[0] != "research.md" {
			t.Errorf("BasedOn should be preserved when empty: got %v", entry.BasedOn)
		}
	})

	t.Run("nil documents map", func(t *testing.T) {
		m := &Manifest{}
		m.AddDocument("doc.md", "research", nil)
		if m.Documents == nil {
			t.Fatal("Documents should be initialized")
		}
		if _, ok := m.Documents["doc.md"]; !ok {
			t.Error("document not added")
		}
	})
}

func TestManifest_SetIncorporates(t *testing.T) {
	m := &Manifest{Documents: map[string]DocumentEntry{
		"draft.md": {Type: "draft"},
	}}

	m.SetIncorporates("draft.md", []string{"debate-1.md", "debate-2.md"})
	entry := m.Documents["draft.md"]
	if len(entry.Incorporates) != 2 {
		t.Errorf("got %d incorporates, want 2", len(entry.Incorporates))
	}

	// Non-existent document: silent no-op
	m.SetIncorporates("missing.md", []string{"x"})
	if _, ok := m.Documents["missing.md"]; ok {
		t.Error("should not create entry for missing document")
	}
}

func TestManifest_SetVerifiedBy(t *testing.T) {
	m := &Manifest{Documents: map[string]DocumentEntry{
		"draft.md": {Type: "draft"},
	}}

	m.SetVerifiedBy("draft.md", "verification.md")
	if m.Documents["draft.md"].VerifiedBy != "verification.md" {
		t.Errorf("VerifiedBy not set")
	}

	// Non-existent document: silent no-op
	m.SetVerifiedBy("missing.md", "verify.md")
	if _, ok := m.Documents["missing.md"]; ok {
		t.Error("should not create entry for missing document")
	}
}

func TestManifest_GetUnverifiedDrafts(t *testing.T) {
	m := &Manifest{Documents: map[string]DocumentEntry{
		"draft.md":    {Type: "draft", VerifiedBy: ""},
		"draft-2.md":  {Type: "draft", VerifiedBy: "verify.md"},
		"research.md": {Type: "research", VerifiedBy: ""},
	}}

	unverified := m.GetUnverifiedDrafts()
	if len(unverified) != 1 {
		t.Fatalf("got %d, want 1", len(unverified))
	}
	if unverified[0] != "draft.md" {
		t.Errorf("got %s, want draft.md", unverified[0])
	}
}

func TestManifest_GetUnresolvedGaps(t *testing.T) {
	m := &Manifest{Documents: map[string]DocumentEntry{
		"sim-1.md": {Type: "simulation"},
		"sim-2.md": {Type: "simulation"},
		"draft.md": {Type: "draft", Resolves: []string{"sim-1.md"}},
	}}

	unresolved := m.GetUnresolvedGaps()
	if len(unresolved) != 1 {
		t.Fatalf("got %d, want 1", len(unresolved))
	}
	if unresolved[0] != "sim-2.md" {
		t.Errorf("got %s, want sim-2.md", unresolved[0])
	}
}

func TestManifest_GetUnincorporatedDebates(t *testing.T) {
	m := &Manifest{
		CurrentDraft: "draft.md",
		Documents: map[string]DocumentEntry{
			"draft.md":    {Type: "draft", Incorporates: []string{"debate-1.md"}},
			"debate-1.md": {Type: "debate"},
			"debate-2.md": {Type: "debate"},
		},
	}

	missing := m.GetUnincorporatedDebates()
	if len(missing) != 1 {
		t.Fatalf("got %d, want 1", len(missing))
	}
	if missing[0] != "debate-2.md" {
		t.Errorf("got %s, want debate-2.md", missing[0])
	}
}
