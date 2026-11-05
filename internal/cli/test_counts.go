package cli

import (
	"regexp"
	"strconv"
	"strings"
)

// Test runners' summary lines.  bts test run reads its counts from them
// when no --total/--passed/--failed/--skipped flag is given, so the counts
// in test-results.json come from the run, not from a hand edit afterwards
// (trial problem #49).  The status never comes from them: it is the exit
// code's.
var (
	// dotnet test, one line per test project:
	// "Passed!  - Failed:     0, Passed:    51, Skipped:     0, Total:    51, Duration: 2 s"
	dotnetSummaryRe = regexp.MustCompile(`Failed:\s*(\d+),\s*Passed:\s*(\d+),\s*Skipped:\s*(\d+),\s*Total:\s*(\d+)`)
	// jest: "Tests:       1 failed, 2 skipped, 40 passed, 43 total"
	jestSummaryRe = regexp.MustCompile(`(?m)^Tests:\s+(.*\d+ total)\s*$`)
	// vitest: " Tests  1 failed | 41 passed (42)"
	vitestSummaryRe = regexp.MustCompile(`(?m)^\s*Tests\s+(.*)\((\d+)\)\s*$`)
	// pytest, with or without -q: "=== 2 failed, 184 passed, 3 skipped in 4.12s ===" or "186 passed in 2.31s"
	pytestSummaryRe = regexp.MustCompile(`(?m)^[= ]*((?:\d+ (?:passed|failed|skipped|errors?|xfailed|xpassed|deselected|warnings?)(?:, )?)+) in [\d.]+s\b`)
	countWordRe     = regexp.MustCompile(`(\d+) (passed|failed|skipped|errors?|xfailed|xpassed|todo|pending)`)
	// go test -v, top-level tests only (subtests are indented).
	goTestResultRe = regexp.MustCompile(`(?m)^--- (PASS|FAIL|SKIP): `)
)

// parseTestCounts reads pass, fail and skip counts from a test runner's
// output.  It reports false when no known summary is present.
func parseTestCounts(output string) (testCounts, bool) {
	if ms := dotnetSummaryRe.FindAllStringSubmatch(output, -1); len(ms) > 0 {
		var c testCounts
		for _, m := range ms {
			c.Failed += countAtoi(m[1])
			c.Passed += countAtoi(m[2])
			c.Skipped += countAtoi(m[3])
			c.Total += countAtoi(m[4])
		}
		return c, true
	}
	if ms := jestSummaryRe.FindAllStringSubmatch(output, -1); len(ms) > 0 {
		c := countWords(ms[len(ms)-1][1])
		if m := regexp.MustCompile(`(\d+) total`).FindStringSubmatch(ms[len(ms)-1][1]); m != nil {
			c.Total = countAtoi(m[1])
		}
		return c, true
	}
	if ms := vitestSummaryRe.FindAllStringSubmatch(output, -1); len(ms) > 0 {
		last := ms[len(ms)-1]
		c := countWords(strings.ReplaceAll(last[1], "|", ","))
		c.Total = countAtoi(last[2])
		return c, true
	}
	if ms := pytestSummaryRe.FindAllStringSubmatch(output, -1); len(ms) > 0 {
		c := countWords(ms[len(ms)-1][1])
		c.Total = c.Passed + c.Failed + c.Skipped
		return c, true
	}
	if ms := goTestResultRe.FindAllStringSubmatch(output, -1); len(ms) > 0 {
		var c testCounts
		for _, m := range ms {
			switch m[1] {
			case "PASS":
				c.Passed++
			case "FAIL":
				c.Failed++
			case "SKIP":
				c.Skipped++
			}
		}
		c.Total = c.Passed + c.Failed + c.Skipped
		return c, true
	}
	return testCounts{}, false
}

// countWords adds up "N passed", "N failed" and the like.  An error counts
// as a failure, xpassed as passed, xfailed, todo and pending as skipped.
func countWords(s string) testCounts {
	var c testCounts
	for _, m := range countWordRe.FindAllStringSubmatch(s, -1) {
		n := countAtoi(m[1])
		switch m[2] {
		case "passed", "xpassed":
			c.Passed += n
		case "failed", "error", "errors":
			c.Failed += n
		case "skipped", "xfailed", "todo", "pending":
			c.Skipped += n
		}
	}
	return c
}

func countAtoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
