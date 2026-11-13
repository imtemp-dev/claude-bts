package state

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// SeenContent reads back the code a CodeSeen recorded, and the files of
// any commit, through one `git cat-file --batch` process.  Open it with
// CodeSeen.OpenContent; Close it when done.
type SeenContent struct {
	root string
	seen *CodeSeen
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Reader
}

// OpenContent opens a reader for what s recorded.  It returns nil when
// there is nothing to read back: outside a git repository, or a Files
// record written before content was kept (no Blobs and no Base).
func (s *CodeSeen) OpenContent(root string) (*SeenContent, error) {
	if s == nil || !inGitRepo(root) {
		return nil, nil
	}
	if !s.Tree && len(s.Blobs) == 0 && s.Base == "" {
		return nil, nil
	}
	cmd := exec.Command("git", "-C", root, "cat-file", "--batch")
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("git cat-file: %w", err)
	}
	return &SeenContent{root: root, seen: s, cmd: cmd, in: in, out: bufio.NewReader(stdout)}, nil
}

// Close ends the reader's git process.
func (c *SeenContent) Close() {
	if c == nil {
		return
	}
	_ = c.in.Close()
	_ = c.cmd.Wait()
}

// Seen returns path as the adversaries saw it: its content and whether it
// existed.  ok is false when the record cannot tell: a Files record that
// did not list the path or kept no blob for it, a symlink, or a blob the
// object store no longer holds.
func (c *SeenContent) Seen(path string) (content []byte, present, ok bool, err error) {
	s := c.seen
	if !s.Tree {
		h, listed := s.Files[path]
		if !listed {
			return nil, false, false, nil
		}
		if h == seenAbsent {
			return nil, false, true, nil
		}
		blob := s.Blobs[path]
		if blob == "" {
			return nil, false, false, nil
		}
		data, found, err := c.object(blob)
		return data, true, found, err
	}
	if v, listed := s.Overlay[path]; listed {
		switch {
		case v == seenAbsent:
			return nil, false, true, nil
		case v == "" || strings.HasPrefix(v, "symlink:"):
			return nil, false, false, nil
		}
		data, found, err := c.object(v)
		return data, true, found, err
	}
	// Not in the overlay: the file was Base's version, or absent from it.
	if s.Base == "" {
		return nil, false, true, nil
	}
	data, found, err := c.At(s.Base, path)
	return data, found, true, err
}

// At returns path's content in commit rev, and whether rev has it.
func (c *SeenContent) At(rev, path string) ([]byte, bool, error) {
	return c.object(rev + ":" + filepath.ToSlash(path))
}

// object reads one object by name; found is false when git has none.
func (c *SeenContent) object(name string) ([]byte, bool, error) {
	if strings.ContainsAny(name, "\n\r") {
		return nil, false, fmt.Errorf("object name %q holds a newline", name)
	}
	if _, err := io.WriteString(c.in, name+"\n"); err != nil {
		return nil, false, fmt.Errorf("git cat-file: %w", err)
	}
	header, err := c.out.ReadString('\n')
	if err != nil {
		return nil, false, fmt.Errorf("git cat-file: %w", err)
	}
	fields := strings.Fields(header)
	if len(fields) == 2 && (fields[1] == "missing" || fields[1] == "ambiguous") {
		return nil, false, nil
	}
	if len(fields) != 3 {
		return nil, false, fmt.Errorf("git cat-file: unexpected header %q", strings.TrimSpace(header))
	}
	size, err := strconv.Atoi(fields[2])
	if err != nil {
		return nil, false, fmt.Errorf("git cat-file: size in %q", strings.TrimSpace(header))
	}
	data := make([]byte, size+1) // the content and its trailing newline
	if _, err := io.ReadFull(c.out, data); err != nil {
		return nil, false, fmt.Errorf("git cat-file: %w", err)
	}
	if fields[1] != "blob" {
		return nil, false, nil
	}
	return data[:size], true, nil
}

// UpstreamMove is how the upstream base under the recipe's commits moved
// between the copies and now: From is where HEAD forked from upstream
// when the adversaries copied the code, To where it forks now.  Both are
// empty when it did not move, or when bts cannot tell (no upstream ref,
// or the recorded HEAD is gone).
type UpstreamMove struct {
	From, To string
}

