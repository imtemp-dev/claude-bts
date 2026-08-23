package state

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestVerifyEntriesForDocLegacyLogIsUntouched(t *testing.T) {
	// Pre-v0.10 logs record no Doc. Narrowing them would make every
	// existing recipe read as unverified, so the whole stream is
	// returned instead.
	entries := []VerifyLogEntry{
		{Iteration: 1, Critical: 1},
		{Iteration: 2, Critical: 0},
	}
	got := VerifyEntriesForDoc(entries, "draft.md")
	if len(got) != 2 {
		t.Fatalf("legacy log narrowed to %d entries, want all 2", len(got))
	}
}

func TestVerifyEntriesForDocScopesOnceDocsAreRecorded(t *testing.T) {
	entries := []VerifyLogEntry{
		{Iteration: 1, Doc: "wireframe.md", Critical: 0, Major: 0},
		{Iteration: 2, Doc: "draft.md", Critical: 2, Major: 1},
		{Iteration: 3, Doc: "wireframe.md", Critical: 0, Major: 0},
	}
	draft := VerifyEntriesForDoc(entries, "draft.md")
	if len(draft) != 1 || draft[0].Critical != 2 {
		t.Fatalf("draft.md scope = %+v, want the single dirty draft round", draft)
	}
	// The bug this fixes: a clean wireframe round used to be the "last
	// entry" and would satisfy draft.md's completion gate.
	if last := draft[len(draft)-1]; last.Critical == 0 {
		t.Error("wireframe round leaked into draft.md's verdict")
	}
}

func TestVerifyEntriesForDocAcceptsFullPaths(t *testing.T) {
	entries := []VerifyLogEntry{{Iteration: 1, Doc: "draft.md"}}
	got := VerifyEntriesForDoc(entries, ".jig/specs/recipes/r-001/draft.md")
	if len(got) != 1 {
		t.Errorf("full path should match on basename, got %d entries", len(got))
	}
}

func TestVerifyEntriesForDocUnknownDoc(t *testing.T) {
	entries := []VerifyLogEntry{{Iteration: 1, Doc: "draft.md"}}
	if got := VerifyEntriesForDoc(entries, "domain.md"); len(got) != 0 {
		t.Errorf("unverified doc should have no history, got %d entries", len(got))
	}
}

func TestNormalizeFindingTitleHandlesKoreanTitles(t *testing.T) {
	// Specs in this project are frequently Korean; identity must survive
	// punctuation churn there too.
	a := NormalizeFindingTitle("스크롤 임계값이 명시되지 않음")
	b := NormalizeFindingTitle("**스크롤 임계값이**  명시되지 않음.")
	if a != b {
		t.Errorf("Korean title normalisation diverged: %q vs %q", a, b)
	}
	if a == "" {
		t.Error("Korean title normalised to empty — letters were dropped")
	}
}

func TestEvidenceCacheRoundTrip(t *testing.T) {
	root := t.TempDir()
	e := &EvidenceEntry{
		Library: "SwiftUI", Topic: "safeAreaInset", Claim: "does not propagate to sheets",
		Verdict: EvidenceSilent, Gathered: "Context7:miss | WebFetch:developer.apple.com:200",
	}
	if err := PutEvidence(root, e); err != nil {
		t.Fatal(err)
	}
	// Casing and spacing must not miss.
	got, err := GetEvidence(root, "swiftui", "  safeAreaInset ", "does not propagate to sheets", 30)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("normalised lookup missed a cached entry")
	}
	if got.Verdict != EvidenceSilent {
		t.Errorf("verdict = %q, want %q", got.Verdict, EvidenceSilent)
	}
	if miss, _ := GetEvidence(root, "swiftui", "safeAreaInset", "a different claim", 30); miss != nil {
		t.Error("different claim must not hit")
	}
}

