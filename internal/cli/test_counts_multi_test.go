package cli

import "testing"

// Trial problem #108: swift-eagle's test command ran the agent_server
// pytest suite (5906 passed) and then the UI vitest suite (1297 passed),
// and test-results.json said passed: 1297.  parseTestCounts returned the
// first runner type it found, vitest before pytest, and kept only its last
// summary.  Every summary counts.
func TestParseTestCounts_AddsEveryRunnerInOneCommand(t *testing.T) {
	cases := map[string]struct {
		out  string
		want testCounts
	}{
		"pytest then vitest": {"....\n5906 passed in 312.40s\n" +
			" Test Files  120 passed (120)\n      Tests  1297 passed (1297)\n",
			testCounts{Total: 7203, Passed: 7203}},
		"two pytest sessions": {"=== 3 failed, 10 passed in 1.20s ===\n...\n40 passed, 2 skipped in 0.80s\n",
			testCounts{Total: 55, Passed: 50, Failed: 3, Skipped: 2}},
		"jest and dotnet": {"Tests:       2 failed, 40 passed, 42 total\n" +
			"Passed!  - Failed:     0, Passed:    51, Skipped:     0, Total:    51, Duration: 2 s\n",
			testCounts{Total: 93, Passed: 91, Failed: 2}},
	}
	for name, tc := range cases {
		got, ok := parseTestCounts(tc.out)
		if !ok || got != tc.want {
			t.Errorf("%s: got %+v ok=%v, want %+v", name, got, ok, tc.want)
		}
	}
}