// Moved reports whether a rebase or merge brought upstream changes in.
func (m UpstreamMove) Moved() bool { return m.From != "" && m.To != "" && m.From != m.To }

// FindUpstreamMove compares where seenHead and HEAD fork from the
// remote's default branch (origin/HEAD, else origin/main or
// origin/master).
func FindUpstreamMove(root, seenHead string) UpstreamMove {
	if seenHead == "" {
		return UpstreamMove{}
	}
	up := upstreamRef(root)
	head := gitHead(root)
	if up == "" || head == "" || head == seenHead {
		return UpstreamMove{}
	}
	from, to := mergeBase(root, seenHead, up), mergeBase(root, head, up)
	if from == "" || to == "" {
		return UpstreamMove{}
	}
	return UpstreamMove{From: from, To: to}
}

// upstreamRef is the remote default branch the recipe's branch forks
// from, "" when there is none.
func upstreamRef(root string) string {
	if out, err := exec.Command("git", "-C", root, "symbolic-ref", "-q", "refs/remotes/origin/HEAD").Output(); err == nil {
		if ref := strings.TrimSpace(string(out)); ref != "" {
			return ref
		}
	}
	for _, ref := range []string{"refs/remotes/origin/main", "refs/remotes/origin/master"} {
		if exec.Command("git", "-C", root, "rev-parse", "--verify", "-q", ref).Run() == nil {
			return ref
		}
	}
	return ""
}

func mergeBase(root, a, b string) string {
	out, err := exec.Command("git", "-C", root, "merge-base", a, b).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// MergeFile carries an upstream change onto what was seen: a three-way
// merge of seen and theirs over their common base, as `git merge-file`
// does it.  ok is false when the two conflict.
func MergeFile(seen, base, theirs []byte) (merged []byte, ok bool, err error) {
	dir, err := os.MkdirTemp("", "bts-merge-")
	if err != nil {
		return nil, false, err
	}
	defer os.RemoveAll(dir)
	paths := []string{filepath.Join(dir, "seen"), filepath.Join(dir, "base"), filepath.Join(dir, "theirs")}
	for i, data := range [][]byte{seen, base, theirs} {
		if err := os.WriteFile(paths[i], data, 0600); err != nil {
			return nil, false, err
		}
	}
	cmd := exec.Command("git", "merge-file", "-p", "-q", paths[0], paths[1], paths[2])
	var out bytes.Buffer
	cmd.Stdout = &out
	err = cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return out.Bytes(), true, nil
	case errors.As(err, &exit) && exit.ExitCode() > 0 && exit.ExitCode() < 128:
		return nil, false, nil // conflicts
	default:
		return nil, false, fmt.Errorf("git merge-file: %w", err)
	}
}

// DiffHunks returns the hunks that turn a into b, as `git diff -U0`
// reports them: for each, the lines removed from a and the lines added in
// b, 1-based.  A hunk that only removes lines has an empty added range
// whose Start is the line of b it follows.
func DiffHunks(a, b []byte) ([]Hunk, error) {
	dir, err := os.MkdirTemp("", "bts-diff-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	pa, pb := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	if err := os.WriteFile(pa, a, 0600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(pb, b, 0600); err != nil {
		return nil, err
	}
	cmd := exec.Command("git", "diff", "--no-index", "--no-color", "--no-ext-diff", "-U0", pa, pb)
	out, err := cmd.Output()
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 1) {
		return nil, fmt.Errorf("git diff: %w", err)
	}
	var hunks []Hunk
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "@@ ") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		oldStart, oldLen := hunkRange(parts[1])
		newStart, newLen := hunkRange(parts[2])
		hunks = append(hunks, Hunk{OldStart: oldStart, OldLen: oldLen, NewStart: newStart, NewLen: newLen})
	}
	return hunks, nil
}

// Hunk is one changed region: OldLen lines of the old file from OldStart
// became NewLen lines of the new one from NewStart.
type Hunk struct {
	OldStart, OldLen, NewStart, NewLen int
}

// hunkRange parses "-12,3" or "+12" into a start and a length.
func hunkRange(s string) (start, n int) {
	s = strings.TrimLeft(s, "-+")
	n = 1
	if i := strings.IndexByte(s, ','); i >= 0 {
		n, _ = strconv.Atoi(s[i+1:])
		s = s[:i]
	}
	start, _ = strconv.Atoi(s)
	return start, n
}
