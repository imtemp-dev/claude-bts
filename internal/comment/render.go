package comment

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
)

// ANSI color codes — disabled when NO_COLOR env is set or stdout is not a TTY.
// Stdout-TTY detection is left to the caller (CLI) to decide; this file
// trusts the `useColor` flag.
const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiRed    = "\x1b[31m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"
	ansiGray   = "\x1b[90m"
)

// ColorEnabled returns true when ANSI color should be emitted.
// Honors NO_COLOR and CLICOLOR=0.
func ColorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("CLICOLOR") == "0" {
		return false
	}
	return true
}

// kindBadge returns a short tag colored by severity.
func kindBadge(k Kind, useColor bool) string {
	label, color := "", ""
	switch k {
	case KindBlock:
		label, color = "BLOCK ", ansiRed+ansiBold
	case KindQuestion:
		label, color = "QUESTN", ansiYellow
	case KindFreeForm:
		label, color = "FREE  ", ansiGray
	default:
		label, color = "NOTE  ", ansiCyan
	}
	if !useColor {
		return label
	}
	return color + label + ansiReset
}

// RenderPreview prints a grouped, color-coded view of comments.
// Output goes to `w` (typically os.Stdout). `useColor` toggles ANSI codes.
func RenderPreview(w io.Writer, comments []Comment, useColor bool) {
	if len(comments) == 0 {
		fmt.Fprintln(w, "No BTS comments found in this recipe.")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Add one in any .md by typing `btsc<Tab>` in VS Code,")
		fmt.Fprintln(w, "or run `bts comment preview --include-freeform` to see free-form additions.")
		return
	}

	// Group by file (stable file order: alphabetical).
	byFile := map[string][]Comment{}
	for _, c := range comments {
		byFile[c.File] = append(byFile[c.File], c)
	}
	files := make([]string, 0, len(byFile))
	for f := range byFile {
		files = append(files, f)
	}
	sort.Strings(files)

	summary := Summarize(comments)

	// Header
	if useColor {
		fmt.Fprintf(w, "%sBTS comments — %d total, %s%d blocking%s\n",
			ansiBold, summary.TotalOpen, ansiRed, summary.TotalBlocking, ansiReset)
	} else {
		fmt.Fprintf(w, "BTS comments — %d total, %d blocking\n", summary.TotalOpen, summary.TotalBlocking)
	}
	fmt.Fprintln(w)

	for _, file := range files {
		cs := byFile[file]
		// Sort within a file by line number.
		sort.Slice(cs, func(i, j int) bool { return cs[i].Line < cs[j].Line })

		fileLabel := file
		if useColor {
			fileLabel = ansiBold + file + ansiReset
		}
		fmt.Fprintf(w, "%s  (%d)\n", fileLabel, len(cs))

		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, c := range cs {
			cls := Classify(c)
			body := singleLine(c.Body, 60)
			impact := ""
			if len(cls.LikelyImpact) > 0 {
				impact = "→ " + strings.Join(cls.LikelyImpact, ", ")
				if useColor {
					impact = ansiGray + impact + ansiReset
				}
			}
			fmt.Fprintf(tw, "  %s\tL%d\t%s\t%s\t%s\n",
				kindBadge(c.Kind, useColor),
				c.Line,
				c.ID,
				body,
				impact,
			)
		}
		tw.Flush()
		fmt.Fprintln(w)
	}

	// Footer hint
	if summary.TotalBlocking > 0 {
		hint := fmt.Sprintf("⚠  %d BTS-BLOCK comment(s) will block recipe finalize until resolved.",
			summary.TotalBlocking)
		if useColor {
			hint = ansiYellow + hint + ansiReset
		}
		fmt.Fprintln(w, hint)
	}
	fmt.Fprintln(w, "Run `bts comment apply <recipe-id>` to incorporate.")
}

// singleLine collapses whitespace and rune-truncates to maxRunes for table display.
// Rune-truncates rather than byte-truncates so non-ASCII bodies (Korean,
// Japanese, emoji) never get cut mid-character, which would emit invalid
// UTF-8 to the terminal.
func singleLine(s string, maxRunes int) string {
	return truncRunesEllipsis(strings.Join(strings.Fields(s), " "), maxRunes)
}
