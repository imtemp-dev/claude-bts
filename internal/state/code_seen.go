package state

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// CodeSeen records the code an interrogate round's adversaries took their
// copies of, file by file, so a later gate can tell how much of it changed
// since (trial problem #52). A run's single code hash says only that
// something changed; a change after the adversaries looked is attacked
// again only when it is large, and telling large from small needs to know
// which files changed.
//
// It takes one of two forms, matching engine.CodeHash:
//   - Files, when the recipe names its code files (tasks.json or the
//     wireframe): each file's ContentHash, "absent" for one that did not
//     exist yet.
//   - Tree, for a recipe that names none (a fix recipe): Base is the
//     commit HEAD named, and Overlay holds the git blob id of every file
//     that differed from it, "absent" for a deleted one. A record of the
//     whole tree would copy every path of a large repository into
//     interrogate.json; the overlay is the size of the change. Base is
//     empty in a repository with no commits, and Overlay then holds every
//     file.
//
// In a git repository both forms also keep what the adversaries saw, not
// only its fingerprint: the overlay's blobs and the Files form's Blobs are
// written to the object store, and Base names HEAD in the Files form too.
// A later check can then diff the code against it, leave out a change to
// comments only (trial problem #140) and carry an upstream change brought
// in by a rebase onto what was seen (trial problem #139).  A record
// written before, or outside git, has no content and is compared by
// fingerprint alone.
type CodeSeen struct {
	Files map[string]string `json:"files,omitempty"`
	// Blobs is the git blob id of each file in Files that existed, written
	// to the repository's object store.
	Blobs   map[string]string `json:"blobs,omitempty"`
	Tree    bool              `json:"tree,omitempty"`
	Base    string            `json:"base,omitempty"`
	Overlay map[string]string `json:"overlay,omitempty"`
}

// CodeChange is what changed since a CodeSeen: Added holds files that
// exist now and were absent from what the adversaries saw, Changed holds
// files they saw whose content differs now (deleted ones included). Both
// sorted.
//
// engine.InterrogateDrift refines a raw change when the record kept the
// content: a file whose code is unchanged and whose comments or blank
// lines changed moves to CommentOnly, one that differs only by what a
// rebase or merge brought in from upstream moves to Upstream, and Hunks
// gives the changed line ranges of each file left in Changed.
type CodeChange struct {
	Added       []string               `json:"added,omitempty"`
	Changed     []string               `json:"changed,omitempty"`
	CommentOnly []string               `json:"comment_only,omitempty"`
	Upstream    []string               `json:"upstream,omitempty"`
	Hunks       map[string][]LineRange `json:"hunks,omitempty"`
}

// LineRange is a run of lines in a file, 1-based and inclusive.
type LineRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// String is "12" or "12-30".
func (r LineRange) String() string {
	if r.End <= r.Start {
		return fmt.Sprintf("%d", r.Start)
	}
	return fmt.Sprintf("%d-%d", r.Start, r.End)
}

// Files returns Added and Changed together, sorted.
func (c CodeChange) Files() []string {
	out := append(append([]string(nil), c.Added...), c.Changed...)
	sort.Strings(out)
	return out
}

const seenAbsent = "absent"

// SeeFiles records the content hash of each file, relative to root, and
// in a git repository the content itself (Blobs) and HEAD (Base).
func SeeFiles(root string, files []string) (*CodeSeen, error) {
	seen := &CodeSeen{Files: map[string]string{}}
	var present []string
	for _, f := range files {
		h, err := seenFileHash(root, f)
		if err != nil {
			return nil, err
		}
		seen.Files[f] = h
		if h != seenAbsent {
			present = append(present, f)
		}
	}
	if !inGitRepo(root) {
		return seen, nil
	}
	seen.Base = gitHead(root)
	if len(present) > 0 {
		blobs, err := hashObjects(root, present, true)
		if err != nil {
			return nil, err
		}
		seen.Blobs = map[string]string{}
		for i, f := range present {
			seen.Blobs[f] = blobs[i]
		}
	}
	return seen, nil
}

// inGitRepo reports whether root is inside a git work tree.
func inGitRepo(root string) bool {
	_, err := exec.Command("git", "-C", root, "rev-parse", "--git-dir").Output()
	return err == nil
}

// gitHead is the commit HEAD names, "" before the first commit.
func gitHead(root string) string {
	out, err := exec.Command("git", "-C", root, "rev-parse", "--verify", "-q", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// hashObjects returns the git blob id of each path, relative to root,
// writing the blobs to the object store when write is set.
func hashObjects(root string, paths []string, write bool) ([]string, error) {
	args := []string{"-C", root, "hash-object"}
	if write {
		args = append(args, "-w")
	}
	cmd := exec.Command("git", append(args, "--stdin-paths")...)
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\n") + "\n")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git hash-object: %w", err)
	}
	blobs := strings.Fields(string(out))
	if len(blobs) != len(paths) {
		return nil, fmt.Errorf("git hash-object: %d ids for %d files", len(blobs), len(paths))
	}
	return blobs, nil
}

