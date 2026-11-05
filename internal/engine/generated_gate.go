package engine

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Generated files: the wireframe's `generate` rows (trial problem #66).
//
// A change to an API often has files no task writes: the OpenAPI document,
// the client types generated from it, agent tool headers generated from
// those. true-shark's wireframe had nowhere to put them, so it marked them
// `generated`, which passed as a non-task row, and nothing recorded that
// they had to be regenerated after the code they come from changed. A
// generate row carries its generator, `bts check generated` runs it and
// records what it wrote, and IMPLEMENT DONE holds the record to the disk.

// WireframeGenerateRows reads the recipe's wireframe.md for its `generate`
// rows; none when the recipe has no wireframe.
func WireframeGenerateRows(root, recipeID string) []GenerateRow {
	data, err := os.ReadFile(filepath.Join(state.RecipeDir(root, recipeID), "wireframe.md"))
	if err != nil {
		return nil
	}
	return ParseWireframeGenerateRows(string(data))
}

// GenerateRowPaths and GenerateRowCommands list a set of rows' paths, and
// their distinct generators in row order: rows that share a generator (one
// command writing many files) run it once.
func GenerateRowPaths(rows []GenerateRow) []string {
	paths := make([]string, 0, len(rows))
	for _, r := range rows {
		paths = append(paths, r.Path)
	}
	return paths
}

func GenerateRowCommands(rows []GenerateRow) []string {
	var cmds []string
	seen := map[string]bool{}
	for _, r := range rows {
		if r.Command == "" || seen[r.Command] {
			continue
		}
		seen[r.Command] = true
		cmds = append(cmds, r.Command)
	}
	return cmds
}

// GenerateRowsWithoutGenerator lists the paths of rows that name none.
func GenerateRowsWithoutGenerator(rows []GenerateRow) []string {
	var paths []string
	for _, r := range rows {
		if r.Command == "" {
			paths = append(paths, r.Path)
		}
	}
	return paths
}

// GeneratedFiles fingerprints generated paths, each relative to root: per
// path, and over all of them. A directory row stands for every file under
// it; a missing file and an empty directory are "absent".
func GeneratedFiles(root string, paths []string) (perPath map[string]string, all string, err error) {
	perPath = make(map[string]string, len(paths))
	lines := make([]string, 0, len(paths))
	for _, p := range paths {
		rel := filepath.ToSlash(filepath.Clean(p))
		h, err := generatedPathHash(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, "", fmt.Errorf("hash %s: %w", rel, err)
		}
		perPath[p] = h
		lines = append(lines, rel+"\x00"+h)
	}
	sort.Strings(lines)
	return perPath, state.ContentHash([]byte(strings.Join(lines, "\n"))), nil
}

func generatedPathHash(full string) (string, error) {
	info, err := os.Stat(full)
	if os.IsNotExist(err) {
		return "absent", nil
	}
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		h, _, err := state.FileContentHash(full)
		return h, err
	}
	var lines []string
	err = filepath.WalkDir(full, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil || !d.Type().IsRegular() {
			return werr
		}
		h, _, herr := state.FileContentHash(p)
		if herr != nil {
			return herr
		}
		rel, _ := filepath.Rel(full, p)
		lines = append(lines, filepath.ToSlash(rel)+"\x00"+h)
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(lines) == 0 {
		return "absent", nil
	}
	sort.Strings(lines)
	return state.ContentHash([]byte(strings.Join(lines, "\n"))), nil
}

// CheckGeneratedCurrent is gate generated_current at IMPLEMENT DONE: when
// the wireframe has `generate` rows, the last `bts check generated` ran
// every generator they name, each exited 0 and wrote its files, and since
// then neither the code (codeHash) nor the generated files (filesHash)
// have changed. Together that is "regenerate and require no diff", judged
// on runs bts made rather than on the agent's word. Returns "" when met.
func CheckGeneratedCurrent(g *state.GeneratedRun, rows []GenerateRow, codeHash, filesHash, recipeID string) string {
	if len(rows) == 0 {
		return ""
	}
	rerun := fmt.Sprintf("`bts check generated %s`", recipeID)
	if bare := GenerateRowsWithoutGenerator(rows); len(bare) > 0 {
		return fmt.Sprintf("The wireframe's generate row(s) %s name no generator. Write `generator:` and the command, in backticks, in the row's Responsibility cell, then run %s.",
			strings.Join(bare, ", "), rerun)
	}
	paths := GenerateRowPaths(rows)
	switch {
	case g == nil:
		return fmt.Sprintf("The wireframe has generate rows (%s) and no generated check is recorded. After the last code change run %s: it reruns each generator and records what it wrote.",
			strings.Join(paths, ", "), rerun)
	case !g.Passed():
		return fmt.Sprintf("The last generated check did not pass (%s). Fix the generator or the code it reads, then run %s again.", generatedFailure(g), rerun)
	case !sameStrings(g.Files, paths) || !sameStrings(runCommands(g.Runs), GenerateRowCommands(rows)):
		return fmt.Sprintf("The wireframe's generate rows changed after the last generated check. Run %s again.", rerun)
	case codeHash != "" && g.CodeHash != codeHash:
		return fmt.Sprintf("The code changed after the generators last ran, so the generated files may no longer match it. Run %s again.", rerun)
	case g.FilesHash != filesHash:
		return fmt.Sprintf("A generated file changed after its generator wrote it, and generated files are never edited by hand. Run %s again; it regenerates them.", rerun)
	}
	return ""
}

func generatedFailure(g *state.GeneratedRun) string {
	var parts []string
	for _, r := range g.Runs {
		if !r.Passed() {
			parts = append(parts, fmt.Sprintf("%q exited %d", r.Command, r.ExitCode))
		}
	}
	if len(g.Missing) > 0 {
		parts = append(parts, "not written: "+strings.Join(g.Missing, ", "))
	}
	if len(parts) == 0 {
		parts = append(parts, "no generator ran")
	}
	return strings.Join(parts, "; ")
}

func runCommands(runs []*state.CommandRun) []string {
	cmds := make([]string, 0, len(runs))
	for _, r := range runs {
		cmds = append(cmds, r.Command)
	}
	return cmds
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// CheckWireframeGenerateRows is the design-time half: `bts verify
// wireframe.md` reports a generate row that names no generator, so the gap
// shows up at the wireframe step rather than at IMPLEMENT DONE.
func CheckWireframeGenerateRows(wireframePath string) []Issue {
	data, err := os.ReadFile(wireframePath)
	if err != nil {
		return nil
	}
	var issues []Issue
	for _, p := range GenerateRowsWithoutGenerator(ParseWireframeGenerateRows(string(data))) {
		issues = append(issues, Issue{
			Category: "wireframe",
			Claim:    "generate_row_without_generator: " + p,
			Severity: SeverityMajor,
			Detail: "A `generate` row is written by its generator, never by a task, so it must say which command regenerates it: " +
				"put `generator: ` and the command in a code span in its Responsibility cell (bts-wireframe Step 4). " +
				"`bts check generated` runs it, and IMPLEMENT DONE requires that run on the current code.",
		})
	}
	return issues
}
