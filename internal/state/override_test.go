package state

import (
	"os"
	"strings"
	"testing"
)

func overrideRoot(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	id := "r-001-test"
	if err := os.MkdirAll(RecipeDir(root, id), 0755); err != nil {
		t.Fatal(err)
	}
	return root, id
}

func TestAppendOverrideRequiresGateAndReason(t *testing.T) {
	root, id := overrideRoot(t)
	if err := AppendOverride(root, id, &OverrideRecord{Reason: "because"}); err == nil {
		t.Error("an override with no gate must be refused — there is no blanket override")
	}
	if err := AppendOverride(root, id, &OverrideRecord{Gate: "replicated_clean_pass"}); err == nil {
		t.Error("an override with no reason must be refused")
	}
	if err := AppendOverride(root, id, &OverrideRecord{Gate: "replicated_clean_pass", Reason: " \n "}); err == nil {
		t.Error("whitespace is not a reason")
	}
}

func TestActiveOverrideAppliesOnlyToItsGateAndDoc(t *testing.T) {
	recs := []OverrideRecord{
		{Gate: "replicated_clean_pass", Doc: "draft.md", DocHash: "sha256:aaa", Reason: "r"},
	}
	if st := ActiveOverride(recs, "replicated_clean_pass", "draft.md", "sha256:aaa"); !st.Active {
		t.Error("the granted gate on the granted revision must apply")
	}
	if st := ActiveOverride(recs, "full_pass_before_final", "draft.md", "sha256:aaa"); st.Active {
		t.Error("an override of one gate must not excuse another")
	}
	if st := ActiveOverride(recs, "replicated_clean_pass", "wireframe.md", "sha256:aaa"); st.Active {
		t.Error("an override on draft.md must not excuse wireframe.md")
	}
}

// The operator weighed a specific text. An edit since then is exactly
// when that judgement has to be made again.
func TestOverrideGoesStaleWhenTheRevisionChanges(t *testing.T) {
	recs := []OverrideRecord{
		{Gate: "replicated_clean_pass", Doc: "draft.md", DocHash: "sha256:aaa", Reason: "r"},
	}
	st := ActiveOverride(recs, "replicated_clean_pass", "draft.md", "sha256:bbb")
	if st.Active {
		t.Fatal("an override granted on another revision must not apply")
	}
	if !st.Stale {
		t.Error("and it must be reported as stale, not merely absent — the difference is what the operator needs to hear")
	}
	if st.Granted != "sha256:aaa" {
		t.Errorf("granted revision = %q", st.Granted)
	}
}

// An override recorded without a hash is not revision-pinned. That is
// the legacy shape and the reason --doc is worth passing.
func TestUnpinnedOverrideAppliesAcrossRevisions(t *testing.T) {
	recs := []OverrideRecord{{Gate: "convergence_budget", Reason: "r"}}
	if st := ActiveOverride(recs, "convergence_budget", "draft.md", "sha256:zzz"); !st.Active {
		t.Error("an unpinned override applies until revoked")
	}
}

func TestRevocationCancelsAnEarlierGrant(t *testing.T) {
	recs := []OverrideRecord{
		{Gate: "convergence_budget", Doc: "draft.md", Reason: "grant"},
		{Gate: "convergence_budget", Doc: "draft.md", Reason: "changed my mind", Revoked: true},
	}
	if st := ActiveOverride(recs, "convergence_budget", "draft.md", ""); st.Active {
		t.Error("a revoked override must not apply")
	}
	if live := LiveOverrides(recs); len(live) != 0 {
		t.Errorf("LiveOverrides = %v, want none", live)
	}
	// And a later grant re-arms it.
	recs = append(recs, OverrideRecord{Gate: "convergence_budget", Doc: "draft.md", Reason: "again"})
	if st := ActiveOverride(recs, "convergence_budget", "draft.md", ""); !st.Active {
		t.Error("a grant after a revocation must apply")
	}
}

func TestOverrideSummaryNamesGatesAndFindingCounts(t *testing.T) {
	recs := []OverrideRecord{
		{Gate: "replicated_clean_pass", Doc: "draft.md", Reason: "r"},
		{Gate: "convergence_budget", Doc: "draft.md", Findings: []string{"F-1", "F-2"}, Reason: "r"},
	}
	got := OverrideSummary(recs)
	want := "replicated_clean_pass, convergence_budget(2 findings)"
	if got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}
	if OverrideSummary(nil) != "" {
		t.Error("no overrides must render as empty, so callers can omit the line entirely")
	}
}

