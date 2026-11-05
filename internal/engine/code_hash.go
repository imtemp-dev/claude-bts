package engine

import (
	"fmt"
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
// document order, carried ones included (DomainInvariantKinds tells them
// apart). A missing file is an error, and so is a file in which no
// invariant row is found: every gate that reads the list (falsifiers red,
// green, able to fail) would otherwise pass with nothing checked (trial
// problem #53).
func DomainInvariantIDs(domainPath string) ([]string, error) {
	invs, err := domainInvariantRows(domainPath)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(invs))
	for _, inv := range invs {
		ids = append(ids, inv.ID)
	}
	return ids, nil
}

// DomainInvariantKinds splits the ids of domain.md § 2 into the invariants
// this recipe introduces and the ones it carries (`[carried]` in the ID
// cell), each in document order. The falsifier gates treat them in
// opposite ways: an introduced invariant's test must fail on the skeleton
// and be shown able to fail, while a carried one's test already passes and
// must keep passing (trial problem #41). Errors as DomainInvariantIDs does.
func DomainInvariantKinds(domainPath string) (introduced, carried []string, err error) {
	invs, err := domainInvariantRows(domainPath)
	if err != nil {
		return nil, nil, err
	}
	for _, inv := range invs {
		if inv.Carried {
			carried = append(carried, inv.ID)
		} else {
			introduced = append(introduced, inv.ID)
		}
	}
	return introduced, carried, nil
}

// domainInvariantRows is the § 2 rows that carry an id; none is an error.
func domainInvariantRows(domainPath string) ([]Invariant, error) {
	invs, err := parseInvariantsTable(domainPath)
	if err != nil {
		return nil, err
	}
	rows := make([]Invariant, 0, len(invs))
	for _, inv := range invs {
		if inv.ID = strings.TrimSpace(inv.ID); inv.ID != "" {
			rows = append(rows, inv)
		}
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%s: no invariant rows found under a \"## 2. Invariants\" heading", filepath.Base(domainPath))
	}
	return rows, nil
}
