package cli

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/spf13/cobra"
)

// bts falsifier mutate: pstack's "able to fail". A falsifier that has only
// been seen failing on the skeleton (at "not implemented") and passing on
// the code proves nothing about its invariant. This breaks the invariant's
// guard in a copy of the project and requires the falsifier to fail there,
// in its own test.
//
//   bts falsifier mutate <id> --inv INV-NNN --patch <file> --expect "<test id>" [--setup "<cmd>"]
//   bts falsifier mutate <id> --inv INV-NNN --not-mutable "<reason>"
//   bts falsifier mutate <id> --rerun        reapply every stored patch to the current code

func init() {
	falsifierCmd.AddCommand(falsifierMutateCmd)
	f := falsifierMutateCmd.Flags()
	f.String("inv", "", "Invariant id from domain.md § 2, e.g. INV-003")
	f.String("patch", "", "Git-style unified diff (a/ and b/ paths) that breaks the invariant's guard in the recipe's code")
	f.String("expect", "", "Text the failing run must print: the invariant's own test id")
	f.String("setup", "", "Command run in the copy before the falsifier, e.g. a dependency install")
	f.String("not-mutable", "", "Record that the invariant has no code guard to break, with the reason")
	f.Bool("rerun", false, "Reapply every stored mutation to the current code and rerun its falsifier")
	f.Int("timeout", 600, "Seconds before each run is killed")
}

var falsifierMutateCmd = &cobra.Command{
	Use:   "mutate <recipe-id>",
	Short: "Break an invariant's guard in a copy of the project; its falsifier must fail there (able to fail)",
	Args:  cobra.ExactArgs(1),
	// A refused or survived mutation is a finding, not a usage mistake.
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		id := args[0]
		inv, _ := cmd.Flags().GetString("inv")
		patch, _ := cmd.Flags().GetString("patch")
		expect, _ := cmd.Flags().GetString("expect")
		setup, _ := cmd.Flags().GetString("setup")
		notMutable, _ := cmd.Flags().GetString("not-mutable")
		rerun, _ := cmd.Flags().GetBool("rerun")
		timeout, _ := cmd.Flags().GetInt("timeout")

		switch {
		case rerun:
			results, err := executeMutateRerun(root, id, timeout, os.Stdout)
			if err != nil {
				return err
			}
			bad := 0
			fmt.Println("\n[bts] mutations, rerun on the current code:")
			for _, inv := range sortedMutationKeys(results) {
				r := results[inv]
				mark := "killed"
				if !r.Killed() {
					mark = "NOT KILLED"
					bad++
				}
				fmt.Printf("  %-10s exit=%-3d %s\n", inv, r.ExitCode, mark)
			}
			if bad > 0 {
				fmt.Printf("[bts] %d of %d falsifiers did not fail with their guard broken\n", bad, len(results))
				os.Exit(1)
			}
			return nil
		case notMutable != "":
			if err := executeMutateNotMutable(root, id, inv, notMutable); err != nil {
				return err
			}
			fmt.Printf("[bts] %s recorded as not mutable → build-evidence.json\n", inv)
			return nil
		default:
			m, err := executeFalsifierMutate(root, id, mutateOptions{
				Inv: inv, PatchFile: patch, Expect: expect, Setup: setup, TimeoutSec: timeout,
			}, os.Stdout)
			if err != nil {
				return err
			}
			fmt.Printf("\n[bts] %s: the falsifier failed in %q with its guard broken (exit=%d) → build-evidence.json\n", inv, m.Expect, m.ExitCode)
			return nil
		}
	},
}

type mutateOptions struct {
	Inv, PatchFile, Expect, Setup string
	TimeoutSec                    int
}

var (
	errMutationSurvived = errors.New("the falsifier passed with the guard broken, so it does not test the invariant yet; " +
		"make the test assert the invariant's outcome, rerun `bts falsifier check`, then mutate again")
	errMutationUnmatched = errors.New("the run failed, but the --expect text was not in its output; it may have failed at import or in another test. " +
		"Break only the guard, or name the test that should fail with --expect")
	errMutationTimedOut = errors.New("the run timed out; a broken guard must make the falsifier fail, not hang")
)

