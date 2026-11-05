package template

import "strings"

// stripYAMLKeys deletes `section.key` paths (and a bare `section` path)
// from a YAML document, together with everything nested under them,
// returning the new text and the dotted paths actually removed.
//
// It works on lines because settings.yaml is a commented file a person
// reads and edits; round-tripping it through a YAML marshaller would
// silently strip every comment in it, which is most of its value.
func stripYAMLKeys(text string, paths [][]string) (string, []string) {
	drop := map[string]bool{}
	for _, p := range paths {
		drop[strings.Join(p, ".")] = true
	}

	lines := strings.Split(text, "\n")
	keep := make([]string, 0, len(lines))
	var removed []string
	section := ""
	skipDeeperThan := -1 // indent of the key being dropped; -1 when not dropping

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " \t"))

		// Inside a dropped subtree: everything more indented goes with it,
		// as do the blank lines between its entries.
		if skipDeeperThan >= 0 {
			if trimmed == "" || indent > skipDeeperThan {
				continue
			}
			skipDeeperThan = -1
		}

		if trimmed == "" || strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, ":") {
			keep = append(keep, line)
			continue
		}

		key := strings.TrimSpace(strings.SplitN(trimmed, ":", 2)[0])
		dotted := key
		if indent == 0 {
			section = key
		} else if section != "" {
			dotted = section + "." + key
		}
		if drop[dotted] {
			removed = append(removed, dotted)
			skipDeeperThan = indent
			continue
		}
		keep = append(keep, line)
	}

	// Drop sections the removal emptied, and collapse the blank runs it left.
	return collapseEmptySections(strings.Join(keep, "\n")), removed
}

// collapseEmptySections removes a top-level `name:` whose body is now
// empty, along with any comment block immediately above it, and squeezes
// runs of blank lines down to one.
func collapseEmptySections(text string) string {
	lines := strings.Split(text, "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		isTopKey := line != "" && line[0] != ' ' && line[0] != '\t' &&
			strings.HasSuffix(trimmed, ":") && !strings.HasPrefix(trimmed, "#")
		if isTopKey {
			// Look ahead: is there an indented line before the next
			// top-level key?
			empty := true
			for j := i + 1; j < len(lines); j++ {
				n := lines[j]
				if strings.TrimSpace(n) == "" {
					continue
				}
				if n[0] == ' ' || n[0] == '\t' {
					if !strings.HasPrefix(strings.TrimSpace(n), "#") {
						empty = false
					}
					continue
				}
				break
			}
			if empty {
				// Drop the comment block directly above it too.
				for len(out) > 0 {
					last := strings.TrimSpace(out[len(out)-1])
					if strings.HasPrefix(last, "#") {
						out = out[:len(out)-1]
						continue
					}
					break
				}
				continue
			}
		}
		if trimmed == "" && len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
