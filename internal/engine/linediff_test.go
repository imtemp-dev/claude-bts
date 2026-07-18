package engine

import (
	"fmt"
	"strings"
	"testing"
)

func TestUnifiedLineDiff_NoChange(t *testing.T) {
	diff, changed := UnifiedLineDiff("a\nb\nc", "a\nb\nc")
	if changed || diff != "" {
		t.Fatalf("expected no change, got changed=%v diff=%q", changed, diff)
	}
}

func TestUnifiedLineDiff_LocalizedEdit(t *testing.T) {
	oldC := "line1\nline2\nline3\nline4\nline5\nline6\nline7"
	newC := "line1\nline2\nline3\nCHANGED\nline5\nline6\nline7"
	diff, changed := UnifiedLineDiff(oldC, newC)
	if !changed {
		t.Fatal("expected change")
	}
	if !strings.Contains(diff, "- line4") || !strings.Contains(diff, "+ CHANGED") {
		t.Errorf("diff missing edit markers:\n%s", diff)
	}
	if strings.Contains(diff, "line1") || strings.Contains(diff, "line7") {
		t.Errorf("diff should not include far context:\n%s", diff)
	}
	if !strings.Contains(diff, "  line3") || !strings.Contains(diff, "  line5") {
		t.Errorf("diff missing near context:\n%s", diff)
	}
}

func TestUnifiedLineDiff_InsertOnly(t *testing.T) {
	oldC := "a\nb\nc\nd\ne"
	newC := "a\nb\nNEW1\nNEW2\nc\nd\ne"
	diff, changed := UnifiedLineDiff(oldC, newC)
	if !changed {
		t.Fatal("expected change")
	}
	if !strings.Contains(diff, "+ NEW1") || !strings.Contains(diff, "+ NEW2") {
		t.Errorf("diff missing inserts:\n%s", diff)
	}
	if strings.Contains(diff, "- ") {
		t.Errorf("insert-only diff has deletions:\n%s", diff)
	}
	// The line the insert precedes is unchanged context, not skipped.
	if !strings.Contains(diff, "  c") {
		t.Errorf("missing after-context line c:\n%s", diff)
	}
}

func TestUnifiedLineDiff_TwoSeparateHunks(t *testing.T) {
	var oldB, newB strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&oldB, "line%d\n", i)
		switch i {
		case 5:
			fmt.Fprintf(&newB, "edited5\n")
		case 30:
			fmt.Fprintf(&newB, "edited30\n")
		default:
			fmt.Fprintf(&newB, "line%d\n", i)
		}
	}
	diff, _ := UnifiedLineDiff(oldB.String(), newB.String())
	if got := strings.Count(diff, "@@ around"); got != 2 {
		t.Errorf("expected 2 hunks, got %d:\n%s", got, diff)
	}
}

func TestUnifiedLineDiff_LargeMiddleCoarseFallback(t *testing.T) {
	// Middles beyond maxLCSLines must not run the LCS (would be huge);
	// they fall back to one coarse replace hunk that still shows content.
	var oldB, newB strings.Builder
	for i := 0; i < maxLCSLines+100; i++ {
		fmt.Fprintf(&oldB, "old%d\n", i)
		fmt.Fprintf(&newB, "new%d\n", i)
	}
	diff, changed := UnifiedLineDiff(oldB.String(), newB.String())
	if !changed {
		t.Fatal("expected change")
	}
	if !strings.Contains(diff, "TRUNCATED") {
		t.Errorf("expected truncation notice for oversized diff:\n%s", diff[:200])
	}
}

func TestUnifiedLineDiff_OutputCap(t *testing.T) {
	var oldB, newB strings.Builder
	for i := 0; i < 500; i++ {
		fmt.Fprintf(&oldB, "shared-prefix line%d\n", i)
	}
	oldB.WriteString("tail\n")
	newB.WriteString(oldB.String())
	for i := 0; i < maxDiffOutputLines+50; i++ {
		fmt.Fprintf(&newB, "added%d\n", i)
	}
	diff, _ := UnifiedLineDiff(oldB.String(), newB.String())
	lines := strings.Count(diff, "\n")
	if lines > maxDiffOutputLines+2 {
		t.Errorf("diff exceeds cap: %d lines", lines)
	}
	if !strings.Contains(diff, "TRUNCATED") {
		t.Error("expected truncation notice")
	}
}
