package engine

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// testScopedWireframe writes each modify row's scope after its action,
// the form two of three trials on 11-06 wrote unprompted (trial problem
// #104): swift-eagle's 31 modify rows and neat-wolf's four all read
// `modify scope=...`, and every one of them dropped out of the anchors.
const testScopedWireframe = "## File Structure\n\n| File | Action | Depends On | Responsibility |\n|---|---|---|---|\n" +
	"| `agent_server/tools/pdf_tool.py` | modify scope=_op_extract_text,pdf_process | — | page coverage |\n" +
	"| `agent_server/agents/onboarding/specialist.py` | **modify** scope=OnboardingSpecialist | — | the skill list |\n" +
	"| `agent_server/agents/onboarding/SKILL.md` | create | — | the review |\n" +
	"| `agent_server/legacy.py` | delete | — | gone |\n" +
	"| `agent_server/tests/prompt.txt` | generate | — | generator: `just snapshots` |\n" +
	"| `agent_server/tests/test_subject.py` | unchanged | — | still matches |\n"

func TestParseWireframeFileTable_ModifyRowWithScopeIsAnAnchor(t *testing.T) {
	got := ParseWireframeFileTable(testScopedWireframe)
	want := []TaskAnchorKey{
		{Path: "agent_server/tools/pdf_tool.py", Action: "modify"},
		{Path: "agent_server/agents/onboarding/specialist.py", Action: "modify"},
		{Path: "agent_server/agents/onboarding/SKILL.md", Action: "create"},
		{Path: "agent_server/legacy.py", Action: "delete"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("a modify row keeps its anchor whatever follows the action:\n got %v\nwant %v", got, want)
	}
}

func TestParseWireframeScopes_ReadsTheScopeAfterTheAction(t *testing.T) {
	got := ParseWireframeScopes(testScopedWireframe)
	want := map[TaskAnchorKey][]string{
		{Path: "agent_server/tools/pdf_tool.py", Action: "modify"}:               {"_op_extract_text", "pdf_process"},
		{Path: "agent_server/agents/onboarding/specialist.py", Action: "modify"}: {"OnboardingSpecialist"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scopes keep their symbols' case:\n got %v\nwant %v", got, want)
	}
}

// A build recipe's final.md carries no task-anchor comments, so the
// wireframe row is the only place a modify task's scope can be declared.
func TestCheckModifyScope_BuildRecipeTakesTheScopeFromTheWireframe(t *testing.T) {
	dir := t.TempDir()
	final := filepath.Join(dir, "final.md")
	_ = os.WriteFile(final, []byte("# Build spec\n\nNo anchors here.\n"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "wireframe.md"), []byte(testScopedWireframe), 0644)
	tasks := filepath.Join(dir, "tasks.json")
	_ = os.WriteFile(tasks, []byte(`{"tasks":[
		{"id":"t-001","file":"agent_server/tools/pdf_tool.py","action":"modify","anchor":"agent_server/tools/pdf_tool.py modify"},
		{"id":"t-002","file":"agent_server/agents/onboarding/specialist.py","action":"modify","anchor":"agent_server/agents/onboarding/specialist.py modify","modify_scope":["Other"]}
	]}`), 0644)

	var claims []string
	for _, is := range CheckModifyScope(final, tasks, "") {
		claims = append(claims, is.Claim)
	}
	joined := strings.Join(claims, "; ")
	if strings.Contains(joined, "modify_scope_required: t-001") {
		t.Errorf("t-001's scope is declared in the wireframe row, so it is not required again: %s", joined)
	}
	if !strings.Contains(joined, "scope_mismatch: t-002") {
		t.Errorf("t-002 says {Other} while its row says {OnboardingSpecialist}: want scope_mismatch, got %s", joined)
	}
}

// Any other action becomes no task, so a typo or an older form must not
// vanish without a word (the silent drop behind #104).
func TestVerifyDocument_UnknownFileActionIsReported(t *testing.T) {
	dir := t.TempDir()
	wf := filepath.Join(dir, "wireframe.md")
	content := testScopedWireframe +
		"| `agent_server/typo.py` | modfy | — | a typo |\n" +
		"| `ui/types/Work.ts` | generated | — | the #66 form |\n"
	_ = os.WriteFile(wf, []byte(content), 0644)
	result, err := VerifyDocument(wf, "", true)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	var found []string
	for _, is := range result.Issues {
		if strings.Contains(is.Claim, "unknown_file_action") {
			found = append(found, is.Claim+" "+is.Severity)
		}
	}
	if len(found) != 2 {
		t.Fatalf("want one major per unknown action (modfy, generated), got %v", found)
	}
	for _, f := range found {
		if !strings.Contains(f, "major") {
			t.Errorf("an unknown action is major: %s", f)
		}
	}
	if !strings.Contains(strings.Join(found, " "), "agent_server/typo.py") || !strings.Contains(strings.Join(found, " "), "ui/types/Work.ts") {
		t.Errorf("each finding names its row's path: %v", found)
	}
}
