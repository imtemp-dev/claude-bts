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
//
// Every summary counts, of every runner: a command often runs several
// (`pytest && vitest`, two pytest sessions).  Returning the first runner
// type found, and only its last summary, recorded swift-eagle's 5906
// pytest passes plus 1297 vitest passes as 1297, and would have hidden an
// earlier session's failures behind a later summary (trial problem #108).
func parseTestCounts(output string) (testCounts, bool) {
	var total testCounts
	found := false
	add := func(c testCounts) {
		total.Total += c.Total
		total.Passed += c.Passed
		total.Failed += c.Failed
		total.Skipped += c.Skipped
		found = true
	}
	for _, m := range dotnetSummaryRe.FindAllStringSubmatch(output, -1) {
		add(testCounts{Failed: countAtoi(m[1]), Passed: countAtoi(m[2]), Skipped: countAtoi(m[3]), Total: countAtoi(m[4])})
	}
	for _, m := range jestSummaryRe.FindAllStringSubmatch(output, -1) {
		c := countWords(m[1])
		if t := jestTotalRe.FindStringSubmatch(m[1]); t != nil {
			c.Total = countAtoi(t[1])
		}
		add(c)
	}
	for _, m := range vitestSummaryRe.FindAllStringSubmatch(output, -1) {
		c := countWords(strings.ReplaceAll(m[1], "|", ","))
		c.Total = countAtoi(m[2])
		add(c)
	}
	for _, m := range pytestSummaryRe.FindAllStringSubmatch(output, -1) {
		c := countWords(m[1])
		c.Total = c.Passed + c.Failed + c.Skipped
		add(c)
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
		add(c)
	}
	return total, found
}

var jestTotalRe = regexp.MustCompile(`(\d+) total`)

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