func TestOverrideRoundTripsThroughTheLedger(t *testing.T) {
	root, id := overrideRoot(t)
	rec := &OverrideRecord{
		Gate: "replicated_clean_pass", Doc: "draft.md", DocHash: "sha256:aaa",
		Findings: []string{"F-abc12345"}, Reason: "prose-only majors",
	}
	if err := AppendOverride(root, id, rec); err != nil {
		t.Fatal(err)
	}
	if rec.Timestamp == "" {
		t.Error("AppendOverride must stamp a timestamp")
	}
	back, err := ReadOverrides(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].Gate != rec.Gate || len(back[0].Findings) != 1 {
		t.Fatalf("round trip lost data: %+v", back)
	}
}

func TestReadOverridesOnMissingLedgerIsEmptyNotAnError(t *testing.T) {
	root, id := overrideRoot(t)
	recs, err := ReadOverrides(root, id)
	if err != nil || len(recs) != 0 {
		t.Fatalf("got %v, %v — a recipe with no overrides is the normal case", recs, err)
	}
}

// The guard `r.DocHash != "" && docHash != ""` read a missing ROUND hash
// as "no conflict", so a pinned override applied to a round that could
// not say which revision it verified. The gate that fires exactly then
// is revision_recorded_before_final, so the one case the guard existed
// to handle was the one it failed open on.
func TestPinnedOverrideIsStaleAgainstAnUnknownRevision(t *testing.T) {
	records := []OverrideRecord{{
		Gate: "replicated_clean_pass", Doc: "draft.md",
		DocHash: "sha256:aaa", Reason: "frozen for release",
	}}
	st := ActiveOverride(records, "replicated_clean_pass", "draft.md", "")
	if st.Active {
		t.Error("an override pinned to a revision must not excuse a round with no recorded revision")
	}
	if !st.Stale {
		t.Errorf("want stale, got %+v", st)
	}
	if st.Granted != "sha256:aaa" {
		t.Errorf("the stale report must name the revision it was granted on, got %q", st.Granted)
	}
	if st := ActiveOverride(records, "replicated_clean_pass", "draft.md", "sha256:aaa"); !st.Active {
		t.Error("the same revision must still be active")
	}
}

// LiveOverrides keyed revocations the same way as grants, so a revoke
// naming no document filed itself under a key nothing had granted: the
// gate stopped being overridden while status, doctor and stats went on
// displaying it as overridden forever.
func TestRevokeWithoutDocClearsEveryDisplayForThatGate(t *testing.T) {
	records := []OverrideRecord{
		{Gate: "replicated_clean_pass", Doc: "draft.md", DocHash: "sha256:aaa", Reason: "granted"},
		{Gate: "full_pass_before_final", Doc: "draft.md", DocHash: "sha256:aaa", Reason: "other gate"},
		{Gate: "replicated_clean_pass", Reason: "no longer applies", Revoked: true},
	}
	if st := ActiveOverride(records, "replicated_clean_pass", "draft.md", "sha256:aaa"); st.Active {
		t.Fatal("the gate is no longer overridden — precondition")
	}
	live := LiveOverrides(records)
	for _, r := range live {
		if r.Gate == "replicated_clean_pass" {
			t.Errorf("a revoked gate must disappear from the display too, got %+v", live)
		}
	}
	if len(live) != 1 || live[0].Gate != "full_pass_before_final" {
		t.Errorf("the untouched gate must survive the revocation, got %+v", live)
	}
	if s := OverrideSummary(records); strings.Contains(s, "replicated_clean_pass") {
		t.Errorf("status summary still shows the revoked gate: %q", s)
	}
}

// A revocation naming one document must not take back another's.
func TestRevokeWithDocIsNarrow(t *testing.T) {
	records := []OverrideRecord{
		{Gate: "replicated_clean_pass", Doc: "draft.md", DocHash: "sha256:aaa", Reason: "a"},
		{Gate: "replicated_clean_pass", Doc: "wireframe.md", DocHash: "sha256:bbb", Reason: "b"},
		{Gate: "replicated_clean_pass", Doc: "draft.md", Reason: "done with this one", Revoked: true},
	}
	live := LiveOverrides(records)
	if len(live) != 1 || live[0].Doc != "wireframe.md" {
		t.Errorf("only draft.md's override should be revoked, got %+v", live)
	}
}
