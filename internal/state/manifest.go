package state

import (
	"path/filepath"
	"time"
)

// Manifest tracks all documents and their relationships within a recipe.
type Manifest struct {
	CurrentDraft string                    `json:"current_draft"`
	Level        float64                   `json:"level"`
	Documents    map[string]DocumentEntry  `json:"documents"`
}

// DocumentEntry tracks one document's metadata and relationships.
type DocumentEntry struct {
	Type         string   `json:"type"`                    // research, draft, debate, simulation, verification
	CreatedAt    string   `json:"created_at"`
	BasedOn      []string `json:"based_on,omitempty"`      // parent documents
	Incorporates []string `json:"incorporates,omitempty"`  // debates/simulations incorporated
	Resolves     []string `json:"resolves,omitempty"`      // gaps resolved
	VerifiedBy   string   `json:"verified_by,omitempty"`   // verification doc path
}

// ManifestPath returns the manifest file path for a recipe.
func ManifestPath(btsRoot, recipeID string) string {
	return filepath.Join(RecipeDir(btsRoot, recipeID), "manifest.json")
}

// LoadManifest reads the manifest file.
func LoadManifest(btsRoot, recipeID string) (*Manifest, error) {
	path := ManifestPath(btsRoot, recipeID)
	var m Manifest
	if err := ReadJSON(path, &m); err != nil {
		// Return empty manifest if not found
		return &Manifest{
			Documents: make(map[string]DocumentEntry),
		}, nil
	}
	return &m, nil
}

// SaveManifest writes the manifest file atomically.
func SaveManifest(btsRoot, recipeID string, m *Manifest) error {
	path := ManifestPath(btsRoot, recipeID)
	return WriteJSON(path, m)
}

// AddDocument registers a new document in the manifest.
func (m *Manifest) AddDocument(path string, docType string, basedOn []string) {
	if m.Documents == nil {
		m.Documents = make(map[string]DocumentEntry)
	}
	m.Documents[path] = DocumentEntry{
		Type:      docType,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		BasedOn:   basedOn,
	}
}

// SetIncorporates marks that a document incorporates results from debates/simulations.
func (m *Manifest) SetIncorporates(docPath string, incorporates []string) {
	if entry, ok := m.Documents[docPath]; ok {
		entry.Incorporates = incorporates
		m.Documents[docPath] = entry
	}
}

// SetVerifiedBy marks which verification document verified this document.
func (m *Manifest) SetVerifiedBy(docPath, verifyPath string) {
	if entry, ok := m.Documents[docPath]; ok {
		entry.VerifiedBy = verifyPath
		m.Documents[docPath] = entry
	}
}

// GetUnverifiedDrafts returns drafts that have no verification record.
func (m *Manifest) GetUnverifiedDrafts() []string {
	var unverified []string
	for path, entry := range m.Documents {
		if entry.Type == "draft" && entry.VerifiedBy == "" {
			unverified = append(unverified, path)
		}
	}
	return unverified
}

// GetUnresolvedGaps returns simulation gaps not yet resolved in any draft.
func (m *Manifest) GetUnresolvedGaps() []string {
	resolved := make(map[string]bool)
	for _, entry := range m.Documents {
		for _, r := range entry.Resolves {
			resolved[r] = true
		}
	}

	var unresolved []string
	for path, entry := range m.Documents {
		if entry.Type == "simulation" {
			if !resolved[path] {
				unresolved = append(unresolved, path)
			}
		}
	}
	return unresolved
}

// GetUnincorporatedDebates returns debate conclusions not yet incorporated in the current draft.
func (m *Manifest) GetUnincorporatedDebates() []string {
	incorporated := make(map[string]bool)
	// Check what the current draft incorporates
	if m.CurrentDraft != "" {
		if entry, ok := m.Documents[m.CurrentDraft]; ok {
			for _, inc := range entry.Incorporates {
				incorporated[inc] = true
			}
		}
	}

	var missing []string
	for path, entry := range m.Documents {
		if entry.Type == "debate" && !incorporated[path] {
			missing = append(missing, path)
		}
	}
	return missing
}
