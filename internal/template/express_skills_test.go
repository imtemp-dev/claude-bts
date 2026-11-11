package template

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Feature #132, step 3: the code sends an express recipe to the skills.
// The resume hint and the gate registry name bts-recipe-build § Express
// mode; an express recipe reaches
// /bts-implement with no review of final.md in verify-log.jsonl, where the
// spec check used to read that as "Spec not verified".  Each skill has to
// carry its side.

func readSkill(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("templates", ".claude", "skills", name, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// section returns the text under a "## heading" up to the next "## ".
func section(doc, heading string) string {
	re := regexp.MustCompile(`(?m)^## ` + regexp.QuoteMeta(heading) + `\s*$`)
	loc := re.FindStringIndex(doc)
	if loc == nil {
		return ""
	}
	rest := doc[loc[1]:]
	if next := regexp.MustCompile(`(?m)^## `).FindStringIndex(rest); next != nil {
		rest = rest[:next[0]]
	}
	return rest
}

func TestExpressMode_TheBuildSkillHasTheSectionTheCodeNames(t *testing.T) {
	express := section(readSkill(t, "bts-recipe-build"), "Express mode")
	if express == "" {
		t.Fatal("bts-recipe-build has no § Express mode, which the resume hint and the gate registry name")
	}
	// The commands the code gives an express recipe: how it is made, how
	// final.md is checked in place of the review round, how it leaves.
	for _, cmd := range []string{"--express", "bts verify .bts/specs/recipes/{id}/final.md", "bts recipe mode {id} full"} {
		if !strings.Contains(express, cmd) {
			t.Errorf("§ Express mode does not give %q", cmd)
		}
	}
	// The DONE gate it is held to, by the name the stop hook reports.
	if !strings.Contains(express, "build_verify_clean") {
		t.Error("§ Express mode does not name its DONE gate, build_verify_clean")
	}
}

func TestExpressMode_ImplementAcceptsAnExpressSpec(t *testing.T) {
	impl := readSkill(t, "bts-implement")
	if !strings.Contains(impl, "build_verify_clean") {
		t.Error("bts-implement's spec check does not know an express recipe's bar (build_verify_clean), so an express spec reads as not verified")
	}
}
