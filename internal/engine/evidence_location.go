package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// An `Opens-with:` line is the command that settles a Known Uncertainty,
// and its point is that someone else can run it again: a reviewer, a later
// session, the prove stage.  On all three trials of 11-06 the probes ran
// from the session scratchpad, which disappears with the session, and on
// cool-crow the line named "the session scratchpad's ... directory" with
// no path at all (trial problem #89).  The skills had said to put probes
// "outside the project".

var (
	opensWithLineRe   = regexp.MustCompile(`(?i)^\s*[*_>-]*\s*opens-with\s*:\s*(.*)$`)
	uncertaintyHeadRe = regexp.MustCompile(`^#{2,6}\s*(U-\d+)\b`)
	// An absolute path inside a command or prose: /private/tmp/..., /tmp/...,
	// /Users/..., ~/....
	absPathRe = regexp.MustCompile("(?:^|[\\s`'\"(=])((?:/|~/)[^\\s`'\")]+)")
	// Words that name a place gone when the session ends.
	ephemeralWordRe = regexp.MustCompile(`(?i)\bscratch\s*pad\b|\bscratchpad\b`)
)

// CheckEvidenceLocation reports each Known Uncertainty whose Opens-with
// runs from outside the project or from a session scratchpad, as a major:
// the evidence cannot be rerun once the session is gone.
func CheckEvidenceLocation(docPath, projectRoot string) []Issue {
	data, err := os.ReadFile(docPath)
	if err != nil {
		return nil
	}
	root := ""
	if projectRoot != "" {
		if abs, aerr := filepath.Abs(projectRoot); aerr == nil {
			root = filepath.Clean(abs)
		}
	}
	var issues []Issue
	current := ""
	for _, line := range strings.Split(string(data), "\n") {
		if m := uncertaintyHeadRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			current = m[1]
			continue
		}
		m := opensWithLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		where := evidenceOutside(m[1], root)
		if where == "" {
			continue
		}
		label := current
		if label == "" {
			label = "an uncertainty"
		}
		issues = append(issues, Issue{
			Category: "evidence_location",
			Claim:    fmt.Sprintf("evidence_outside_project: %s", label),
			Severity: SeverityMajor,
			Detail: fmt.Sprintf("%s's Opens-with runs from %s, which is gone when the session ends, so no reviewer, later "+
				"session or prove stage can rerun it. Keep the probe in the recipe's probes/ directory "+
				"(.bts/specs/recipes/<id>/probes/) and name it by its path from the project root.", label, where),
		})
	}
	return issues
}

// systemPath reports a path every machine has (`/usr/bin/env`, `2>/dev/null`),
// which a rerun finds as well as the first run did.
func systemPath(p string) bool {
	for _, prefix := range []string{"/usr/", "/bin/", "/sbin/", "/dev/", "/opt/", "/etc/"} {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// evidenceOutside names where an Opens-with points outside the project,
// or returns "" when it stays inside.
func evidenceOutside(text, root string) string {
	if ephemeralWordRe.MatchString(text) {
		for _, m := range absPathRe.FindAllStringSubmatch(text, -1) {
			return m[1]
		}
		return "a session scratchpad"
	}
	for _, m := range absPathRe.FindAllStringSubmatch(text, -1) {
		p := m[1]
		if strings.HasPrefix(p, "~/") {
			return p
		}
		if systemPath(p) {
			continue
		}
		clean := filepath.Clean(p)
		if root != "" && (clean == root || strings.HasPrefix(clean, root+string(filepath.Separator))) {
			continue
		}
		return p
	}
	return ""
}
