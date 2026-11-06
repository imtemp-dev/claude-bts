package template

import (
	"io/fs"
	"regexp"
	"strconv"
	"testing"
)

// Both architect runners hit their 40-turn limit on swift-eagle and on
// neat-wolf, each a brownfield recipe in a large monorepo where most turns
// go to reading code: one runner was cut off before its report, the
// others needed a continuation or a retry (trial problem #109, Multica
// AGEN-119).  cool-crow's runners, in a small repository, used 45 and 28
// tool calls.
func TestArchitectRunner_HasTurnsForALargeRepository(t *testing.T) {
	fsys, err := EmbeddedTemplates()
	if err != nil {
		t.Fatal(err)
	}
	data, err := fs.ReadFile(fsys, ".claude/agents/bts-architect-runner.md")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^maxTurns:\s*(\d+)\s*$`).FindSubmatch(data)
	if m == nil {
		t.Fatal("bts-architect-runner.md declares no maxTurns")
	}
	if n, _ := strconv.Atoi(string(m[1])); n < 80 {
		t.Errorf("maxTurns is %d; the runner needs about 80 to read a large repository and still write its file", n)
	}
}
