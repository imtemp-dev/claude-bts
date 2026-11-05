package template

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// The bts reviewer and diagnosis agents declared `memory: project`, so a
// review wrote `.claude/agent-memory/<agent>/` into the project, untracked
// in every worktree of a shared repository (trial problem #82, Multica
// AGEN-59).  `memory: local` keeps the same memory across sessions in the
// checkout, under `.claude/agent-memory-local/`, which stays out of git.
func TestAgentTemplates_MemoryStaysOutOfTheProject(t *testing.T) {
	fsys, err := EmbeddedTemplates()
	if err != nil {
		t.Fatal(err)
	}
	agents, err := fs.Glob(fsys, ".claude/agents/*.md")
	if err != nil || len(agents) == 0 {
		t.Fatalf("no agent templates found: %v", err)
	}
	memoryLine := regexp.MustCompile(`(?m)^memory:\s*(\S+)\s*$`)
	withMemory := 0
	for _, p := range agents {
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			t.Fatal(err)
		}
		front := string(data)
		if parts := strings.SplitN(front, "\n---", 2); len(parts) == 2 {
			front = parts[0]
		}
		m := memoryLine.FindStringSubmatch(front)
		if m == nil {
			continue
		}
		withMemory++
		if m[1] != "local" {
			t.Errorf("%s declares memory: %s; a bts agent keeps its memory out of the project (memory: local)", p, m[1])
		}
	}
	if withMemory == 0 {
		t.Fatal("no agent declares memory; the reviewers are meant to keep what they learn")
	}
}
