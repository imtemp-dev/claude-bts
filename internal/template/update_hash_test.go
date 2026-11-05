package template

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Update deployed only when the recorded template version differed, and
// the version is the release plus the commit, so a binary built from a
// tree with uncommitted template changes kept the version and never
// redeployed them (trial problem #15, Multica AGEN-65).  A hash of the
// binary's templates is recorded beside the version, and a different one
// deploys.  A project with no recorded hash (deployed before hashes were
// kept) is still judged by the version alone.
func TestUpdate_RedeploysTemplatesThatChangedUnderTheSameVersion(t *testing.T) {
	root := updateRoot(t, "v-current")
	hashFile := filepath.Join(root, ".bts", "config", ".template-hash")
	if err := os.WriteFile(hashFile, []byte("templates-of-an-older-build\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := Update(root, "v-current")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Deployed {
		t.Fatal("templates that changed under the same version must be deployed")
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "bts-prove", "SKILL.md")); err != nil {
		t.Errorf("a live template must be deployed: %v", err)
	}
	got, _ := os.ReadFile(hashFile)
	if h := strings.TrimSpace(string(got)); h == "" || h == "templates-of-an-older-build" {
		t.Fatalf("the deploy must record this binary's template hash, got %q", h)
	}
	again, err := Update(root, "v-current")
	if err != nil || again.Deployed {
		t.Fatalf("with version and hash both current a second update must not deploy: deployed=%v err=%v", again.Deployed, err)
	}
}
