package feature

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Code review F3: an edited or missing part of the confirmed copy is an
// error, never served.
func TestConfirmedCopyIsChecked(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tamper func(cdir string)
	}{
		{"edited contracts", func(c string) {
			_ = os.WriteFile(filepath.Join(c, ContractsFile), []byte("# Contracts\n\nchanged after the user said yes\n"), 0644)
		}},
		{"deleted intent", func(c string) { _ = os.Remove(filepath.Join(c, IntentFile)) }},
		{"item added to plan.json", func(c string) {
			data, _ := os.ReadFile(filepath.Join(c, PlanFile))
			_ = os.WriteFile(filepath.Join(c, PlanFile), []byte(strings.Replace(string(data), "[", `[{"id":"i9","title":"x","type":"build"},`, 1)), 0644)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, f := newFeature(t)
			_, _ = AddItem(dir, ItemSpec{Title: ptr("a"), Type: ptr("build")})
			if _, err := Confirm(dir, "go"); err != nil {
				t.Fatal(err)
			}
			tc.tamper(filepath.Join(dir, ConfirmedDir, "1"))
			f, _ = Load(dir)
			if _, _, err := Confirmed(dir, f); err == nil {
				t.Fatal("Confirmed must refuse a changed copy")
			}
			if _, _, err := Context(f.ID, "i1", ""); err == nil {
				t.Fatal("Context must refuse a changed copy")
			}
		})
	}
}

// Code review F4: an item removed after confirmation is refused, not a
// panic, and still shows in the status until the removal is confirmed.
func TestClaimOnItemRemovedAfterConfirm(t *testing.T) {
	dir, f := newFeature(t)
	_, _ = AddItem(dir, ItemSpec{Title: ptr("a"), Type: ptr("build")})
	i2, _ := AddItem(dir, ItemSpec{Title: ptr("b"), Type: ptr("build")})
	_, _ = Confirm(dir, "go")
	if err := RemoveItem(dir, i2); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, ".bts", "specs", "recipes"), 0755)
	if _, err := BeginClaim(ClaimRequest{FeatureID: f.ID, ItemID: i2, Root: root}); err == nil || !strings.Contains(err.Error(), "removed") {
		t.Fatalf("claim on a removed item: %v", err)
	}
	f, _ = Load(dir)
	s, _ := Derive(dir, f)
	if it := s.item(i2); it == nil || !it.Removed || !strings.Contains(it.Note, "removed") {
		t.Fatalf("the removed item must still show: %+v", it)
	}
	if s.State == FeatureReady {
		t.Fatal("a confirmed item still in force keeps the feature from ready")
	}
}

// Should-fix 5: a dead claim directory between attempts does not block the
// item for ever.
func TestAttemptNumberingSkipsAGap(t *testing.T) {
	dir, f := newFeature(t)
	i1, _ := AddItem(dir, ItemSpec{Title: ptr("a"), Type: ptr("build")})
	_, _ = Confirm(dir, "go")
	writeAttempt(t, dir, i1, 1, "cancelled", "cancelled", "")
	dead := filepath.Join(dir, ItemsDir, i1, "a2")
	_ = os.MkdirAll(dead, 0755)
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(dead, old, old)
	writeAttempt(t, dir, i1, 3, "cancelled", "cancelled", "")
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, ".bts", "specs", "recipes"), 0755)
	c, err := BeginClaim(ClaimRequest{FeatureID: f.ID, ItemID: i1, Root: root})
	if err != nil || c.Attempt != 4 {
		t.Fatalf("claim after a gap: %+v %v", c, err)
	}
}

// Should-fix 7, 11: a key cannot leave the store; ids parse loosely.
func TestKeyStaysInStoreAndLooseIDs(t *testing.T) {
	for _, u := range []string{"git@host:../../../escaped/repo.git", "https://host/a/../b", "git@host:a//b"} {
		if k := RepoKey(u); k != "" {
			t.Errorf("RepoKey(%q) = %q, want empty", u, k)
		}
	}
	if ValidKey("../x") || ValidKey("/abs") || !ValidKey("local/app-1234") {
		t.Fatal("ValidKey")
	}
	dir, _ := newFeature(t)
	if got, err := Find("f-1"); err != nil || got != dir {
		t.Fatalf("Find(f-1) = %q, %v", got, err)
	}
}

