package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func findingsRoot(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	id := "r-001-test"
	if err := os.MkdirAll(RecipeDir(root, id), 0755); err != nil {
		t.Fatal(err)
	}
	return root, id
}

func TestFindingIDIsStableAcrossFormattingChurn(t *testing.T) {
	base := FindingID("draft.md", "safeAreaInset propagation to DocumentScanView left unresolved")
	same := []string{
		"safeAreaInset propagation to DocumentScanView left unresolved.",
		"`safeAreaInset` propagation to **DocumentScanView** left unresolved",
		"SafeAreaInset  propagation to DocumentScanView — left unresolved!",
	}
	for _, s := range same {
		if got := FindingID("draft.md", s); got != base {
			t.Errorf("FindingID(%q) = %s, want %s (formatting must not change identity)", s, got, base)
		}
	}
	if FindingID("wireframe.md", "safeAreaInset propagation to DocumentScanView left unresolved") == base {
		t.Error("same title on a different document must get a different ID")
	}
	if FindingID("draft.md", "a completely different defect") == base {
		t.Error("different titles must not collide")
	}
}

func TestSyncFindingsLifecycle(t *testing.T) {
	root, id := findingsRoot(t)

	// Round 1: two findings appear.
	r1, err := SyncFindings(root, id, "draft.md", 1, []ReportedFinding{
		{Severity: "critical", Title: "contradictory retry policy"},
		{Severity: "major", Title: "missing error path for timeout"},
	}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(r1.New) != 2 || len(r1.Carried) != 0 {
		t.Fatalf("round 1: new=%v carried=%v, want 2 new", r1.New, r1.Carried)
	}

	// Round 2: the critical is fixed, the major persists.
	r2, err := SyncFindings(root, id, "draft.md", 2, []ReportedFinding{
		{Severity: "major", Title: "missing error path for timeout"},
	}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Fixed) != 1 {
		t.Errorf("round 2: fixed=%v, want 1", r2.Fixed)
	}
	if len(r2.Carried) != 1 {
		t.Errorf("round 2: carried=%v, want 1", r2.Carried)
	}

	// Round 3: the fixed finding comes back — a regression the loop
	// could not previously see, because findings had no identity.
	r3, err := SyncFindings(root, id, "draft.md", 3, []ReportedFinding{
		{Severity: "critical", Title: "contradictory retry policy"},
		{Severity: "major", Title: "missing error path for timeout"},
	}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(r3.Reopened) != 1 {
		t.Fatalf("round 3: reopened=%v, want 1", r3.Reopened)
	}

	// The major has now been open in rounds 1, 2 and 3 → stagnant at 3.
	if len(r3.Stagnant) != 1 {
		t.Errorf("round 3: stagnant=%v, want the 3-round-old major", r3.Stagnant)
	}

	states, err := LoadFindings(root, id, "draft.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 {
		t.Fatalf("expected 2 tracked findings, got %d", len(states))
	}
	byID := map[string]*FindingState{}
	for _, st := range states {
		byID[st.ID] = st
	}
	crit := byID[FindingID("draft.md", "contradictory retry policy")]
	if crit.Reopened != 1 {
		t.Errorf("critical reopened = %d, want 1", crit.Reopened)
	}
	if crit.OpenRounds != 2 {
		t.Errorf("critical open rounds = %d, want 2", crit.OpenRounds)
	}
}

func TestSyncFindingsScopesByDocument(t *testing.T) {
	root, id := findingsRoot(t)
	if _, err := SyncFindings(root, id, "wireframe.md", 1, []ReportedFinding{
		{Severity: "major", Title: "component boundary undefined"},
	}, 3); err != nil {
		t.Fatal(err)
	}
	// A draft round must not close the wireframe's open finding.
	if _, err := SyncFindings(root, id, "draft.md", 2, []ReportedFinding{
		{Severity: "minor_resolvable", Title: "stale level header"},
	}, 3); err != nil {
		t.Fatal(err)
	}
	wf, err := LoadFindings(root, id, "wireframe.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(wf) != 1 || wf[0].Status != FindingOpen {
		t.Fatalf("wireframe finding should still be open, got %+v", wf)
	}
}

func TestDeferredFindingsArePersistentNotFixed(t *testing.T) {
	root, id := findingsRoot(t)
	if _, err := SyncFindings(root, id, "draft.md", 1, []ReportedFinding{
		{Severity: "minor_deferred", Title: "measured scroll threshold unknown until device test"},
	}, 3); err != nil {
		t.Fatal(err)
	}
	// A later round that does not repeat it must not mark it fixed —
	// deferred items are runtime watch-items carried into implement.
	r2, err := SyncFindings(root, id, "draft.md", 2, nil, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Fixed) != 0 {
		t.Errorf("deferred finding was marked fixed: %v", r2.Fixed)
	}
	states, _ := LoadFindings(root, id, "draft.md")
	if len(states) != 1 || states[0].Status != FindingDeferred {
		t.Fatalf("want one deferred finding, got %+v", states)
	}
}

func TestDismissSuppressesThenDetectsRelitigation(t *testing.T) {
	root, id := findingsRoot(t)
	if _, err := SyncFindings(root, id, "draft.md", 1, []ReportedFinding{
		{Severity: "major", Title: "uses deprecated lifecycle hook"},
	}, 3); err != nil {
		t.Fatal(err)
	}
	fid := FindingID("draft.md", "uses deprecated lifecycle hook")
	if err := DismissFinding(root, id, fid, "official docs confirm the hook is current"); err != nil {
		t.Fatal(err)
	}

	states, _ := LoadFindings(root, id, "draft.md")
	if states[0].Status != FindingDismissed {
		t.Fatalf("want dismissed, got %s", states[0].Status)
	}
	block := CarryForwardBlock(states)
	if !strings.Contains(block, "do NOT re-raise") || !strings.Contains(block, fid) {
		t.Errorf("carry-forward block should warn against re-raising %s:\n%s", fid, block)
	}

	// If a later verifier raises it anyway, that is re-litigation.
	r, err := SyncFindings(root, id, "draft.md", 2, []ReportedFinding{
		{Severity: "major", Title: "uses deprecated lifecycle hook"},
	}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Reopened) != 1 {
		t.Errorf("re-raising a dismissed finding should register as reopened, got %+v", r)
	}
}

func TestCarryForwardBlockEmptyLedger(t *testing.T) {
	if got := CarryForwardBlock(nil); got != "" {
		t.Errorf("empty ledger should render nothing, got %q", got)
	}
}

func TestReadFindingEventsSkipsMalformedLines(t *testing.T) {
	root, id := findingsRoot(t)
	path := filepath.Join(RecipeDir(root, id), "findings.jsonl")
	content := `{"id":"F-aaaaaaaa","doc":"draft.md","severity":"major","title":"ok","status":"open"}
not json at all
{"id":"F-bbbbbbbb","doc":"draft.md","severity":"info","title":"second","status":"open"}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	events, err := ReadFindingEvents(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Errorf("want 2 parsed events, got %d", len(events))
	}
}

// A dismissal is an adjudication: the verifier raising the point again
// must NOT silently un-dismiss it. Otherwise one re-raise erases the
// "do NOT re-raise" hint and the finding is re-litigated forever —
// defeating the purpose of `bts recipe findings dismiss`.
func TestDismissalIsStickyAcrossReRaises(t *testing.T) {
	root, id := findingsRoot(t)
	title := "uses deprecated lifecycle hook"
	if _, err := SyncFindings(root, id, "draft.md", 1, []ReportedFinding{
		{Severity: "major", Title: title},
	}, 3); err != nil {
		t.Fatal(err)
	}
	fid := FindingID("draft.md", title)
	if err := DismissFinding(root, id, fid, "official docs confirm the hook is current"); err != nil {
		t.Fatal(err)
	}
	// Round 2: a fresh verifier raises it again.
	if _, err := SyncFindings(root, id, "draft.md", 2, []ReportedFinding{
		{Severity: "major", Title: title},
	}, 3); err != nil {
		t.Fatal(err)
	}

	states, _ := LoadFindings(root, id, "draft.md")
	if len(states) != 1 {
		t.Fatalf("want 1 finding, got %d", len(states))
	}
	st := states[0]
	if st.Status != FindingDismissed {
		t.Errorf("status = %q, want %q — a verifier must not override an adjudicated dismissal",
			st.Status, FindingDismissed)
	}
	if st.Reason == "" {
		t.Error("dismissal reason was lost on re-raise")
	}
	if st.Reopened != 1 {
		t.Errorf("Reopened = %d, want 1 — re-litigating a dismissed finding is the signal the ledger exists to surface", st.Reopened)
	}

	// Round 3 must still carry the do-not-re-raise warning.
	block := CarryForwardBlock(states)
	if !strings.Contains(block, "do NOT re-raise") || !strings.Contains(block, fid) {
		t.Errorf("carry-forward lost the dismissal warning after a re-raise:\n%s", block)
	}
}

// FoldFindings (what `bts recipe findings list` shows) and SyncFindings
// (what `bts recipe log` prints) must agree on what counts as a reopen.
func TestReopenCountersAgreeBetweenFoldAndSync(t *testing.T) {
	root, id := findingsRoot(t)
	title := "retry policy contradicts timeout"
	report := []ReportedFinding{{Severity: "critical", Title: title}}

	if _, err := SyncFindings(root, id, "draft.md", 1, report, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncFindings(root, id, "draft.md", 2, nil, 3); err != nil { // fixed
		t.Fatal(err)
	}
	sync, err := SyncFindings(root, id, "draft.md", 3, report, 3) // back again
	if err != nil {
		t.Fatal(err)
	}
	states, _ := LoadFindings(root, id, "draft.md")
	if len(sync.Reopened) != states[0].Reopened {
		t.Errorf("SyncResult.Reopened=%d but FindingState.Reopened=%d — the two views disagree",
			len(sync.Reopened), states[0].Reopened)
	}
}
