package state

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RoadmapProgress scans roadmap.md for checkbox items.
// Returns (done, total, nextItemDescription).
// Returns (0, 0, "") if roadmap.md doesn't exist or Status is DRAFT.
func RoadmapProgress(root string) (int, int, string) {
	path := filepath.Join(StatePath(root), "roadmap.md")
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, ""
	}
	defer f.Close()

	done, total := 0, 0
	nextItem := ""
	confirmed := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "Status:") && strings.Contains(line, "CONFIRMED") {
			confirmed = true
		}
		switch {
		case strings.HasPrefix(line, "- [x] "):
			done++
			total++
		case strings.HasPrefix(line, "- [ ] "):
			total++
			if nextItem == "" {
				item := strings.TrimPrefix(line, "- [ ] ")
				if idx := strings.Index(item, " (recipe:"); idx > 0 {
					item = item[:idx]
				}
				nextItem = item
			}
		case strings.HasPrefix(line, "- [-] "):
			total++
		}
	}
	if !confirmed {
		return 0, 0, ""
	}
	return done, total, nextItem
}

// MarkRoadmapItemDone finds a roadmap item matching the recipe ID and marks it [x].
// Matches by "(recipe: {id})" annotation. Updates the Progress line.
// No-op if roadmap.md doesn't exist or no matching item found.
func MarkRoadmapItemDone(root string, recipeID string) {
	path := filepath.Join(StatePath(root), "roadmap.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	lines := strings.Split(string(data), "\n")
	found := false
	marker := "(recipe: " + recipeID + ")"

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- [ ] ") && strings.Contains(trimmed, marker) {
			lines[i] = strings.Replace(lines[i], "- [ ] ", "- [x] ", 1)
			found = true
			break
		}
	}
	if !found {
		return
	}

	// Recount and update Progress line
	done, total := 0, 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "- [x] "):
			done++
			total++
		case strings.HasPrefix(trimmed, "- [ ] "):
			total++
		case strings.HasPrefix(trimmed, "- [-] "):
			total++
		}
	}
	progressLine := fmt.Sprintf("Progress: %d/%d", done, total)
	progressFound := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "Progress:") {
			lines[i] = progressLine
			progressFound = true
			break
		}
	}
	if !progressFound {
		// Insert Progress line after Status line
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "Status:") {
				lines = append(lines[:i+1], append([]string{progressLine}, lines[i+1:]...)...)
				break
			}
		}
	}

	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}

// VisionExists checks if vision.md exists at the project level.
func VisionExists(root string) bool {
	path := filepath.Join(StatePath(root), "vision.md")
	_, err := os.Stat(path)
	return err == nil
}

// RoadmapExists checks if roadmap.md exists at the project level.
func RoadmapExists(root string) bool {
	path := filepath.Join(StatePath(root), "roadmap.md")
	_, err := os.Stat(path)
	return err == nil
}