func TestEvidenceUnavailableExpiresQuickly(t *testing.T) {
	now := time.Now().UTC()
	// An outage must not pin a claim to "unavailable" for the full TTL.
	stale := &EvidenceEntry{
		Verdict:   EvidenceUnavailable,
		FetchedAt: now.Add(-2 * time.Hour).Format(time.RFC3339),
	}
	if !EvidenceExpired(stale, 30, now) {
		t.Error("2h-old unavailable entry should have expired")
	}
	fresh := &EvidenceEntry{
		Verdict:   EvidenceUnavailable,
		FetchedAt: now.Add(-10 * time.Minute).Format(time.RFC3339),
	}
	if EvidenceExpired(fresh, 30, now) {
		t.Error("10m-old unavailable entry should still be cached")
	}
	// A real verdict lives for the configured TTL.
	ok := &EvidenceEntry{
		Verdict:   EvidenceConfirms,
		FetchedAt: now.Add(-48 * time.Hour).Format(time.RFC3339),
	}
	if EvidenceExpired(ok, 30, now) {
		t.Error("2-day-old confirms entry should still be live under a 30-day TTL")
	}
	if !EvidenceExpired(ok, 1, now) {
		t.Error("2-day-old entry should expire under a 1-day TTL")
	}
}

func TestEvidenceZeroTTLNeverExpires(t *testing.T) {
	now := time.Now().UTC()
	old := &EvidenceEntry{
		Verdict:   EvidenceConfirms,
		FetchedAt: now.Add(-10000 * time.Hour).Format(time.RFC3339),
	}
	if EvidenceExpired(old, 0, now) {
		t.Error("ttlDays=0 must mean never expire for successful lookups")
	}
}

func TestEvidencePrune(t *testing.T) {
	root := t.TempDir()
	if err := PutEvidence(root, &EvidenceEntry{
		Library: "a", Claim: "x", Verdict: EvidenceConfirms, URLs: []string{"https://go.dev"},
		FetchedAt: time.Now().UTC().Add(-100 * 24 * time.Hour).Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	if err := PutEvidence(root, &EvidenceEntry{
		Library: "b", Claim: "y", Verdict: EvidenceConfirms, URLs: []string{"https://go.dev"},
	}); err != nil {
		t.Fatal(err)
	}
	n, err := PruneEvidence(root, 30)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("pruned %d, want 1", n)
	}
	list, _ := ListEvidence(root)
	if len(list) != 1 {
		t.Errorf("%d entries remain, want 1", len(list))
	}
}

// The spec loop runs /jig-verify and /jig-audit concurrently and
// both gather evidence. A read-modify-write cache silently drops one of
// two simultaneous puts; appends cannot.
func TestEvidenceConcurrentPutsDoNotLoseEntries(t *testing.T) {
	root := t.TempDir()
	const n = 24
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- PutEvidence(root, &EvidenceEntry{
				Library: "lib", Topic: "topic",
				Claim:   fmt.Sprintf("claim number %d", i),
				Verdict: EvidenceSilent, Gathered: "Context7:miss",
			})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent put: %v", err)
		}
	}
	for i := 0; i < n; i++ {
		got, err := GetEvidence(root, "lib", "topic", fmt.Sprintf("claim number %d", i), 30)
		if err != nil {
			t.Fatal(err)
		}
		if got == nil {
			t.Fatalf("entry %d was lost by a concurrent put", i)
		}
	}
}

// Re-putting the same claim must update it, not duplicate it.
func TestEvidencePutOverwritesSameKey(t *testing.T) {
	root := t.TempDir()
	for _, v := range []string{EvidenceUnavailable, EvidenceSilent} {
		if err := PutEvidence(root, &EvidenceEntry{
			Library: "lib", Topic: "t", Claim: "c", Verdict: v,
		}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := ListEvidence(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 folded entry, got %d", len(list))
	}
	if list[0].Verdict != EvidenceSilent {
		t.Errorf("verdict = %q, want the later write %q", list[0].Verdict, EvidenceSilent)
	}
}