// Should-fix 6: the lock serialises concurrent manager commands.
func TestConcurrentMutatesAllLand(t *testing.T) {
	dir, _ := newFeature(t)
	var wg sync.WaitGroup
	for k := 0; k < 10; k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := AddItem(dir, ItemSpec{Title: ptr("x"), Type: ptr("build")}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	f, _ := Load(dir)
	if len(f.Items) != 10 || f.Rev != 10 {
		t.Fatalf("items %d rev %d, want 10 and 10", len(f.Items), f.Rev)
	}
}

// A released attempt that finishes later stays released.
func TestReleasedAttemptStaysReleased(t *testing.T) {
	dir, _ := newFeature(t)
	i1, _ := AddItem(dir, ItemSpec{Title: ptr("a"), Type: ptr("build")})
	_, _ = Confirm(dir, "go")
	writeAttempt(t, dir, i1, 1, "implement", "", "")
	if _, err := ReleaseItem(dir, i1, "the run died"); err != nil {
		t.Fatal(err)
	}
	writeAttempt(t, dir, i1, 1, "complete", "done", "gate")
	f, _ := Load(dir)
	if s, _ := Derive(dir, f); s.item(i1).State != StateClaimable {
		t.Fatalf("a released attempt must not count: %s", s.item(i1).State)
	}
}

// Should-fix 8: skip is refused while an attempt is open, and restore
// undoes a skip.
func TestSkipWaitsAndRestoreUndoes(t *testing.T) {
	dir, _ := newFeature(t)
	i1, _ := AddItem(dir, ItemSpec{Title: ptr("a"), Type: ptr("build")})
	_, _ = Confirm(dir, "go")
	writeAttempt(t, dir, i1, 1, "implement", "", "")
	if err := DecideItem(dir, i1, "skip", "no"); err == nil || !strings.Contains(err.Error(), "open attempt") {
		t.Fatalf("skip with an open attempt: %v", err)
	}
	writeAttempt(t, dir, i1, 1, "cancelled", "cancelled", "")
	if err := DecideItem(dir, i1, "skip", "not needed"); err != nil {
		t.Fatal(err)
	}
	if err := RestoreItem(dir, i1, "needed after all"); err != nil {
		t.Fatal(err)
	}
	f, _ := Load(dir)
	if s, _ := Derive(dir, f); s.item(i1).State != StateClaimable {
		t.Fatalf("after restore: %s", s.item(i1).State)
	}
}

// Should-fix 11: a reconcile with nothing new writes nothing.
func TestReconcileWithoutChangeKeepsRev(t *testing.T) {
	dir, _ := newFeature(t)
	i1, _ := AddItem(dir, ItemSpec{Title: ptr("a"), Type: ptr("build")})
	_, _ = Confirm(dir, "go")
	writeAttempt(t, dir, i1, 1, "implement", "", "")
	_, _ = Reconcile(dir)
	f, _ := Load(dir)
	rev := f.Rev
	_, _ = Reconcile(dir)
	f, _ = Load(dir)
	if f.Rev != rev {
		t.Fatalf("rev moved from %d to %d with nothing new", rev, f.Rev)
	}
}

// Should-fix 4: a snapshot set aside by a sync that died between its two
// renames is still read.
func TestSnapshotSetAsideIsRead(t *testing.T) {
	dir, _ := newFeature(t)
	i1, _ := AddItem(dir, ItemSpec{Title: ptr("a"), Type: ptr("build")})
	_, _ = Confirm(dir, "go")
	writeAttempt(t, dir, i1, 1, "complete", "done", "gate")
	ad := filepath.Join(dir, ItemsDir, i1, "a1")
	if err := os.Rename(filepath.Join(ad, SnapshotDir), filepath.Join(ad, ".prev-1")); err != nil {
		t.Fatal(err)
	}
	f, _ := Load(dir)
	if s, _ := Derive(dir, f); s.item(i1).State != StateDone {
		t.Fatalf("the set-aside snapshot must be read: %s", s.item(i1).State)
	}
}
