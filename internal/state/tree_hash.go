package state

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// TreeHash fingerprints a whole project, for a recipe whose records name no
// code files: a fix recipe writes no tasks.json, and without a fingerprint
// every freshness gate (tests_cover_current_code, interrogate_clean,
// app_proven) would accept a run recorded before the last edit.
//
// It covers the files `git ls-files -co --exclude-standard` lists, the same
// set an interrogate or falsifier copy holds: tracked and untracked, without
// ignored ones, and without .git/, .bts/, .claude/ and .verify/, where bts
// state, the agent's skills and drive evidence live. Each file is named by
// its git blob id, taken from the index when the working tree matches it and
// hashed by `git hash-object` when it does not, so a commit, a stage or a
// touch does not read as a change and an edit, a new file or a delete does.
// Unlike ImplHash it cannot tell the recipe's code from unrelated work: any
// edit in the tree reads as drift, and the remedy is the same cheap re-run.
//
// It returns "" outside a git repository, or when git lists no file.
func TreeHash(root string) (string, error) {
	staged, err := exec.Command("git", "-C", root, "ls-files", "-s", "-z").Output()
	if err != nil {
		return "", nil // not a git repository: no fingerprint, as before
	}
	ids := map[string]string{}
	for _, rec := range strings.Split(string(staged), "\x00") {
		// "<mode> <blob> <stage>\t<path>"
		meta, path, ok := strings.Cut(rec, "\t")
		if !ok || treeHashSkips(path) {
			continue
		}
		if f := strings.Fields(meta); len(f) == 3 {
			ids[path] = f[1]
		}
	}
	// Files whose working copy may differ from the index, and untracked
	// files. -m can list a file whose stat changed but content did not;
	// hashing it gives the index's id back, so that is no change.
	changed, err := exec.Command("git", "-C", root, "ls-files", "-m", "-o", "--exclude-standard", "-z").Output()
	if err != nil {
		return "", fmt.Errorf("git ls-files: %w", err)
	}
	var files []string
	for _, path := range strings.Split(string(changed), "\x00") {
		if treeHashSkips(path) {
			continue
		}
		info, serr := os.Lstat(filepath.Join(root, path))
		switch {
		case serr != nil:
			ids[path] = "absent"
		case info.Mode()&os.ModeSymlink != 0:
			target, _ := os.Readlink(filepath.Join(root, path))
			ids[path] = "symlink:" + target
		case info.Mode().IsRegular():
			files = append(files, path)
		}
		// A directory (an untracked repository inside this one) is skipped.
	}
	if len(files) > 0 {
		cmd := exec.Command("git", "-C", root, "hash-object", "--stdin-paths")
		cmd.Stdin = strings.NewReader(strings.Join(files, "\n") + "\n")
		out, herr := cmd.Output()
		if herr != nil {
			return "", fmt.Errorf("git hash-object: %w", herr)
		}
		blobs := strings.Fields(string(out))
		if len(blobs) != len(files) {
			return "", fmt.Errorf("git hash-object: %d ids for %d files", len(blobs), len(files))
		}
		for i, path := range files {
			ids[path] = blobs[i]
		}
	}
	if len(ids) == 0 {
		return "", nil
	}
	lines := make([]string, 0, len(ids))
	for path, id := range ids {
		lines = append(lines, path+"\x00"+id)
	}
	sort.Strings(lines)
	return ContentHash([]byte(strings.Join(lines, "\n"))), nil
}

// treeHashSkips is the part of the tree TreeHash leaves out: git's own
// directory, bts state, the agent's skills and hooks, and drive evidence.
func treeHashSkips(path string) bool {
	if path == "" || strings.HasSuffix(path, "/") {
		return true
	}
	switch strings.SplitN(path, "/", 2)[0] {
	case ".git", ".bts", ".claude", ".verify":
		return true
	}
	return false
}
