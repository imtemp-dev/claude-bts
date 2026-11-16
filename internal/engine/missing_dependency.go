package engine

import (
	"fmt"
	"regexp"
)

// missingDependencySigns are what a run prints when it stopped before the
// code under test, for want of a dependency the environment lacks: a
// module, a package, the command itself.  Each was checked against the
// real tool's output (python, pytest's conftest and collection errors,
// node's require and import, sh, uv, go).  A name a project does provide
// can match too (a repro importing a module the recipe forgot); the caller
// tells the two apart by running the recipe's own tests in the same kind
// of copy.
var missingDependencySigns = []struct {
	re   *regexp.Regexp
	what string
}{
	{regexp.MustCompile(`No module named '?([\w.\-]+)'?`), "No module named %s"},
	{regexp.MustCompile(`Cannot find module '([^']+)'`), "Cannot find module %s"},
	{regexp.MustCompile(`Cannot find package '([^']+)'`), "Cannot find package %s"},
	{regexp.MustCompile("Failed to spawn: `([^`]+)`"), "Failed to spawn %s"},
	{regexp.MustCompile(`(?m)^(?:sh|bash|zsh)(?:: line \d+|: \d+)?: ([^:\n]+): (?:command )?not found`), "command not found: %s"},
	{regexp.MustCompile(`cannot find module providing package (\S+?):?\s`), "cannot find module providing package %s"},
	{regexp.MustCompile(`no required module provides package (\S+?);?\s`), "no required module provides package %s"},
}

// MissingDependency reports why a failed run says nothing about the code it
// was meant to test, when its output shows a missing dependency, or exit
// code 126 or 127 shows the command could not start.  "" when neither.
func MissingDependency(output []byte, exitCode int) string {
	if exitCode == 0 {
		return ""
	}
	for _, s := range missingDependencySigns {
		if m := s.re.FindSubmatch(output); m != nil {
			return fmt.Sprintf(s.what, m[1])
		}
	}
	switch exitCode {
	case 126:
		return "exit 126 (the command could not run)"
	case 127:
		return "exit 127 (command not found)"
	}
	return ""
}