// SeeTree records the git tree as HEAD plus the files that differ from it.
// It returns nil outside a git repository, where TreeHash records no
// fingerprint either.
func SeeTree(root string) (*CodeSeen, error) {
	if !inGitRepo(root) {
		return nil, nil
	}
	base := gitHead(root)
	overlay, _, err := treeOverlay(root, base, true)
	if err != nil {
		return nil, err
	}
	return &CodeSeen{Tree: true, Base: base, Overlay: overlay}, nil
}

// Since compares the code on disk with what was seen. files is the
// recipe's current code file list; a tree record ignores it.
func (s *CodeSeen) Since(root string, files []string) (CodeChange, error) {
	if s.Tree {
		return s.treeSince(root)
	}
	var c CodeChange
	paths := map[string]bool{}
	for _, f := range files {
		paths[f] = true
	}
	for f := range s.Files {
		paths[f] = true
	}
	for f := range paths {
		now, err := seenFileHash(root, f)
		if err != nil {
			return CodeChange{}, err
		}
		then, saw := s.Files[f]
		switch {
		case (!saw || then == seenAbsent) && now != seenAbsent:
			c.Added = append(c.Added, f)
		case saw && then != now:
			c.Changed = append(c.Changed, f)
		}
	}
	sort.Strings(c.Added)
	sort.Strings(c.Changed)
	return c, nil
}

// treeSince compares the tree now with the recorded one. A path in
// neither overlay is the same as at Base both times, so only the two
// overlays need reading: a path missing from one of them is Base's
// version of the file there.
func (s *CodeSeen) treeSince(root string) (CodeChange, error) {
	now, addedToBase, err := treeOverlay(root, s.Base, false)
	if err != nil {
		return CodeChange{}, err
	}
	var c CodeChange
	paths := map[string]bool{}
	for p := range s.Overlay {
		paths[p] = true
	}
	for p := range now {
		paths[p] = true
	}
	for p := range paths {
		then, thenListed := s.Overlay[p]
		cur, curListed := now[p]
		switch {
		case thenListed && curListed:
			if then == cur {
				continue
			}
			if then == seenAbsent {
				c.Added = append(c.Added, p)
			} else {
				c.Changed = append(c.Changed, p)
			}
		case curListed:
			// Then it was Base's version: absent when Base lacks it.
			if addedToBase[p] {
				c.Added = append(c.Added, p)
			} else {
				c.Changed = append(c.Changed, p)
			}
		default:
			// Now it is Base's version again.
			if then == seenAbsent {
				c.Added = append(c.Added, p)
			} else {
				c.Changed = append(c.Changed, p)
			}
		}
	}
	sort.Strings(c.Added)
	sort.Strings(c.Changed)
	return c, nil
}

// treeOverlay lists the files that differ from base, with their blob id
// ("absent" for a deleted one), and which of them base does not have. It
// leaves out what TreeHash leaves out. With no base, every file is listed
// and none is in base. write stores the listed files' blobs.
func treeOverlay(root, base string, write bool) (overlay map[string]string, addedToBase map[string]bool, err error) {
	overlay, addedToBase = map[string]string{}, map[string]bool{}
	var present []string
	note := func(path string, added bool) {
		if treeHashSkips(path) {
			return
		}
		if added {
			addedToBase[path] = true
		}
		info, serr := os.Lstat(filepath.Join(root, path))
		switch {
		case serr != nil:
			overlay[path] = seenAbsent
		case info.Mode()&os.ModeSymlink != 0:
			target, _ := os.Readlink(filepath.Join(root, path))
			overlay[path] = "symlink:" + target
		case info.Mode().IsRegular():
			overlay[path] = ""
			present = append(present, path)
		}
	}
	if base == "" {
		out, gerr := exec.Command("git", "-C", root, "ls-files", "-c", "-o", "--exclude-standard", "-z").Output()
		if gerr != nil {
			return nil, nil, fmt.Errorf("git ls-files: %w", gerr)
		}
		for _, p := range strings.Split(string(out), "\x00") {
			note(p, true)
		}
	} else {
		// Working tree against base; tracked paths only, so untracked
		// files are listed next.
		out, gerr := exec.Command("git", "-C", root, "diff", "--name-status", "--no-renames", "-z", base).Output()
		if gerr != nil {
			return nil, nil, fmt.Errorf("git diff %s: %w", base, gerr)
		}
		fields := strings.Split(string(out), "\x00")
		for i := 0; i+1 < len(fields); i += 2 {
			note(fields[i+1], strings.HasPrefix(fields[i], "A"))
		}
		out, gerr = exec.Command("git", "-C", root, "ls-files", "-o", "--exclude-standard", "-z").Output()
		if gerr != nil {
			return nil, nil, fmt.Errorf("git ls-files: %w", gerr)
		}
		for _, p := range strings.Split(string(out), "\x00") {
			note(p, true)
		}
	}
	if len(present) > 0 {
		blobs, herr := hashObjects(root, present, write)
		if herr != nil {
			return nil, nil, herr
		}
		for i, p := range present {
			overlay[p] = blobs[i]
		}
	}
	return overlay, addedToBase, nil
}

func seenFileHash(root, rel string) (string, error) {
	p := rel
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, filepath.FromSlash(rel))
	}
	h, ok, err := FileContentHash(p)
	if err != nil {
		return "", fmt.Errorf("hash %s: %w", rel, err)
	}
	if !ok {
		return seenAbsent, nil
	}
	return h, nil
}
