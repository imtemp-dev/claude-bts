package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func plannedFixture(t *testing.T, phase string, rows ...string) (root, dir string) {
	t.Helper()
	root = t.TempDir()
	dir = filepath.Join(root, ".bts", "specs", "recipes", "r-129")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "recipe.json"), []byte(`{"id":"r-129","type":"build","phase":"`+phase+`"}`), 0644)
	table := "## File Structure\n\n| File | Action | Depends On | Responsibility |\n|---|---|---|---|\n"
	for _, r := range rows {
		table += "| `" + r + "` | create | — | x |\n"
	}
	_ = os.WriteFile(filepath.Join(dir, "wireframe.md"), []byte(table), 0644)
	return root, dir
}

func fileRefSeverities(content, root, dir string) map[string]string {
	result := &VerifyResult{}
	checkCodeReferences(content, root, dir, result)
	got := map[string]string{}
	for _, is := range result.Issues {
		if is.Category == "file_ref" {
			got[is.Claim] = is.Severity
		}
	}
	return got
}

// Trial problem #129: cool-crow's r-006 wireframe planned
// app/Sources/SlackPRRelayKit/SwapLink.swift and
// app/Tests/SlackPRRelayKitTests/SwapLinkTests.swift, and its mermaid
// labels named them SlackPRRelayKit/SwapLink.swift and
// Tests/SwapLinkTests.swift: four criticals "Referenced file does not
// exist" for the new files the design is about.
func TestCheckCodeReferences_AShortReferenceToAPlannedFile(t *testing.T) {
	doc := "The app makes `Kit/SwapLink.swift`, tested by `Tests/SwapLinkTests.swift`; `Other/SwapLink.swift` is not planned.\n"
	for phase, want := range map[string]string{"falsify": SeverityInfo, "complete": SeverityMajor} {
		root, dir := plannedFixture(t, phase, "app/Sources/Kit/SwapLink.swift", "app/Tests/KitTests/SwapLinkTests.swift")
		got := fileRefSeverities(doc, root, dir)
		for _, ref := range []string{"Kit/SwapLink.swift", "Tests/SwapLinkTests.swift"} {
			if got[ref] != want {
				t.Errorf("phase %s: %s names a planned file by a shorter path; want %s, got %q", phase, ref, want, got[ref])
			}
		}
		if got["Other/SwapLink.swift"] != SeverityCritical {
			t.Errorf("phase %s: a directory the planned file does not have is no match; want critical, got %q", phase, got["Other/SwapLink.swift"])
		}
	}
}

func TestCheckCodeReferences_AShortReferenceToSeveralPlannedFiles(t *testing.T) {
	root, dir := plannedFixture(t, "falsify", "app/A/Kit/SwapLink.swift", "app/B/Kit/SwapLink.swift")
	if got := fileRefSeverities("It makes `Kit/SwapLink.swift`.\n", root, dir); got["Kit/SwapLink.swift"] != SeverityMajor {
		t.Errorf("two planned files end with the reference: want major, got %q", got["Kit/SwapLink.swift"])
	}
}
