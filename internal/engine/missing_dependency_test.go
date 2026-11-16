package engine

import "testing"

// The outputs are the real tools' (captured 2026-11-16), with paths
// shortened.
func TestMissingDependency(t *testing.T) {
	cases := []struct {
		name, out string
		exit      int
		want      string
	}{
		{"python import", "Traceback (most recent call last):\n  File \"<string>\", line 1, in <module>\n    import dotenv\nModuleNotFoundError: No module named 'dotenv'\n", 1,
			"No module named dotenv"},
		{"pytest conftest (exit 4, trial problem #141)", "ImportError while loading conftest '/w/agent_server/tests/conftest.py'.\nconftest.py:1: in <module>\n    import dotenv\nE   ModuleNotFoundError: No module named 'dotenv'\n", 4,
			"No module named dotenv"},
		{"pytest collection (exit 2)", "____ ERROR collecting test_b.py ____\nImportError while importing test module '/w/test_b.py'.\ntest_b.py:1: in <module>\n    import dotenv\nE   ModuleNotFoundError: No module named 'dotenv'\n!!!! Interrupted: 1 error during collection !!!!\n", 2,
			"No module named dotenv"},
		{"python -m without the module", "/opt/homebrew/opt/python@3.15/bin/python3.15: No module named pytest\n", 1,
			"No module named pytest"},
		{"node require", "node:internal/modules/cjs/loader:1573\n  throw err;\n  ^\n\nError: Cannot find module 'left-pad'\nRequire stack:\n- /w/[eval]\n", 1,
			"Cannot find module left-pad"},
		{"node import", "Error [ERR_MODULE_NOT_FOUND]: Cannot find package 'left-pad' imported from /w/a.mjs\n", 1,
			"Cannot find package left-pad"},
		{"sh", "sh: pytest: command not found\n", 127, "command not found: pytest"},
		{"dash", "sh: 1: pytest: not found\n", 127, "command not found: pytest"},
		{"uv run", "error: Failed to spawn: `pytest`\n  Caused by: No such file or directory (os error 2)\n", 2,
			"Failed to spawn pytest"},
		{"go build", "go: finding module for package example.com/x/y\nmain.go:2:8: cannot find module providing package example.com/x/y: module lookup disabled by GOPROXY=off\n", 1,
			"cannot find module providing package example.com/x/y"},
		{"exit 127 alone", "", 127, "exit 127 (command not found)"},

		// A failure of the code itself is a reproduction, not a missing
		// dependency.
		{"assertion", "FAILED tests/test_x.py::test_order - AssertionError: assert 2 == 3\n1 failed, 4 passed\n", 1, ""},
		{"import-time crash in the code", "E   ValueError: REPORT_LIMIT must be positive\n!!!! Interrupted: 1 error during collection !!!!\n", 2, ""},
		{"a name missing from a module", "E   ImportError: cannot import name 'render' from 'reports.contract' (/w/reports/contract.py)\n", 2, ""},
		{"a pass", "No module named 'dotenv' (warning only)\n", 0, ""},
	}
	for _, c := range cases {
		if got := MissingDependency([]byte(c.out), c.exit); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
