package engine

import (
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #134: owner_drift asked the owner file to spell the
// domain's conceptual owner name.  It now asks for the member the
// Enforcement point names.

func ownerDomain(owner, enforcement string) string {
	return "## 2. Invariants\n\n| ID | Statement | Owner | Enforcement point |\n|----|-----------|-------|-------------------|\n" +
		"| INV-001 | a recurring report expands once per period | " + owner + " | " + enforcement + " |\n"
}

func ownerDrift(t *testing.T, domain, file, source string) *state.StructureFinding {
	t.Helper()
	root, recipeDir := setupPerTask(t, "", domain, file, source)
	task := &state.Task{ID: "t-001", File: file, Action: "create"}
	for _, f := range CheckTaskStructure(root, recipeDir, task) {
		if f.Category == "owner_drift" {
			return &f
		}
	}
	return nil
}

func TestOwnerDrift_ModuleNeedNotSpellItsOwner(t *testing.T) {
	source := "\"\"\"Expands recurring reports.\"\"\"\n\ndef expand_recurring(report):\n    return [report]\n"
	if f := ownerDrift(t, ownerDomain("Engine", "Engine.expand_recurring()"), "agent_server/reports/engine.py", source); f != nil {
		t.Fatalf("engine.py is the Engine module and declares expand_recurring, yet: %+v", f)
	}
}

func TestOwnerDrift_ClassMemberCounts(t *testing.T) {
	source := "class Engine:\n    def expand_recurring(self, report):\n        return [report]\n"
	if f := ownerDrift(t, ownerDomain("Engine", "`Engine.expand_recurring()` guard"), "engine.py", source); f != nil {
		t.Fatalf("Engine.expand_recurring is declared, yet: %+v", f)
	}
}

func TestOwnerDrift_MissingEnforcementMemberIsReported(t *testing.T) {
	source := "def stage(report):\n    return report\n"
	f := ownerDrift(t, ownerDomain("ReportEngine", "ReportEngine.expand_recurring()"), "reports/report_engine.py", source)
	if f == nil {
		t.Fatal("report_engine.py is the ReportEngine module and no longer declares expand_recurring: want owner_drift")
	}
	if f.Severity != "critical" {
		t.Errorf("severity = %s, want critical", f.Severity)
	}
}

func TestOwnerDrift_ProseEnforcementIsNotChecked(t *testing.T) {
	for _, cell := range []string{"derived, no independent store", "`engine.py` guard", "agent_server/engine.run at startup"} {
		if f := ownerDrift(t, ownerDomain("Engine", cell), "engine.py", "def stage(report):\n    return report\n"); f != nil {
			t.Errorf("Enforcement point %q names no member of Engine, yet: %+v", cell, f)
		}
	}
}

func TestEnforcementMember(t *testing.T) {
	cases := []struct{ owner, cell, want string }{
		{"Arrangement", "Arrangement.place()", "place"},
		{"DragGesture", "DragGesture.begin() guard", "begin"},
		{"Relay", "`Relay.open()` lookup, kept by `tests/test_relay.py::test_one_issue_per_pr`", "open"},
		{"ReportEngine", "report_engine.expand()", "expand"},
		{"Engine", "`engine.py` guard", ""},
		{"Engine", "derived, no independent store", ""},
		{"Engine", "Store.save()", ""},
	}
	for _, c := range cases {
		if got := enforcementMember(c.owner, c.cell); got != c.want {
			t.Errorf("enforcementMember(%q, %q) = %q, want %q", c.owner, c.cell, got, c.want)
		}
	}
}
