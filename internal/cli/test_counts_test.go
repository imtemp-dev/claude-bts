package cli

import "testing"

// Trial problem #49: bts test run recorded total, passed, failed and
// skipped as 0 unless they were passed as flags, and the skill told the
// agent to fill them in by hand.  They now come from the runner's summary.
func TestParseTestCounts(t *testing.T) {
	cases := map[string]struct {
		out  string
		want testCounts
	}{
		"pytest -q": {"....\n242 passed in 3.12s\n", testCounts{Total: 242, Passed: 242}},
		"pytest with failures": {"=== 2 failed, 184 passed, 3 skipped, 1 error in 4.12s ===\n",
			testCounts{Total: 190, Passed: 184, Failed: 3, Skipped: 3}},
		"dotnet, two projects": {"Passed!  - Failed:     0, Passed:    51, Skipped:     0, Total:    51, Duration: 2 s\n" +
			"Failed!  - Failed:     1, Passed:    64, Skipped:     2, Total:    67, Duration: 9 s\n",
			testCounts{Total: 118, Passed: 115, Failed: 1, Skipped: 2}},
		"jest":   {"Tests:       1 failed, 2 skipped, 40 passed, 43 total\n", testCounts{Total: 43, Passed: 40, Failed: 1, Skipped: 2}},
		"vitest": {" Test Files  3 passed (3)\n      Tests  1 failed | 41 passed (42)\n", testCounts{Total: 42, Passed: 41, Failed: 1}},
		"go test -v": {"=== RUN   TestA\n--- PASS: TestA (0.00s)\n    --- PASS: TestA/sub (0.00s)\n--- FAIL: TestB (0.00s)\n--- SKIP: TestC (0.00s)\nFAIL\n",
			testCounts{Total: 3, Passed: 1, Failed: 1, Skipped: 1}},
	}
	for name, tc := range cases {
		got, ok := parseTestCounts(tc.out)
		if !ok || got != tc.want {
			t.Errorf("%s: got %+v ok=%v, want %+v", name, got, ok, tc.want)
		}
	}
	if _, ok := parseTestCounts("ok  \tgithub.com/x/y\t0.4s\n"); ok {
		t.Error("output with no known summary must report false")
	}
}
