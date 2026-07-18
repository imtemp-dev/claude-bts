package engine

import (
	"fmt"
	"strings"
)

// Line diff for /bts-verify focus hints — changes since the last
// verified snapshot. Precision matters less than boundedness here:
// the output lands in an LLM prompt, so it must stay compact and must
// never silently truncate without saying so.

const (
	// maxLCSLines bounds the O(n·m) LCS table. Beyond this the middle
	// is emitted as one coarse replace-hunk instead of a minimal diff.
	maxLCSLines = 800
	// diffContextLines of unchanged context around each hunk.
	diffContextLines = 2
	// maxDiffOutputLines caps rendered output (prompt budget guard).
	maxDiffOutputLines = 300
)

// UnifiedLineDiff renders a unified-style diff from oldContent to
// newContent. Returns the rendered diff and whether any change exists.
func UnifiedLineDiff(oldContent, newContent string) (string, bool) {
	if oldContent == newContent {
		return "", false
	}
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	// Trim common prefix/suffix — iteration edits are usually localized.
	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(oldLines)-prefix && suffix < len(newLines)-prefix &&
		oldLines[len(oldLines)-1-suffix] == newLines[len(newLines)-1-suffix] {
		suffix++
	}
	oldMid := oldLines[prefix : len(oldLines)-suffix]
	newMid := newLines[prefix : len(newLines)-suffix]

	var ops []diffOp
	if len(oldMid) > maxLCSLines || len(newMid) > maxLCSLines {
		// Coarse fallback: one replace hunk covering the whole middle.
		ops = coarseOps(oldMid, newMid)
	} else {
		ops = lcsOps(oldMid, newMid)
	}

	return renderHunks(oldLines, newLines, ops, prefix), true
}

type diffOp struct {
	kind    byte // '-', '+'
	oldIdx  int  // index within the middle slice (for '-')
	newIdx  int  // index within the middle slice (for '+')
	content string
}

func coarseOps(oldMid, newMid []string) []diffOp {
	ops := make([]diffOp, 0, len(oldMid)+len(newMid))
	for i, l := range oldMid {
		ops = append(ops, diffOp{kind: '-', oldIdx: i, content: l})
	}
	for i, l := range newMid {
		ops = append(ops, diffOp{kind: '+', oldIdx: len(oldMid), newIdx: i, content: l})
	}
	return ops
}

// lcsOps computes delete/insert operations via a standard LCS table.
func lcsOps(oldMid, newMid []string) []diffOp {
	n, m := len(oldMid), len(newMid)
	table := make([][]int, n+1)
	for i := range table {
		table[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if oldMid[i] == newMid[j] {
				table[i][j] = table[i+1][j+1] + 1
			} else if table[i+1][j] >= table[i][j+1] {
				table[i][j] = table[i+1][j]
			} else {
				table[i][j] = table[i][j+1]
			}
		}
	}
	var ops []diffOp
	i, j := 0, 0
	for i < n && j < m {
		if oldMid[i] == newMid[j] {
			i++
			j++
		} else if table[i+1][j] >= table[i][j+1] {
			ops = append(ops, diffOp{kind: '-', oldIdx: i, newIdx: j, content: oldMid[i]})
			i++
		} else {
			ops = append(ops, diffOp{kind: '+', oldIdx: i, newIdx: j, content: newMid[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{kind: '-', oldIdx: i, newIdx: m, content: oldMid[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{kind: '+', oldIdx: n, newIdx: j, content: newMid[j]})
	}
	return ops
}

// renderHunks groups ops into hunks with context and caps output length.
func renderHunks(oldLines, newLines []string, ops []diffOp, prefix int) string {
	var b strings.Builder
	written := 0
	truncated := false
	write := func(line string) {
		if truncated {
			return
		}
		if written >= maxDiffOutputLines {
			truncated = true
			return
		}
		b.WriteString(line)
		b.WriteByte('\n')
		written++
	}

	// Group consecutive ops into hunks: ops whose middle positions are
	// within 2*context of each other share a hunk.
	type hunk struct{ ops []diffOp }
	var hunks []hunk
	for _, op := range ops {
		pos := op.oldIdx
		if len(hunks) > 0 {
			last := hunks[len(hunks)-1].ops
			lastPos := last[len(last)-1].oldIdx
			if pos-lastPos <= diffContextLines*2 {
				hunks[len(hunks)-1].ops = append(hunks[len(hunks)-1].ops, op)
				continue
			}
		}
		hunks = append(hunks, hunk{ops: []diffOp{op}})
	}

	for _, h := range hunks {
		firstOld := h.ops[0].oldIdx + prefix
		lastOld := h.ops[len(h.ops)-1].oldIdx + prefix
		ctxStart := firstOld - diffContextLines
		if ctxStart < 0 {
			ctxStart = 0
		}
		// 1-based line numbers in the NEW document for hunk header.
		newStart := h.ops[0].newIdx + prefix + 1
		write(fmt.Sprintf("@@ around new-file line %d @@", newStart))
		for k := ctxStart; k < firstOld; k++ {
			if k < len(oldLines) {
				write("  " + oldLines[k])
			}
		}
		for _, op := range h.ops {
			write(string(op.kind) + " " + op.content)
		}
		// An insert at oldIdx=i precedes old line i, which is itself
		// unchanged — start after-context there rather than skipping it.
		afterStart := lastOld + 1
		if h.ops[len(h.ops)-1].kind == '+' {
			afterStart = lastOld
		}
		for k := afterStart; k < afterStart+diffContextLines && k < len(oldLines); k++ {
			write("  " + oldLines[k])
		}
	}
	if truncated {
		fmt.Fprintf(&b, "⚠ DIFF TRUNCATED at %d lines — more changes exist; treat focus hints as partial.\n", maxDiffOutputLines)
	}
	return b.String()
}
