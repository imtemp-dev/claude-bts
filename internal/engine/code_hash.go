package engine

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// CodeHash fingerprints a recipe's code files for evidence that must name
// the code it ran on. It is state.ImplHash over tasks.json when the recipe
// has one, and otherwise over the files the wireframe's File Structure rows
// will create, modify or delete.
//
// The fallback exists for the build recipe: its skeleton is compiled and its
// falsifiers are run before implementation writes tasks.json, and those runs
// still have to say which code they judged. Implementation derives its tasks
// from the same rows, so both sources name the same files.
//
// When neither source names a file (a fix recipe writes neither), it is
// state.TreeHash over the whole project, so the freshness gates still see
// an edit made after a run; "" only outside a git repository.
func CodeHash(root, recipeID string) (string, error) {
	if h, err := state.ImplHash(root, codeTasks(root, recipeID)); err != nil || h != "" {
		return h, err
	}
	return state.TreeHash(root)
}

// CodeFiles lists the files CodeHash covers, as written in tasks.json or
// the wireframe (relative to the project root, slash-separated).
func CodeFiles(root, recipeID string) []string {
	var files []string
	seen := map[string]bool{}
	for _, t := range codeTasks(root, recipeID) {
		f := filepath.ToSlash(filepath.Clean(strings.TrimSpace(t.File)))
		if f == "" || f == "." || seen[f] {
			continue
		}
		seen[f] = true
		files = append(files, f)
	}
	return files
}

func codeTasks(root, recipeID string) []state.Task {
	if ts, err := state.LoadTaskState(root, recipeID); err == nil && len(ts.Tasks) > 0 {
		return ts.Tasks
	}
	data, err := os.ReadFile(filepath.Join(state.RecipeDir(root, recipeID), "wireframe.md"))
	if err != nil {
		return nil
	}
	var tasks []state.Task
	for _, k := range ParseWireframeFileTable(string(data)) {
		switch strings.ToLower(strings.TrimSpace(k.Action)) {
		case "create", "modify", "delete":
			tasks = append(tasks, state.Task{File: k.Path, Action: k.Action})
		}
	}
	return tasks
}

// DomainInvariantIDs lists the invariant ids declared in domain.md § 2, in
// document order. A missing file is an error; a file with no invariants
// returns an empty list.
func DomainInvariantIDs(domainPath string) ([]string, error) {
	invs, err := parseInvariantsTable(domainPath)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(invs))
	for _, inv := range invs {
		if id := strings.TrimSpace(inv.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
