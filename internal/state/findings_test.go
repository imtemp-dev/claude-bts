package state

import (
	"os"
	"path/filepath"
	"slices"
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
	r1, err := SyncFindings(root, id, legacyRound("draft.md", 1), []ReportedFinding{
		{Severity: "critical", Title: "contradictory retry policy"},
		{Severity: "major", Title: "missing error path for timeout"},
	}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(r1.New) != 2 || len(r1.Carried) != 0 {
		t.Fatalf("round 1: new=%v carried=%v, want 2 new", r1.New, r1.Carried)
	}

	// Round 2: the critical goes silent. Absence alone is not a closure —
	// it demotes to unreported and stays owed.
	r2, err := SyncFindings(root, id, legacyRound("draft.md", 2), []ReportedFinding{
		{Severity: "major", Title: "missing error path for timeout"},
	}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Fixed) != 0 {
		t.Errorf("round 2: fixed=%v, want 0 — one silent round must not close a finding", r2.Fixed)
	}
	if len(r2.Unreported) != 1 {
		t.Errorf("round 2: unreported=%v, want 1", r2.Unreported)
	}
	if len(r2.Carried) != 1 {
		t.Errorf("round 2: carried=%v, want 1", r2.Carried)
	}

	// Round 3: it comes back. It was never confirmed fixed, so this is
	// the finding still being open — not a regression.
	r3, err := SyncFindings(root, id, legacyRound("draft.md", 3), []ReportedFinding{
		{Severity: "critical", Title: "contradictory retry policy"},
		{Severity: "major", Title: "missing error path for timeout"},
	}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(r3.Reopened) != 0 {
		t.Fatalf("round 3: reopened=%v, want 0 — an unconfirmed finding returning is not a regression", r3.Reopened)
	}
	if len(r3.Carried) != 2 {
		t.Fatalf("round 3: carried=%v, want both findings carried", r3.Carried)
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
	if crit.Status != FindingOpen {
		t.Errorf("critical status = %s, want open", crit.Status)
	}
	if crit.OpenRounds != 2 {
		t.Errorf("critical open rounds = %d, want 2", crit.OpenRounds)
	}
}

// A finding stays owed until two consecutive rounds go silent on it, and
// only then when its anchor has gone quiet too.
func TestAbsenceClosesOnlyAfterConfirmation(t *testing.T) {
	root, id := findingsRoot(t)
	if _, err := SyncFindings(root, id, legacyRound("draft.md", 1), []ReportedFinding{
		{Severity: "major", Title: "retry budget is never bounded", Anchor: "## Retry"},
	}, 0); err != nil {
		t.Fatal(err)
	}
	r2, err := SyncFindings(root, id, legacyRound("draft.md", 2), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Unreported) != 1 || len(r2.Fixed) != 0 {
		t.Fatalf("round 2: unreported=%v fixed=%v, want the finding held", r2.Unreported, r2.Fixed)
	}
	r3, err := SyncFindings(root, id, legacyRound("draft.md", 3), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(r3.Fixed) != 1 {
		t.Fatalf("round 3: fixed=%v, want the confirmed closure", r3.Fixed)
	}
	states, _ := LoadFindings(root, id, "draft.md")
	if states[0].Status != FindingFixed {
		t.Errorf("status = %s, want fixed after two silent rounds", states[0].Status)
	}
}

// The restatement signature: a finding goes silent in the same round a
// NEW finding lands on its anchor. That is what a verifier rewording the
// same defect produces, and it must not read as progress.
func TestRestatementAtTheSameAnchorDoesNotClose(t *testing.T) {
	root, id := findingsRoot(t)
	if _, err := SyncFindings(root, id, legacyRound("draft.md", 1), []ReportedFinding{
		{Severity: "minor_resolvable", Title: "the example writes a misspelled extra_config key", Anchor: "## The association"},
	}, 0); err != nil {
		t.Fatal(err)
	}
	// Round 2: silent on the original, but the same anchor produces a
	// differently-worded finding.
	r2, err := SyncFindings(root, id, legacyRound("draft.md", 2), []ReportedFinding{
		{Severity: "minor_resolvable", Title: "the shallow-copy leak example misspells the reasoning_effort key", Anchor: "## The association"},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Unreported) != 1 || len(r2.Fixed) != 0 {
		t.Fatalf("round 2: unreported=%v fixed=%v", r2.Unreported, r2.Fixed)
	}
	// Round 3: the anchor keeps producing. The original is still held.
	r3, err := SyncFindings(root, id, legacyRound("draft.md", 3), []ReportedFinding{
		{Severity: "minor_resolvable", Title: "the leak example names a key that no reader can resolve", Anchor: "## The association"},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(r3.Fixed) != 0 {
		t.Errorf("round 3: fixed=%v, want 0 — the anchor is still generating findings", r3.Fixed)
	}
	if len(r3.Restated) != 1 {
		t.Errorf("round 3: restated=%v, want the held finding named", r3.Restated)
	}
	// Round 4: the anchor finally goes quiet. Now the held finding may
	// close — as may the round-2 restatement, which has also had its two
	// silent rounds by now. The round-3 one has had only its first.
	r4, err := SyncFindings(root, id, legacyRound("draft.md", 4), []ReportedFinding{
		{Severity: "major", Title: "an unrelated defect elsewhere", Anchor: "## Files"},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	orig := FindingID("draft.md", "the example writes a misspelled extra_config key")
	if !slices.Contains(r4.Fixed, orig) {
		t.Errorf("round 4: fixed=%v, want it to include the held finding %s once the anchor went quiet",
			r4.Fixed, orig)
	}
	newest := FindingID("draft.md", "the leak example names a key that no reader can resolve")
	if slices.Contains(r4.Fixed, newest) {
		t.Errorf("round 4: %s closed after only one silent round", newest)
	}
}

// The hold has to survive the restatement becoming ordinary. The first
// version of this rule marked an anchor "hot" only from findings that
// were NEW in the round being processed, so a restatement was hot for
// exactly one round: by the next one it was merely carried, the anchor
// read as quiet, and the original closed as `fixed`. The test that was
// supposed to catch this invented a fresh title every round, which kept
// the anchor artificially hot and hid the defect.
func TestRestatementHoldsWhileTheSameWordingPersists(t *testing.T) {
	root, id := findingsRoot(t)
	const anchor = "## The association"
	orig := "the example writes a misspelled extra_config key"
	restated := "the shallow-copy leak example misspells the reasoning_effort key"

	if _, err := SyncFindings(root, id, legacyRound("draft.md", 1), []ReportedFinding{
		{Severity: "minor_resolvable", Title: orig, Anchor: anchor},
	}, 0); err != nil {
		t.Fatal(err)
	}
	// Round 2: the original goes silent, a reworded one appears.
	if _, err := SyncFindings(root, id, legacyRound("draft.md", 2), []ReportedFinding{
		{Severity: "minor_resolvable", Title: restated, Anchor: anchor},
	}, 0); err != nil {
		t.Fatal(err)
	}
	// Rounds 3-5: the SAME restatement is reported again each time, so
	// it is carried, not new. The anchor is still producing findings.
	for iter := 3; iter <= 5; iter++ {
		r, err := SyncFindings(root, id, legacyRound("draft.md", iter), []ReportedFinding{
			{Severity: "minor_resolvable", Title: restated, Anchor: anchor},
		}, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Fixed) != 0 {
			t.Fatalf("round %d: fixed=%v, want 0 — the same defect is still being reported at this anchor",
				iter, r.Fixed)
		}
		if !slices.Contains(r.Restated, FindingID("draft.md", orig)) {
			t.Fatalf("round %d: restated=%v, want the held original named", iter, r.Restated)
		}
	}
	// The anchor finally clears. Now the original may close.
	r6, err := SyncFindings(root, id, legacyRound("draft.md", 6), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r6.Fixed, FindingID("draft.md", orig)) {
		t.Errorf("round 6: fixed=%v, want the original once the anchor went quiet", r6.Fixed)
	}
}

// A deferred watch-item is an accepted carry-forward, not evidence the
// section is still defective, so it must not hold its neighbours open
// forever.
func TestDeferredItemDoesNotKeepAnAnchorHot(t *testing.T) {
	root, id := findingsRoot(t)
	const anchor = "## Retry policy"
	orig := "the backoff ceiling contradicts the timeout"
	if _, err := SyncFindings(root, id, legacyRound("draft.md", 1), []ReportedFinding{
		{Severity: "major", Title: orig, Anchor: anchor},
		{Severity: "minor_deferred", Title: "jitter strategy left to runtime", Anchor: anchor},
	}, 0); err != nil {
		t.Fatal(err)
	}
	deferredOnly := []ReportedFinding{
		{Severity: "minor_deferred", Title: "jitter strategy left to runtime", Anchor: anchor},
	}
	if _, err := SyncFindings(root, id, legacyRound("draft.md", 2), deferredOnly, 0); err != nil {
		t.Fatal(err)
	}
	r3, err := SyncFindings(root, id, legacyRound("draft.md", 3), deferredOnly, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r3.Fixed, FindingID("draft.md", orig)) {
		t.Errorf("fixed=%v, want the repaired major — only a deferred watch-item remains at the anchor", r3.Fixed)
	}
}

// A genuine regression — confirmed fixed, then raised again — must still
// be counted as a reopen.
func TestConfirmedFixedThenRaisedAgainIsAReopen(t *testing.T) {
	root, id := findingsRoot(t)
	title := "contradictory retry policy"
	if _, err := SyncFindings(root, id, legacyRound("draft.md", 1), []ReportedFinding{
		{Severity: "critical", Title: title},
	}, 0); err != nil {
		t.Fatal(err)
	}
	for _, it := range []int{2, 3} { // two silent rounds → confirmed fixed
		if _, err := SyncFindings(root, id, legacyRound("draft.md", it), nil, 0); err != nil {
			t.Fatal(err)
		}
	}
	r4, err := SyncFindings(root, id, legacyRound("draft.md", 4), []ReportedFinding{
		{Severity: "critical", Title: title},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(r4.Reopened) != 1 {
		t.Fatalf("round 4: reopened=%v, want 1", r4.Reopened)
	}
	states, _ := LoadFindings(root, id, "draft.md")
	if states[0].Reopened != 1 {
		t.Errorf("reopened count = %d, want 1", states[0].Reopened)
	}
}

func TestSyncFindingsScopesByDocument(t *testing.T) {
	root, id := findingsRoot(t)
	if _, err := SyncFindings(root, id, legacyRound("wireframe.md", 1), []ReportedFinding{
		{Severity: "major", Title: "component boundary undefined"},
	}, 3); err != nil {
		t.Fatal(err)
	}
	// A draft round must not close the wireframe's open finding.
	if _, err := SyncFindings(root, id, legacyRound("draft.md", 2), []ReportedFinding{
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
	if _, err := SyncFindings(root, id, legacyRound("draft.md", 1), []ReportedFinding{
		{Severity: "minor_deferred", Title: "measured scroll threshold unknown until device test"},
	}, 3); err != nil {
		t.Fatal(err)
	}
	// A later round that does not repeat it must not mark it fixed —
	// deferred items are runtime watch-items carried into implement.
	r2, err := SyncFindings(root, id, legacyRound("draft.md", 2), nil, 3)
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
	if _, err := SyncFindings(root, id, legacyRound("draft.md", 1), []ReportedFinding{
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
	r, err := SyncFindings(root, id, legacyRound("draft.md", 2), []ReportedFinding{
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
	if _, err := SyncFindings(root, id, legacyRound("draft.md", 1), []ReportedFinding{
		{Severity: "major", Title: title},
	}, 3); err != nil {
		t.Fatal(err)
	}
	fid := FindingID("draft.md", title)
	if err := DismissFinding(root, id, fid, "official docs confirm the hook is current"); err != nil {
		t.Fatal(err)
	}
	// Round 2: a fresh verifier raises it again.
	if _, err := SyncFindings(root, id, legacyRound("draft.md", 2), []ReportedFinding{
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

	if _, err := SyncFindings(root, id, legacyRound("draft.md", 1), report, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncFindings(root, id, legacyRound("draft.md", 2), nil, 3); err != nil { // fixed
		t.Fatal(err)
	}
	sync, err := SyncFindings(root, id, legacyRound("draft.md", 3), report, 3) // back again
	if err != nil {
		t.Fatal(err)
	}
	states, _ := LoadFindings(root, id, "draft.md")
	if len(sync.Reopened) != states[0].Reopened {
		t.Errorf("SyncResult.Reopened=%d but FindingState.Reopened=%d — the two views disagree",
			len(sync.Reopened), states[0].Reopened)
	}
}

// legacyRound is a round that does not say which instruments it ran —
// the shape every ledger written before VerifyLogEntry.Dimensions has.
// The tests above are the legacy contract, so they keep it: roundCovers
// only tightens on rounds that DO declare a class.
func legacyRound(doc string, iteration int) *VerifyLogEntry {
	return &VerifyLogEntry{Doc: doc, Iteration: iteration}
}

// The measured failure this gate exists to remove. Three consecutive
// rounds ran verify, then audit, then simulate against a BYTE-IDENTICAL
// document, and three findings the first round raised — one CRITICAL —
// closed as `fixed` without anyone touching a line. An audit has no
// reason to restate a logical inconsistency; its silence about one is
// not evidence.
func TestSilenceFromAnotherInstrumentClosesNothing(t *testing.T) {
	root, id := findingsRoot(t)
	round := func(n int, dims ...string) *VerifyLogEntry {
		return &VerifyLogEntry{Doc: "draft.md", Iteration: n, FullPass: true, Dimensions: dims}
	}
	const claim = "contradictory retry policy"

	if _, err := SyncFindings(root, id, round(1, "verify"),
		[]ReportedFinding{{Severity: "critical", Title: claim, Anchor: "§5"}}, 0); err != nil {
		t.Fatal(err)
	}
	// The audit finds its own thing and says nothing about the critical.
	r2, err := SyncFindings(root, id, round(2, "audit"),
		[]ReportedFinding{{Severity: "major", Title: "no rollback for step 2", Anchor: "§5"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Unreported) != 0 {
		t.Errorf("an audit must not demote a verify finding, got unreported=%v", r2.Unreported)
	}
	if len(r2.Unjudged) != 1 {
		t.Errorf("the verify finding should be reported as unjudged, got %v", r2.Unjudged)
	}
	// And a third round on yet another instrument still closes nothing.
	r3, err := SyncFindings(root, id, round(3, "simulate"),
		[]ReportedFinding{{Severity: "major", Title: "cell reuse races", Anchor: "§3"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(r3.Fixed) != 0 {
		t.Fatalf("nothing was fixed on an unchanged document, got %v", r3.Fixed)
	}
	states, err := LoadFindings(root, id, "draft.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range states {
		if st.Title == claim && st.Status != FindingOpen {
			t.Errorf("the critical must still be open, got %q", st.Status)
		}
	}
}

// The gate is one-directional: a round that ran the instrument may still
// close what that instrument raised, or the ledger would never close
// anything once dimensions are recorded.
func TestARoundThatRanTheInstrumentStillCloses(t *testing.T) {
	root, id := findingsRoot(t)
	all := []string{"verify", "audit", "simulate"}
	const claim = "contradictory retry policy"

	if _, err := SyncFindings(root, id,
		&VerifyLogEntry{Doc: "draft.md", Iteration: 1, FullPass: true, Dimensions: []string{"verify"}},
		[]ReportedFinding{{Severity: "critical", Title: claim}}, 0); err != nil {
		t.Fatal(err)
	}
	for i, want := range []int{0, 1} { // demoted, then closed
		r, err := SyncFindings(root, id,
			&VerifyLogEntry{Doc: "draft.md", Iteration: i + 2, FullPass: true, Dimensions: all},
			nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Fixed) != want {
			t.Errorf("round %d: fixed=%v, want %d", i+2, r.Fixed, want)
		}
	}
}

// A delta round read part of the document. Its silence about the rest is
// the scope, not the finding's absence.
func TestDeltaRoundDemotesNothing(t *testing.T) {
	root, id := findingsRoot(t)
	if _, err := SyncFindings(root, id,
		&VerifyLogEntry{Doc: "draft.md", Iteration: 1, FullPass: true, Dimensions: []string{"verify"}},
		[]ReportedFinding{{Severity: "major", Title: "missing timeout path"}}, 0); err != nil {
		t.Fatal(err)
	}
	r, err := SyncFindings(root, id,
		&VerifyLogEntry{Doc: "draft.md", Iteration: 2, FullPass: false, Dimensions: []string{"verify"}},
		nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Unreported) != 0 {
		t.Errorf("a delta round must not demote, got %v", r.Unreported)
	}
}

// The two defects the r-026 ledger showed: a finding re-rated between
// rounds, and one defect written twice in two languages.

func TestSyncFindingsReportsReclassification(t *testing.T) {
	root, id := findingsRoot(t)

	// Round 1 (verify) rates it major.
	if _, err := SyncFindings(root, id, legacyRound("draft.md", 1),
		[]ReportedFinding{{Severity: "major", Title: "§4 row count disagrees with the wireframe", Anchor: "§4"}}, 3); err != nil {
		t.Fatal(err)
	}
	// Round 2 (simulate) re-rates the SAME finding minor_resolvable over
	// an untouched document.
	res, err := SyncFindings(root, id, legacyRound("draft.md", 2),
		[]ReportedFinding{{Severity: "minor_resolvable", Title: "§4 row count disagrees with the wireframe", Anchor: "§4"}}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Reclassified) != 1 {
		t.Fatalf("want 1 reclassification, got %d (%+v)", len(res.Reclassified), res.Reclassified)
	}
	if got := res.Reclassified[0]; got.From != "major" || got.To != "minor_resolvable" {
		t.Fatalf("want major → minor_resolvable, got %s → %s", got.From, got.To)
	}
	// An unchanged severity is not movement.
	res, err = SyncFindings(root, id, legacyRound("draft.md", 3),
		[]ReportedFinding{{Severity: "minor_resolvable", Title: "§4 row count disagrees with the wireframe", Anchor: "§4"}}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Reclassified) != 0 {
		t.Fatalf("same severity must not be reported as movement: %+v", res.Reclassified)
	}
}

func TestNominateDuplicatesAcrossLanguages(t *testing.T) {
	// Verbatim from r-026: one defect, Korean and English, one anchor.
	reported := []ReportedFinding{
		{Severity: "minor_resolvable", Anchor: "§5.5",
			Title: "verify.sql 호출 횟수가 '5회'로 적혀 있으나 실제는 11개 위치인자 10회 + 12개 1회"},
		{Severity: "minor_resolvable", Anchor: "§5.5",
			Title: "verify.sql call-site count in §5 is understated"},
		// Same anchor, genuinely unrelated: no shared artefact or number.
		{Severity: "major", Anchor: "§5.5",
			Title: "the recreated grants are not stated as part of the contract"},
		// Same artefact, different anchor: location must agree too.
		{Severity: "major", Anchor: "§2",
			Title: "verify.sql is not listed among the touched files"},
		// Anchorless: too weak to nominate.
		{Severity: "major", Title: "verify.sql ownership is unassigned"},
	}
	got := nominateDuplicates("draft.md", reported)
	if len(got) != 1 {
		t.Fatalf("want exactly the cross-language pair, got %d: %+v", len(got), got)
	}
	if got[0].Anchor != "§5.5" || !slices.Contains(got[0].Shared, "verify.sql") {
		t.Fatalf("want §5.5 naming verify.sql, got %s/%v", got[0].Anchor, got[0].Shared)
	}
	if got[0].A == got[0].B {
		t.Fatal("a finding must not be nominated against itself")
	}
}

func TestNominateDuplicatesSharedDigitPair(t *testing.T) {
	// Also from r-026: the §5.4 signature pair shares only the number 12,
	// and both halves were rated differently.
	reported := []ReportedFinding{
		{Severity: "major", Anchor: "§3 / §5.4",
			Title: "§3이 지명한 12인자 시그니처의 소유자(§5.4)가 그 시그니처를 담고 있지 않다"},
		{Severity: "minor_resolvable", Anchor: "§3 / §5.4",
			Title: "§3이 'drop할 12인자 시그니처는 §5.4가 소유한다'고 위임하지만 §5.4에 시그니처가 없다"},
	}
	got := nominateDuplicates("draft.md", reported)
	if len(got) != 1 {
		t.Fatalf("want 1 nomination, got %d: %+v", len(got), got)
	}
	if !slices.Contains(got[0].Shared, "12") {
		t.Fatalf("want the shared quantity 12, got %v", got[0].Shared)
	}
}

func TestInvariantTokensDropsSingleChars(t *testing.T) {
	// A lone digit is a section number: shared by half the findings in
	// any document, and worthless as identity.
	toks := invariantTokens("§5 is wrong and §3 disagrees")
	for tok := range toks {
		if len(tok) < 2 {
			t.Fatalf("single-character token survived: %q", tok)
		}
	}
}

// The known over-nomination, pinned so a future tightening has to face
// it: two unrelated defects that both name ArtworkView on one anchor.
// It is raised, and the operator dismisses it. See DuplicateCandidate
// for why erring this way is deliberate.
func TestNominateDuplicatesOverNominatesSharedSubject(t *testing.T) {
	reported := []ReportedFinding{
		{Severity: "major", Anchor: "§3.1",
			Title: "U-006의 ArtworkView 회귀 표면이 10곳 중 3곳만 적혀 있다"},
		{Severity: "minor_resolvable", Anchor: "§3.1",
			Title: "ArtworkView의 file:// 소비자 둘이 RemoteImageStore 추출 시 조용히 빌 수 있다"},
	}
	got := nominateDuplicates("draft.md", reported)
	if len(got) != 1 || !slices.Contains(got[0].Shared, "ArtworkView") {
		t.Fatalf("want the ArtworkView pair raised for the operator, got %+v", got)
	}
}