func executeFalsifierMutate(root, recipeID string, o mutateOptions, sink io.Writer) (*state.MutationRun, error) {
	f, _, err := loadFalsifierFor(root, recipeID, o.Inv)
	if err != nil {
		return nil, err
	}
	if o.PatchFile == "" {
		return nil, errors.New("--patch is required (or --not-mutable, or --rerun)")
	}
	if strings.TrimSpace(o.Expect) == "" {
		return nil, errors.New("--expect is required: the invariant's own test id, which the failing run must print")
	}
	patch, err := os.ReadFile(o.PatchFile)
	if err != nil {
		return nil, fmt.Errorf("read patch: %w", err)
	}
	files, err := checkMutationPatch(root, recipeID, patch)
	if err != nil {
		return nil, err
	}
	rel := filepath.ToSlash(filepath.Join("mutations", o.Inv+".patch"))
	if err := os.MkdirAll(filepath.Join(state.RecipeDir(root, recipeID), "mutations"), 0755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(state.RecipeDir(root, recipeID), rel), patch, 0644); err != nil {
		return nil, fmt.Errorf("store patch: %w", err)
	}
	m, err := runMutation(root, recipeID, o.Inv, f.Command, patch, rel, files, o.Expect, o.Setup, o.TimeoutSec, sink)
	if err != nil {
		return nil, err
	}
	// Read again after the run, so what another command recorded meanwhile
	// is kept (trial problem #117).
	err = state.UpdateBuildEvidence(root, recipeID, func(ev *state.BuildEvidence) error {
		g := ev.Falsifiers[o.Inv]
		if g == nil || g.Command != f.Command {
			return fmt.Errorf("%s's falsifier was recorded again while the mutation ran; run the mutation again", o.Inv)
		}
		g.Mutation, g.NotMutable = m, ""
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("save build evidence: %w", err)
	}
	appendEvidenceChangelog(root, recipeID, "mutate", mutationSummary(o.Inv, m), m.BaseHash)
	return m, mutationError(o.Inv, m)
}

func executeMutateNotMutable(root, recipeID, inv, reason string) error {
	f, ev, err := loadFalsifierFor(root, recipeID, inv)
	if err != nil {
		return err
	}
	f.NotMutable = strings.TrimSpace(reason)
	f.Mutation = nil
	if err := state.SaveBuildEvidence(root, recipeID, ev); err != nil {
		return fmt.Errorf("save build evidence: %w", err)
	}
	hash, _ := engine.CodeHash(root, recipeID)
	appendEvidenceChangelog(root, recipeID, "mutate", fmt.Sprintf("%s not mutable: %s", inv, f.NotMutable), hash)
	return nil
}

func executeMutateRerun(root, recipeID string, timeoutSec int, sink io.Writer) (map[string]*state.MutationRun, error) {
	if _, err := state.LoadRecipeState(root, recipeID); err != nil {
		return nil, fmt.Errorf("load recipe %s: %w", recipeID, err)
	}
	ev, err := state.LoadBuildEvidence(root, recipeID)
	if err != nil {
		return nil, fmt.Errorf("load build evidence: %w", err)
	}
	results := map[string]*state.MutationRun{}
	var order []string
	for _, inv := range engine.FalsifierIDs(ev) {
		f := ev.Falsifiers[inv]
		if f == nil || f.Mutation == nil || f.NotMutable != "" {
			continue
		}
		prev := f.Mutation
		patch, err := os.ReadFile(filepath.Join(state.RecipeDir(root, recipeID), filepath.FromSlash(prev.Patch)))
		if err != nil {
			return nil, fmt.Errorf("%s: read stored patch: %w", inv, err)
		}
		fmt.Fprintf(sink, "\n[bts] %s: %s (patch %s)\n", inv, f.Command, prev.Patch)
		m, err := runMutation(root, recipeID, inv, f.Command, patch, prev.Patch, prev.Files, prev.Expect, prev.Setup, timeoutSec, sink)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", inv, err)
		}
		results[inv] = m
		order = append(order, inv)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no mutations recorded; record each with `bts falsifier mutate %s --inv <id> --patch <file> --expect \"<test id>\"`", recipeID)
	}
	// Read again after the runs (trial problem #117): a falsifier recorded
	// anew, given another patch or marked not mutable meanwhile keeps what
	// it has now.
	var changed []string
	err = state.UpdateBuildEvidence(root, recipeID, func(ev *state.BuildEvidence) error {
		for _, inv := range order {
			m, f := results[inv], ev.Falsifiers[inv]
			if f == nil || f.Command != m.Command || f.Mutation == nil || f.Mutation.Patch != m.Patch || f.NotMutable != "" {
				changed = append(changed, inv)
				delete(results, inv)
				continue
			}
			f.Mutation = m
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("save build evidence: %w", err)
	}
	if len(changed) > 0 {
		fmt.Fprintf(sink, "\n[bts] %s recorded again while the mutations ran; the runs here were not recorded\n", strings.Join(changed, ", "))
	}
	killed := 0
	for _, m := range results {
		if m.Killed() {
			killed++
		}
	}
	hash, _ := engine.CodeHash(root, recipeID)
	appendEvidenceChangelog(root, recipeID, "mutate", fmt.Sprintf("rerun: %d of %d killed", killed, len(results)), hash)
	return results, nil
}

// loadFalsifierFor returns the recorded falsifier of an invariant declared
// in domain.md, with the evidence it belongs to.
func loadFalsifierFor(root, recipeID, inv string) (*state.Falsifier, *state.BuildEvidence, error) {
	if _, err := state.LoadRecipeState(root, recipeID); err != nil {
		return nil, nil, fmt.Errorf("load recipe %s: %w", recipeID, err)
	}
	if inv == "" {
		return nil, nil, errors.New("--inv is required")
	}
	ids, err := engine.DomainInvariantIDs(filepath.Join(state.RecipeDir(root, recipeID), "domain.md"))
	if err != nil {
		return nil, nil, fmt.Errorf("read domain.md invariants: %w", err)
	}
	if !containsString(ids, inv) {
		return nil, nil, fmt.Errorf("%s is not an invariant in domain.md § 2 (declared: %v)", inv, ids)
	}
	ev, err := state.LoadBuildEvidence(root, recipeID)
	if err != nil {
		return nil, nil, fmt.Errorf("load build evidence: %w", err)
	}
	f := ev.Falsifiers[inv]
	if f == nil || f.Command == "" {
		return nil, nil, fmt.Errorf("%s has no falsifier recorded; record it with `bts falsifier run %s --inv %s --cmd \"<test>\"`", inv, recipeID, inv)
	}
	return f, ev, nil
}

// checkMutationPatch returns the files a patch changes, and refuses a patch
// that changes none of the recipe's code files, or that changes a test: a
// mutation must break the guard, not the test that watches it.
func checkMutationPatch(root, recipeID string, patch []byte) ([]string, error) {
	files, err := patchFiles(patch)
	if err != nil {
		return nil, err
	}
	code := map[string]bool{}
	for _, c := range engine.CodeFiles(root, recipeID) {
		code[c] = true
	}
	touchesCode := false
	for _, p := range files {
		if isTestPath(p) {
			return nil, fmt.Errorf("the patch changes %s, a test: a mutation breaks the code's guard, never the test that watches it", p)
		}
		if code[p] {
			touchesCode = true
		}
	}
	if !touchesCode {
		return nil, fmt.Errorf("the patch changes none of the recipe's code files (%s); break the guard where the code enforces the invariant", strings.Join(files, ", "))
	}
	return files, nil
}

// patchFiles lists the paths a git-style unified diff changes.
func patchFiles(patch []byte) ([]string, error) {
	var files []string
	seen := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(patch))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		var p, prefix string
		switch {
		case strings.HasPrefix(line, "--- "):
			p, prefix = line[4:], "a/"
		case strings.HasPrefix(line, "+++ "):
			p, prefix = line[4:], "b/"
		default:
			continue
		}
		if i := strings.IndexByte(p, '\t'); i >= 0 {
			p = p[:i]
		}
		p = strings.TrimSpace(p)
		if p == "/dev/null" {
			continue
		}
		if !strings.HasPrefix(p, prefix) {
			return nil, fmt.Errorf("patch path %q has no %q prefix; write a git-style diff (git diff, or diff -u --label a/<path> --label b/<path>)", p, prefix)
		}
		p = filepath.ToSlash(filepath.Clean(strings.TrimPrefix(p, prefix)))
		if !seen[p] {
			seen[p] = true
			files = append(files, p)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read patch: %w", err)
	}
	if len(files) == 0 {
		return nil, errors.New("the patch names no files; write a unified diff")
	}
	return files, nil
}

// isTestPath reports whether a path looks like a test or test support
// file. It lives in engine, where the interrogate rescope rule needs it too.
func isTestPath(p string) bool { return engine.IsTestPath(p) }

// runMutation copies the project, applies the patch to the copy, runs the
// setup and then the falsifier there, and records the run. The project on
// disk is never touched.
func runMutation(root, recipeID, inv, falsifierCmd string, patch []byte, patchRel string, files []string, expect, setup string, timeoutSec int, sink io.Writer) (*state.MutationRun, error) {
	baseHash, herr := engine.CodeHash(root, recipeID)
	if herr != nil {
		fmt.Fprintf(os.Stderr, "[bts] warning: code fingerprint: %v (this run records none)\n", herr)
	}
	dir, err := os.MkdirTemp("", "bts-mutate-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	isolation, err := copyProjectTo(root, dir)
	if err != nil {
		return nil, fmt.Errorf("copy project: %w", err)
	}
	apply := exec.Command("git", "apply", "--whitespace=nowarn", "-")
	apply.Dir = dir
	apply.Stdin = bytes.NewReader(patch)
	if out, err := apply.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("the patch does not apply to the current code: %v: %s", err, strings.TrimSpace(string(out)))
	}
	var output bytes.Buffer
	if setup != "" {
		fmt.Fprintf(sink, "[bts] setup in the copy: %s\n", setup)
		s, err := runShellCommand(dir, setup, timeoutSec, sink)
		if err != nil {
			return nil, fmt.Errorf("setup %q: %w", setup, err)
		}
		if s.ExitCode != 0 || s.TimedOut {
			return nil, fmt.Errorf("setup %q failed in the copy (exit=%d); the falsifier was not run", setup, s.ExitCode)
		}
	}
	run, err := runShellCommand(dir, falsifierCmd, timeoutSec, sink)
	if err != nil {
		return nil, fmt.Errorf("run %q: %w", falsifierCmd, err)
	}
	output.Write(run.Output)
	logDir := filepath.Join(state.LocalPath(root), "recipes", recipeID)
	if err := os.MkdirAll(logDir, 0755); err == nil {
		_ = os.WriteFile(filepath.Join(logDir, "mutation-"+inv+".log"), output.Bytes(), 0644)
	}
	return &state.MutationRun{
		Patch:     patchRel,
		PatchHash: state.ContentHash(patch),
		Files:     files,
		Setup:     setup,
		Command:   falsifierCmd,
		ExitCode:  run.ExitCode,
		TimedOut:  run.TimedOut,
		Expect:    expect,
		Matched:   bytes.Contains(output.Bytes(), []byte(expect)),
		BaseHash:  baseHash,
		Isolation: isolation,
		RunAt:     time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// copyProjectTo copies the project's files into dir. In a git repository
// it copies what `git ls-files -co --exclude-standard` lists: tracked and
// untracked files, without ignored or excluded ones, so uncommitted code is
// included and virtualenvs and build output are not. Elsewhere it walks
// the tree. .git and .bts are never copied.
func copyProjectTo(root, dir string) (string, error) {
	if list, err := exec.Command("git", "-C", root, "ls-files", "-co", "--exclude-standard", "-z").Output(); err == nil {
		for _, rel := range strings.Split(string(list), "\x00") {
			if rel == "" || skipInCopy(rel) {
				continue
			}
			if err := copyOne(filepath.Join(root, rel), filepath.Join(dir, rel)); err != nil {
				return "", err
			}
		}
		return "git-ls-files", nil
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if rel == "." {
			return nil
		}
		if skipInCopy(filepath.ToSlash(rel)) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		return copyOne(path, filepath.Join(dir, rel))
	})
	if err != nil {
		return "", err
	}
	return "walk", nil
}

func skipInCopy(rel string) bool {
	first := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
	return first == ".git" || first == ".bts"
}

func copyOne(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // listed but deleted in the working tree
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, info.Mode().Perm())
}

func mutationSummary(inv string, m *state.MutationRun) string {
	verdict := "killed"
	if !m.Killed() {
		verdict = "not killed"
	}
	return fmt.Sprintf("%s %s: exit=%d expect-matched=%v (%s)", inv, verdict, m.ExitCode, m.Matched, m.Patch)
}

func mutationError(inv string, m *state.MutationRun) error {
	switch {
	case m.TimedOut:
		return fmt.Errorf("%s: %w", inv, errMutationTimedOut)
	case m.ExitCode == 0:
		return fmt.Errorf("%s: %w", inv, errMutationSurvived)
	case !m.Matched:
		return fmt.Errorf("%s: %w", inv, errMutationUnmatched)
	}
	return nil
}

func sortedMutationKeys(m map[string]*state.MutationRun) []string {
	ev := &state.BuildEvidence{Falsifiers: map[string]*state.Falsifier{}}
	for k := range m {
		ev.Falsifiers[k] = nil
	}
	return engine.FalsifierIDs(ev)
}
