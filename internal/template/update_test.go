package template

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func updateRoot(t *testing.T, recorded string) string {
	t.Helper()
	root := t.TempDir()
	put := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if recorded != "" {
		put(".bts/config/.template-version", recorded)
	}
	for _, rel := range retiredTemplateFiles {
		put(rel, "retired\n")
	}
	put(".bts/config/settings.yaml", "verify:\n  max_iterations: 3\n  convergence:\n    require_zero_major: true\n")
	return root
}

func assertCleaned(t *testing.T, root string, res *UpdateResult) {
	t.Helper()
	for _, rel := range retiredTemplateFiles {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Errorf("%s must be removed (err=%v)", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "bts-simulate")); !os.IsNotExist(err) {
		t.Errorf("the emptied bts-simulate skill directory must go too (err=%v)", err)
	}
	settings, _ := os.ReadFile(filepath.Join(root, ".bts", "config", "settings.yaml"))
	if strings.Contains(string(settings), "convergence") || !strings.Contains(string(settings), "max_iterations: 3") {
		t.Errorf("settings.yaml must lose only the dead key, got:\n%s", settings)
	}
	if len(res.Notes) < 2 {
		t.Errorf("want a note per cleanup step that changed something, got %v", res.Notes)
	}
}

// Trial problem #36: the SessionStart auto-update recorded the new version
// without the cleanup, so `bts update` then said "already up to date" and
// the retired simulate skill stayed listed.  The cleanup must run when the
// recorded version already matches.
func TestUpdate_CleansEvenWhenTheVersionAlreadyMatches(t *testing.T) {
	root := updateRoot(t, "v-current")
	res, err := Update(root, "v-current")
	if err != nil {
		t.Fatal(err)
	}
	if res.Deployed {
		t.Error("a matching version must not redeploy")
	}
	assertCleaned(t, root, res)
	if got, _ := os.ReadFile(filepath.Join(root, ".bts", "config", ".template-version")); string(got) != "v-current" {
		t.Errorf("version must stay v-current, got %q", got)
	}
}

func TestUpdate_DeploysCleansAndRecordsTheVersion(t *testing.T) {
	root := updateRoot(t, "v-old")
	res, err := Update(root, "v-new")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Deployed || res.OldVersion != "v-old" || len(res.Files) == 0 {
		t.Fatalf("want a deploy from v-old, got deployed=%v old=%q files=%d", res.Deployed, res.OldVersion, len(res.Files))
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "bts-prove", "SKILL.md")); err != nil {
		t.Errorf("a live template must be deployed: %v", err)
	}
	assertCleaned(t, root, res)
	if got, _ := os.ReadFile(filepath.Join(root, ".bts", "config", ".template-version")); string(got) != "v-new" {
		t.Errorf("version must be recorded as v-new, got %q", got)
	}
	again, err := Update(root, "v-new")
	if err != nil || again.Deployed || len(again.Notes) != 0 {
		t.Errorf("a second update must be a no-op, got deployed=%v notes=%v err=%v", again.Deployed, again.Notes, err)
	}
}

// The SessionStart hook's stdout is the JSON the harness parses, so Update
// must print nothing: its caller reports the result.
func TestUpdate_WritesNothingToStdout(t *testing.T) {
	root := updateRoot(t, "v-old")
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	_, uerr := Update(root, "v-new")
	os.Stdout = saved
	_ = w.Close()
	out, _ := io.ReadAll(r)
	if uerr != nil {
		t.Fatal(uerr)
	}
	if len(out) != 0 {
		t.Errorf("Update must not write to stdout, got %q", out)
	}
}

// Trial problem #38: after simulation was removed, settings.yaml kept a
// `simulate:` section only because finding_batch was still read from it.
func TestUpdate_MovesSimulateFindingBatchToDefend(t *testing.T) {
	settings := func(t *testing.T, body string) (string, string) {
		t.Helper()
		root := t.TempDir()
		p := filepath.Join(root, ".bts", "config", "settings.yaml")
		_ = os.MkdirAll(filepath.Dir(p), 0755)
		_ = os.WriteFile(p, []byte(body), 0644)
		_ = os.WriteFile(filepath.Join(root, ".bts", "config", ".template-version"), []byte("v"), 0644)
		if _, err := Update(root, "v"); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(p)
		return root, string(got)
	}

	_, got := settings(t, "verify:\n  max_iterations: 3\n\n# Simulation\nsimulate:\n  finding_batch: 4   # old\n")
	if strings.Contains(got, "simulate") {
		t.Errorf("the simulate section must go, got:\n%s", got)
	}
	if v, ok := yamlValue(got, "defend", "finding_batch"); !ok || v != "4" {
		t.Errorf("the old value must move to defend.finding_batch, got %q %v in:\n%s", v, ok, got)
	}
	if v, _ := yamlValue(got, "verify", "max_iterations"); v != "3" {
		t.Errorf("other settings must stay, got:\n%s", got)
	}

	_, got = settings(t, "defend:\n  finding_batch: 8\nsimulate:\n  finding_batch: 4\n")
	if strings.Contains(got, "simulate") {
		t.Errorf("the simulate section must go when defend is set, got:\n%s", got)
	}
	if v, _ := yamlValue(got, "defend", "finding_batch"); v != "8" {
		t.Errorf("defend's own value must win, got %q", v)
	}
}
