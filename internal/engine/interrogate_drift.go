package engine

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// refineDrift reads each file of a raw change against the code the
// adversaries saw, when the record kept it, and sorts it:
//
//   - a file whose only difference is what a rebase or merge brought in
//     from upstream goes to Upstream (trial problem #139): the upstream
//     change is carried onto the seen content by a three-way merge first,
//     so what is left is the recipe's own change;
//   - a file whose code is the same and whose comments, docstrings or blank
//     lines changed goes to CommentOnly (trial problem #140);
//   - any other stays Added or Changed, and a changed file gets the line
//     ranges of its code hunks.
//
// A file the record cannot read back, or whose upstream change conflicts
// with the recipe's, stays as the raw change had it: counted.  A reader
// that fails to start leaves the whole change as it was.
func refineDrift(root string, seen *state.CodeSeen, raw state.CodeChange) state.CodeChange {
	rd, err := seen.OpenContent(root)
	if err != nil || rd == nil {
		return raw
	}
	defer rd.Close()
	move := state.FindUpstreamMove(root, seen.Base)

	out := state.CodeChange{}
	keep := func(path string, added bool) {
		if added {
			out.Added = append(out.Added, path)
		} else {
			out.Changed = append(out.Changed, path)
		}
	}
	type cand struct {
		path  string
		added bool
	}
	var cands []cand
	for _, p := range raw.Added {
		cands = append(cands, cand{p, true})
	}
	for _, p := range raw.Changed {
		cands = append(cands, cand{p, false})
	}
	for _, c := range cands {
		then, thenPresent, ok, err := rd.Seen(c.path)
		if err != nil || !ok {
			keep(c.path, c.added)
			continue
		}
		upstream := false
		if move.Moved() {
			old, oldPresent, err1 := rd.At(move.From, c.path)
			cur, curPresent, err2 := rd.At(move.To, c.path)
			if err1 != nil || err2 != nil {
				keep(c.path, c.added)
				continue
			}
			if oldPresent != curPresent || !bytes.Equal(old, cur) {
				upstream = true
				then, thenPresent, ok = carryUpstream(then, thenPresent, old, oldPresent, cur, curPresent)
				if !ok {
					keep(c.path, c.added)
					continue
				}
			}
		}
		now, nowPresent, err := readWorkFile(root, c.path)
		if err != nil {
			keep(c.path, c.added)
			continue
		}
		switch {
		case thenPresent == nowPresent && bytes.Equal(then, now):
			if upstream {
				out.Upstream = append(out.Upstream, c.path)
			}
		case !thenPresent:
			out.Added = append(out.Added, c.path)
		case !nowPresent:
			out.Changed = append(out.Changed, c.path)
		case CodeEquivalent(c.path, then, now):
			out.CommentOnly = append(out.CommentOnly, c.path)
		default:
			out.Changed = append(out.Changed, c.path)
			if ranges := codeHunks(c.path, then, now); len(ranges) > 0 {
				if out.Hunks == nil {
					out.Hunks = map[string][]state.LineRange{}
				}
				out.Hunks[c.path] = ranges
			}
		}
	}
	for _, l := range [][]string{out.Added, out.Changed, out.CommentOnly, out.Upstream} {
		sort.Strings(l)
	}
	return out
}

// carryUpstream applies the upstream change old → cur to what the
// adversaries saw.  ok is false when the two cannot be merged.
func carryUpstream(seen []byte, seenPresent bool, old []byte, oldPresent bool, cur []byte, curPresent bool) ([]byte, bool, bool) {
	switch {
	case seenPresent == oldPresent && bytes.Equal(seen, old):
		// The recipe had not touched the file: it is upstream's now.
		return cur, curPresent, true
	case !seenPresent:
		// The recipe had deleted it; the deletion stands.
		return nil, false, true
	case !curPresent:
		// Upstream deleted a file the recipe changed.
		return nil, false, false
	}
	merged, ok, err := state.MergeFile(seen, old, cur)
	if err != nil || !ok {
		return nil, false, false
	}
	return merged, true, true
}

// readWorkFile reads a file of the working tree; present is false when it
// does not exist.
func readWorkFile(root, rel string) ([]byte, bool, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// rangeGap is how many unchanged lines may separate two hunks reported as
// one range.
const rangeGap = 3

// codeHunks returns the line ranges of now that changed code since then:
// the hunks of a diff whose removed or added lines hold code.  A hunk
// that only removes lines is reported at the line it follows.
func codeHunks(path string, then, now []byte) []state.LineRange {
	hunks, err := state.DiffHunks(then, now)
	if err != nil {
		return nil
	}
	thenCode, nowCode := codeLines(path, then), codeLines(path, now)
	holdsCode := func(lines []string, start, n int) bool {
		for i := start; i < start+n; i++ {
			if i >= 1 && i <= len(lines) && lines[i-1] != "" {
				return true
			}
		}
		return false
	}
	var out []state.LineRange
	for _, h := range hunks {
		if !holdsCode(thenCode, h.OldStart, h.OldLen) && !holdsCode(nowCode, h.NewStart, h.NewLen) {
			continue
		}
		r := state.LineRange{Start: h.NewStart, End: h.NewStart + h.NewLen - 1}
		if h.NewLen == 0 {
			r = state.LineRange{Start: max(h.NewStart, 1), End: max(h.NewStart, 1)}
		}
		if n := len(out); n > 0 && r.Start-out[n-1].End <= rangeGap+1 {
			out[n-1].End = max(out[n-1].End, r.End)
			continue
		}
		out = append(out, r)
	}
	return out
}
