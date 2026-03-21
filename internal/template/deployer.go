package template

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// DeployForce overwrites existing files except those in skipFiles.
// skipFiles paths are relative to projectRoot (e.g., ".bts/config/settings.yaml").
func DeployForce(projectRoot string, skipFiles []string) ([]string, error) {
	skip := make(map[string]bool)
	for _, f := range skipFiles {
		skip[f] = true
	}

	tmplFS, err := EmbeddedTemplates()
	if err != nil {
		return nil, fmt.Errorf("load templates: %w", err)
	}

	var updated []string

	err = fs.WalkDir(tmplFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		targetPath := filepath.Join(projectRoot, path)

		if d.IsDir() {
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return fmt.Errorf("mkdir %s: %w", targetPath, err)
			}
			return nil
		}

		// Skip protected files
		if skip[path] {
			return nil
		}

		data, err := fs.ReadFile(tmplFS, path)
		if err != nil {
			return fmt.Errorf("read template %s: %w", path, err)
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return fmt.Errorf("mkdir parent %s: %w", filepath.Dir(targetPath), err)
		}

		if err := os.WriteFile(targetPath, data, 0644); err != nil {
			return fmt.Errorf("write %s: %w", targetPath, err)
		}

		if filepath.Ext(path) == ".sh" {
			if err := os.Chmod(targetPath, 0755); err != nil {
				return fmt.Errorf("chmod %s: %w", targetPath, err)
			}
		}

		updated = append(updated, path)
		return nil
	})

	return updated, err
}

// Deploy walks the embedded filesystem and writes all files to projectRoot.
// Existing files are NOT overwritten (user files take precedence).
func Deploy(projectRoot string) ([]string, error) {
	tmplFS, err := EmbeddedTemplates()
	if err != nil {
		return nil, fmt.Errorf("load templates: %w", err)
	}

	var created []string

	err = fs.WalkDir(tmplFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		targetPath := filepath.Join(projectRoot, path)

		if d.IsDir() {
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return fmt.Errorf("mkdir %s: %w", targetPath, err)
			}
			return nil
		}

		// Don't overwrite existing files
		if _, err := os.Stat(targetPath); err == nil {
			return nil
		}

		data, err := fs.ReadFile(tmplFS, path)
		if err != nil {
			return fmt.Errorf("read template %s: %w", path, err)
		}

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return fmt.Errorf("mkdir parent %s: %w", filepath.Dir(targetPath), err)
		}

		if err := os.WriteFile(targetPath, data, 0644); err != nil {
			return fmt.Errorf("write %s: %w", targetPath, err)
		}

		// Preserve executable bit for shell scripts
		if filepath.Ext(path) == ".sh" {
			if err := os.Chmod(targetPath, 0755); err != nil {
				return fmt.Errorf("chmod %s: %w", targetPath, err)
			}
		}

		created = append(created, path)
		return nil
	})

	return created, err
}
