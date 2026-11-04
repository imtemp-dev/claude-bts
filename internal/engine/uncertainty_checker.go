package engine

import (
	"os"
	"regexp"
	"strings"
)

// UncertaintyEntry is one entry parsed from final.md's
// "## Known Uncertainties" section. Entries are the `### U-NNN` blocks;
// Status tells whether one of the three resolution prefixes is present.
type UncertaintyEntry struct {
	Heading    string // raw heading line without the leading "### "
	ID         string // "U-001" etc. — extracted from the heading
	LineNumber int    // 1-based line number of the heading in final.md
	Status     string // "resolved" | "diverged" | "still-unknown" | ""
}

var (
	// Section start: tolerant of capitalization and trailing colons/spaces.
	uncertaintySectionRe = regexp.MustCompile(`(?mi)^##\s+Known\s+Uncertainties\s*:?\s*$`)
	// Entry heading: "### U-001: ..." (id prefix required to avoid matching
	// other ### subsections inside the Known Uncertainties section).
	uncertaintyHeadingRe = regexp.MustCompile(`(?mi)^###\s+(U-\d+)\b[^\n]*`)
	// Resolution marker: Resolved / Diverged / Still-unknown at line start
	// of the entry body, then a colon. A qualifier for the part it covers
	// may sit before the colon, in parentheses ("Resolved (reads):") or
	// after "for" ("Resolved for reads (§ 3):").
	uncertaintyResolRe = regexp.MustCompile(`(?mi)^(Resolved|Diverged|Still-unknown)(?:[ \t]*\([^)\n]{1,80}\)|[ \t]+for[ \t][^:\n]{1,80})?:\s*\S`)
)

// CheckKnownUncertainties reads final.md and returns all Known
// Uncertainty entries plus the subset that lack a resolution marker.
//
// Semantics — the implement hook (Phase 8 Drift #2) blocks completion
// only when len(unresolved) > 0. Missing file or missing section both
// return (nil, nil, nil): uncertainty tracking is optional, not
// universal, and recipes without the section are unaffected.
func CheckKnownUncertainties(finalPath string) (all []UncertaintyEntry, unresolved []UncertaintyEntry, err error) {
	data, err := os.ReadFile(finalPath)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	content := string(data)

	// Locate the section. Without a header, there are no entries to check.
	loc := uncertaintySectionRe.FindStringIndex(content)
	if loc == nil {
		return nil, nil, nil
	}

	// Scope the parse to the section body: from the end of the header
	// line to the next `## ` heading (or EOF).
	bodyStart := loc[1]
	bodyEnd := len(content)
	// next H2 after section start
	for m := range nextH2Indices(content[bodyStart:]) {
		bodyEnd = bodyStart + m
		break
	}
	body := content[bodyStart:bodyEnd]

	// Count lines up to bodyStart so we can produce accurate line numbers.
	bodyStartLine := strings.Count(content[:bodyStart], "\n") + 1

	// Walk entries: split the body at each ### U-NNN heading.
	headings := uncertaintyHeadingRe.FindAllStringSubmatchIndex(body, -1)
	for i, m := range headings {
		// m = [full-start, full-end, id-start, id-end]
		entryStart := m[0]
		entryEnd := len(body)
		if i+1 < len(headings) {
			entryEnd = headings[i+1][0]
		}
		block := body[entryStart:entryEnd]

		headingLine := strings.SplitN(block, "\n", 2)[0]
		id := strings.TrimSpace(block[m[2]-m[0] : m[3]-m[0]])
		entry := UncertaintyEntry{
			Heading:    strings.TrimSpace(strings.TrimPrefix(headingLine, "###")),
			ID:         id,
			LineNumber: bodyStartLine + strings.Count(body[:entryStart], "\n"),
			Status:     classifyResolution(block),
		}
		all = append(all, entry)
		if entry.Status == "" {
			unresolved = append(unresolved, entry)
		}
	}
	return all, unresolved, nil
}

// classifyResolution returns the lower-cased marker ("resolved" |
// "diverged" | "still-unknown") or "" if none of the three keywords
// appears at the start of a line inside the entry block. A mixed outcome
// (one line per part) takes the least settled part: still-unknown, then
// diverged, then resolved.
func classifyResolution(block string) string {
	rank := map[string]int{"resolved": 1, "diverged": 2, "still-unknown": 3}
	status := ""
	for _, m := range uncertaintyResolRe.FindAllStringSubmatch(block, -1) {
		if s := strings.ToLower(m[1]); rank[s] > rank[status] {
			status = s
		}
	}
	return status
}

// nextH2Indices yields the byte offsets (within the input) of every
// line that begins a fresh `## ` heading. Emitted lazily via a channel
// so the caller can break early after finding the first one.
func nextH2Indices(body string) <-chan int {
	ch := make(chan int)
	go func() {
		defer close(ch)
		pos := 0
		for {
			idx := strings.Index(body[pos:], "\n## ")
			if idx < 0 {
				return
			}
			ch <- pos + idx + 1 // +1 to skip the newline
			pos = pos + idx + 1
		}
	}()
	return ch
}
