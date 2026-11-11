package state

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PointerFile, in a directory with no .bts/, names the bts project it stands
// for: a path relative to that directory, or absolute.  `bts init --multica`
// and `--session-root` write one at a Claude session root that sits above the
// repository worktree holding .bts/ (template.LinkSession).
const PointerFile = ".bts-root"

// followPointer returns the bts project dir's pointer file names, or "" when
// dir has no pointer or it names a directory without .bts/.
func followPointer(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, PointerFile))
	if err != nil {
		return ""
	}
	target := strings.TrimSpace(string(data))
	if target == "" {
		return ""
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(dir, target)
	}
	target = filepath.Clean(target)
	if fi, err := os.Stat(filepath.Join(target, ".bts")); err != nil || !fi.IsDir() {
		return ""
	}
	return target
}

// FindRoot searches for .bts/ directory starting from cwd upward.  A
// directory with no .bts/ but a PointerFile resolves to the project the
// pointer names.
// Automatically migrates old .bts/state/ layout to specs/ + local/ if needed.
func FindRoot(cwd string) (string, error) {
	dir := cwd
	for i := 0; i < 10; i++ {
		// Check for .bts/ (current) or .forge/ (legacy)
		btsDir := filepath.Join(dir, ".bts")
		forgeDir := filepath.Join(dir, ".forge")

		if _, err := os.Stat(btsDir); err == nil {
			_ = maybeMigrate(dir)
			return dir, nil
		}
		if target := followPointer(dir); target != "" {
			_ = maybeMigrate(target)
			return target, nil
		}

		// Legacy: rename .forge/ → .bts/
		if _, err := os.Stat(forgeDir); err == nil {
			fmt.Fprintf(os.Stderr, "[bts] Migrating .forge/ → .bts/\n")
			if err := os.Rename(forgeDir, btsDir); err == nil {
				_ = updateGitignoreForRename(dir)
				_ = maybeMigrate(dir)
				return dir, nil
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf(".bts/ not found from %s", cwd)
}

// SpecsPath returns the path to the specs directory (git tracked).
func SpecsPath(root string) string {
	return filepath.Join(root, ".bts", "specs")
}

// LocalPath returns the path to the local runtime directory (gitignored).
func LocalPath(root string) string {
	return filepath.Join(root, ".bts", "local")
}

// maybeMigrate migrates old .bts/state/ layout to .bts/specs/ + .bts/local/.
func maybeMigrate(root string) error {
	stateDir := filepath.Join(root, ".bts", "state")
	specsDir := filepath.Join(root, ".bts", "specs")
	localDir := filepath.Join(root, ".bts", "local")

	// Only migrate if old state/ directory still exists
	if _, err := os.Stat(stateDir); os.IsNotExist(err) {
		return nil
	}

	// Check if there's anything left to migrate
	entries, err := os.ReadDir(stateDir)
	if err != nil || len(entries) == 0 {
		_ = os.Remove(stateDir) // clean up empty dir
		return nil
	}

	fmt.Fprintf(os.Stderr, "[bts] Migrating .bts/state/ → .bts/specs/ + .bts/local/\n")

	_ = os.MkdirAll(specsDir, 0755)
	_ = os.MkdirAll(localDir, 0755)

	// Move spec files to specs/
	for _, f := range []string{"vision.md", "roadmap.md", "project-status.md", "project-map.md"} {
		moveIfExists(filepath.Join(stateDir, f), filepath.Join(specsDir, f))
	}

	// Move spec directories to specs/
	for _, d := range []string{"recipes", "debates", "layers"} {
		moveIfExists(filepath.Join(stateDir, d), filepath.Join(specsDir, d))
	}

	// Move local files to local/
	for _, f := range []string{"metrics.jsonl", "work-state.json", "active-agent.json", ".metrics-token-ts"} {
		moveIfExists(filepath.Join(stateDir, f), filepath.Join(localDir, f))
	}

	// Move per-recipe metrics.jsonl to local/recipes/{id}/
	recipesDir := filepath.Join(specsDir, "recipes")
	if entries, err := os.ReadDir(recipesDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			src := filepath.Join(recipesDir, entry.Name(), "metrics.jsonl")
			if _, err := os.Stat(src); err == nil {
				dstDir := filepath.Join(localDir, "recipes", entry.Name())
				_ = os.MkdirAll(dstDir, 0755)
				moveIfExists(src, filepath.Join(dstDir, "metrics.jsonl"))
			}
		}
	}

	// Remove state/ if empty
	_ = os.Remove(stateDir)

	// Update .gitignore
	updateGitignore(root)

	return nil
}

func moveIfExists(src, dst string) {
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return
	}
	_ = os.MkdirAll(filepath.Dir(dst), 0755)
	_ = os.Rename(src, dst)
}

func updateGitignoreForRename(root string) error {
	path := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := strings.ReplaceAll(string(data), ".forge/", ".bts/")
	return os.WriteFile(path, []byte(content), 0644)
}

func updateGitignore(root string) {
	path := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	content := string(data)
	if strings.Contains(content, ".bts/local/") {
		return // already updated
	}

	// Replace old pattern or append new
	if strings.Contains(content, ".bts/state/") {
		content = strings.Replace(content, ".bts/state/", ".bts/local/", 1)
	} else {
		// Read lines, replace the comment too if present
		var lines []string
		scanner := bufio.NewScanner(strings.NewReader(content))
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		lines = append(lines, "# bts local data (not committed)")
		lines = append(lines, ".bts/local/")
		content = strings.Join(lines, "\n") + "\n"
	}

	_ = os.WriteFile(path, []byte(content), 0644)
}

// ReadJSON reads a JSON file into the target struct.
func ReadJSON(path string, target interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

// WriteJSON writes a struct to a JSON file atomically (temp + rename).
func WriteJSON(path string, data interface{}) error {
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".bts-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(bytes); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp: %w", err)
	}
	tmp.Close()

	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename: %w", err)
	}

	return nil
}

// AppendJSONL appends a JSON line to a JSONL file.
func AppendJSONL(path string, data interface{}) error {
	bytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	_, err = fmt.Fprintf(f, "%s\n", bytes)
	return err
}
