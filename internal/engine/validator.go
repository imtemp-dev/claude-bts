package engine

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidationError represents one schema violation.
type ValidationError struct {
	File    string `json:"file"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e ValidationError) String() string {
	return fmt.Sprintf("%s: %s — %s", e.File, e.Field, e.Message)
}

// ValidateRecipeDir checks all JSON files in a recipe directory for schema compliance.
func ValidateRecipeDir(recipeDir string) ([]ValidationError, error) {
	var errors []ValidationError

	// 1. recipe.json
	recipePath := filepath.Join(recipeDir, "recipe.json")
	if errs := validateRecipeJSON(recipePath); len(errs) > 0 {
		errors = append(errors, errs...)
	}

	// 2. manifest.json
	manifestPath := filepath.Join(recipeDir, "manifest.json")
	if errs := validateManifestJSON(manifestPath); len(errs) > 0 {
		errors = append(errors, errs...)
	}

	// 3. changelog.jsonl
	changelogPath := filepath.Join(recipeDir, "changelog.jsonl")
	if errs := validateChangelogJSONL(changelogPath); len(errs) > 0 {
		errors = append(errors, errs...)
	}

	// 4. debate meta.json files
	debatesDir := filepath.Join(recipeDir, "debates")
	if entries, err := os.ReadDir(debatesDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				metaPath := filepath.Join(debatesDir, entry.Name(), "meta.json")
				if errs := validateDebateMetaJSON(metaPath); len(errs) > 0 {
					errors = append(errors, errs...)
				}
			}
		}
	}

	// 5. tasks.json (optional — only exists after /implement)
	tasksPath := filepath.Join(recipeDir, "tasks.json")
	if errs := validateTasksJSON(tasksPath); len(errs) > 0 {
		errors = append(errors, errs...)
	}

	// 6. test-results.json (optional — only exists after /test)
	testResultsPath := filepath.Join(recipeDir, "test-results.json")
	if errs := validateTestResultsJSON(testResultsPath); len(errs) > 0 {
		errors = append(errors, errs...)
	}

	return errors, nil
}

func validateRecipeJSON(path string) []ValidationError {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []ValidationError{{File: "recipe.json", Field: "(file)", Message: "missing — create recipe.json at recipe start"}}
	}
	if err != nil {
		return []ValidationError{{File: "recipe.json", Field: "(file)", Message: err.Error()}}
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return []ValidationError{{File: "recipe.json", Field: "(parse)", Message: "invalid JSON: " + err.Error()}}
	}

	var errs []ValidationError
	for _, field := range []string{"id", "type", "topic", "phase", "started_at", "updated_at"} {
		if _, ok := raw[field]; !ok {
			errs = append(errs, ValidationError{File: "recipe.json", Field: field, Message: "missing required field"})
		}
	}
	for _, field := range []string{"iteration", "draft_version", "level"} {
		if _, ok := raw[field]; !ok {
			errs = append(errs, ValidationError{File: "recipe.json", Field: field, Message: "missing required field"})
		}
	}

	// Validate type enum
	if t, ok := raw["type"].(string); ok {
		valid := map[string]bool{"analyze": true, "design": true, "blueprint": true, "fix": true}
		if !valid[t] {
			errs = append(errs, ValidationError{File: "recipe.json", Field: "type", Message: fmt.Sprintf("invalid value '%s', must be analyze/design/blueprint", t)})
		}
	}

	// Validate phase enum
	if p, ok := raw["phase"].(string); ok {
		valid := map[string]bool{
			"scoping": true,
			"research": true, "draft": true, "assess": true, "improve": true,
			"verify": true, "debate": true, "simulate": true, "audit": true,
			"finalize": true, "cancelled": true,
			"implement": true, "test": true, "sync": true, "status": true,
			"complete": true,
		}
		if !valid[p] {
			errs = append(errs, ValidationError{File: "recipe.json", Field: "phase", Message: fmt.Sprintf("invalid value '%s', must be a valid phase", p)})
		}
	}

	return errs
}

func validateManifestJSON(path string) []ValidationError {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []ValidationError{{File: "manifest.json", Field: "(file)", Message: "missing — create manifest.json to track documents"}}
	}
	if err != nil {
		return []ValidationError{{File: "manifest.json", Field: "(file)", Message: err.Error()}}
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return []ValidationError{{File: "manifest.json", Field: "(parse)", Message: "invalid JSON: " + err.Error()}}
	}

	var errs []ValidationError

	// Required fields
	if _, ok := raw["current_draft"]; !ok {
		errs = append(errs, ValidationError{File: "manifest.json", Field: "current_draft", Message: "missing required field"})
	}
	if _, ok := raw["level"]; !ok {
		errs = append(errs, ValidationError{File: "manifest.json", Field: "level", Message: "missing required field"})
	}

	// documents must be a map of path → DocumentEntry (not categorized lists)
	if docs, ok := raw["documents"]; ok {
		docsMap, isMap := docs.(map[string]interface{})
		if !isMap {
			errs = append(errs, ValidationError{File: "manifest.json", Field: "documents", Message: "must be an object with file paths as keys, not categorized lists"})
		} else {
			for path, entry := range docsMap {
				entryMap, isObj := entry.(map[string]interface{})
				if !isObj {
					errs = append(errs, ValidationError{File: "manifest.json", Field: "documents." + path, Message: "must be a DocumentEntry object with 'type' and 'created_at'"})
					continue
				}
				if t, ok := entryMap["type"].(string); ok {
					validTypes := map[string]bool{
						"research": true, "draft": true, "debate": true,
						"simulation": true, "verification": true,
						"implementation": true, "test-result": true, "deviation": true,
					}
					if !validTypes[t] {
						errs = append(errs, ValidationError{File: "manifest.json", Field: "documents." + path + ".type", Message: fmt.Sprintf("invalid type '%s'", t)})
					}
				} else if _, exists := entryMap["type"]; !exists {
					errs = append(errs, ValidationError{File: "manifest.json", Field: "documents." + path + ".type", Message: "missing required field"})
				}
				if _, ok := entryMap["created_at"]; !ok {
					errs = append(errs, ValidationError{File: "manifest.json", Field: "documents." + path + ".created_at", Message: "missing required field"})
				}
			}
		}
	} else {
		errs = append(errs, ValidationError{File: "manifest.json", Field: "documents", Message: "missing required field"})
	}

	return errs
}

func validateChangelogJSONL(path string) []ValidationError {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil // changelog is optional early on
	}
	if err != nil {
		return []ValidationError{{File: "changelog.jsonl", Field: "(file)", Message: err.Error()}}
	}
	defer f.Close()

	var errs []ValidationError
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			errs = append(errs, ValidationError{File: "changelog.jsonl", Field: fmt.Sprintf("line %d", lineNum), Message: "invalid JSON"})
			continue
		}

		// Key must be "time", not "timestamp"
		if _, ok := raw["time"]; !ok {
			if _, hasTimestamp := raw["timestamp"]; hasTimestamp {
				errs = append(errs, ValidationError{File: "changelog.jsonl", Field: fmt.Sprintf("line %d", lineNum), Message: "use 'time' not 'timestamp' as key name"})
			} else {
				errs = append(errs, ValidationError{File: "changelog.jsonl", Field: fmt.Sprintf("line %d.time", lineNum), Message: "missing required field"})
			}
		}

		if action, ok := raw["action"].(string); ok {
			validActions := map[string]bool{
				"research": true, "draft": true, "improve": true, "verify": true,
				"debate": true, "simulate": true, "audit": true, "assess": true,
				"sync-check": true, "finalize": true,
				"implement": true, "test": true, "sync": true, "status": true,
			"adjudicate": true,
			}
			if !validActions[action] {
				errs = append(errs, ValidationError{File: "changelog.jsonl", Field: fmt.Sprintf("line %d.action", lineNum), Message: fmt.Sprintf("invalid action '%s'", action)})
			}
		} else if _, exists := raw["action"]; !exists {
			errs = append(errs, ValidationError{File: "changelog.jsonl", Field: fmt.Sprintf("line %d.action", lineNum), Message: "missing required field"})
		}
	}

	return errs
}

func validateDebateMetaJSON(path string) []ValidationError {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return []ValidationError{{File: path, Field: "(file)", Message: err.Error()}}
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return []ValidationError{{File: path, Field: "(parse)", Message: "invalid JSON: " + err.Error()}}
	}

	fileName := filepath.Base(filepath.Dir(path)) + "/meta.json"
	var errs []ValidationError

	for _, field := range []string{"id", "topic"} {
		if _, ok := raw[field]; !ok {
			errs = append(errs, ValidationError{File: fileName, Field: field, Message: "missing required field"})
		}
	}
	if _, ok := raw["rounds"]; !ok {
		errs = append(errs, ValidationError{File: fileName, Field: "rounds", Message: "missing required field"})
	}
	if _, ok := raw["decided"]; !ok {
		errs = append(errs, ValidationError{File: fileName, Field: "decided", Message: "missing required field — use boolean, not 'status'"})
	}

	// conclusion must be string, not object
	if conclusion, ok := raw["conclusion"]; ok {
		if _, isString := conclusion.(string); !isString {
			errs = append(errs, ValidationError{File: fileName, Field: "conclusion", Message: "must be a string, not an object. Write structured conclusions as a single sentence."})
		}
	}

	for _, field := range []string{"started_at", "updated_at"} {
		if _, ok := raw[field]; !ok {
			errs = append(errs, ValidationError{File: fileName, Field: field, Message: "missing required field"})
		}
	}

	return errs
}

func validateTasksJSON(path string) []ValidationError {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil // tasks.json is optional — only exists after /implement
	}
	if err != nil {
		return []ValidationError{{File: "tasks.json", Field: "(file)", Message: err.Error()}}
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return []ValidationError{{File: "tasks.json", Field: "(parse)", Message: "invalid JSON: " + err.Error()}}
	}

	var errs []ValidationError

	for _, field := range []string{"recipe_id", "started_at", "updated_at"} {
		if _, ok := raw[field]; !ok {
			errs = append(errs, ValidationError{File: "tasks.json", Field: field, Message: "missing required field"})
		}
	}

	tasks, ok := raw["tasks"]
	if !ok {
		errs = append(errs, ValidationError{File: "tasks.json", Field: "tasks", Message: "missing required field"})
		return errs
	}

	tasksList, isList := tasks.([]interface{})
	if !isList {
		errs = append(errs, ValidationError{File: "tasks.json", Field: "tasks", Message: "must be an array"})
		return errs
	}

	validStatuses := map[string]bool{
		"pending": true, "in_progress": true, "done": true, "blocked": true, "skipped": true,
	}
	validActions := map[string]bool{"create": true, "modify": true}

	for i, task := range tasksList {
		taskMap, isObj := task.(map[string]interface{})
		if !isObj {
			errs = append(errs, ValidationError{File: "tasks.json", Field: fmt.Sprintf("tasks[%d]", i), Message: "must be an object"})
			continue
		}

		for _, field := range []string{"id", "file", "action", "status", "description"} {
			if _, ok := taskMap[field]; !ok {
				errs = append(errs, ValidationError{File: "tasks.json", Field: fmt.Sprintf("tasks[%d].%s", i, field), Message: "missing required field"})
			}
		}

		if action, ok := taskMap["action"].(string); ok {
			if !validActions[action] {
				errs = append(errs, ValidationError{File: "tasks.json", Field: fmt.Sprintf("tasks[%d].action", i), Message: fmt.Sprintf("invalid value '%s', must be create/modify", action)})
			}
		}

		if status, ok := taskMap["status"].(string); ok {
			if !validStatuses[status] {
				errs = append(errs, ValidationError{File: "tasks.json", Field: fmt.Sprintf("tasks[%d].status", i), Message: fmt.Sprintf("invalid value '%s', must be pending/in_progress/done/blocked/skipped", status)})
			}
		}
	}

	return errs
}

func validateTestResultsJSON(path string) []ValidationError {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil // test-results.json is optional — only exists after /test
	}
	if err != nil {
		return []ValidationError{{File: "test-results.json", Field: "(file)", Message: err.Error()}}
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return []ValidationError{{File: "test-results.json", Field: "(parse)", Message: "invalid JSON: " + err.Error()}}
	}

	var errs []ValidationError

	for _, field := range []string{"recipe_id", "run_at", "framework"} {
		if _, ok := raw[field]; !ok {
			errs = append(errs, ValidationError{File: "test-results.json", Field: field, Message: "missing required field"})
		}
	}

	for _, field := range []string{"iterations", "total", "passed", "failed", "skipped"} {
		if _, ok := raw[field]; !ok {
			errs = append(errs, ValidationError{File: "test-results.json", Field: field, Message: "missing required field"})
		}
	}

	if status, ok := raw["status"].(string); ok {
		if status != "pass" && status != "fail" {
			errs = append(errs, ValidationError{File: "test-results.json", Field: "status", Message: fmt.Sprintf("invalid value '%s', must be pass/fail", status)})
		}
	} else if _, exists := raw["status"]; !exists {
		errs = append(errs, ValidationError{File: "test-results.json", Field: "status", Message: "missing required field"})
	}

	return errs
}
