package pages

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Publishing follows the Artifact tool's rules: a new file makes a page,
// the same file again updates it, --page names one, --new forces another.
func TestPublish_NewSameFileNamedAndForced(t *testing.T) {
	home := t.TempDir()
	src := t.TempDir()
	page := writeFile(t, filepath.Join(src, "explainer.html"), "<html><head><title>Tron run lifecycle</title></head><body><img src=\"img/a.png\"></body></html>")
	img := writeFile(t, filepath.Join(src, "a.png"), "PNG1")

	p, created, err := Publish(home, PublishOptions{File: page, Files: map[string]string{"img/a.png": img}, Description: "how a run moves"})
	if err != nil || !created {
		t.Fatalf("first publish: %v, created %v", err, created)
	}
	if !regexp.MustCompile(`^tron-run-lifecycle-[a-z2-9]{4}$`).MatchString(p.ID) || p.Version != 1 || p.Title != "Tron run lifecycle" {
		t.Fatalf("page %+v", p)
	}
	if got := readFile(t, filepath.Join(CurrentDir(home, p), "img", "a.png")); got != "PNG1" {
		t.Errorf("supporting file %q", got)
	}

	// The same file again: a new version of the same page; the image it
	// did not name again is kept.
	writeFile(t, page, "<html><head><title>Tron run lifecycle, v2</title></head><body>two</body></html>")
	p2, created, err := Publish(home, PublishOptions{File: page})
	if err != nil || created || p2.ID != p.ID || p2.Version != 2 || p2.Title != "Tron run lifecycle, v2" {
		t.Fatalf("republish: %+v, created %v, %v", p2, created, err)
	}
	if got := readFile(t, filepath.Join(CurrentDir(home, p2), "img", "a.png")); got != "PNG1" {
		t.Errorf("the unnamed supporting file was not carried over: %q", got)
	}
	if got := readFile(t, filepath.Join(VersionDir(home, p.ID, 1), PageFile)); !strings.Contains(got, "<img") {
		t.Errorf("version 1 was overwritten: %q", got)
	}
	if p2.Description != "how a run moves" || len(p2.Versions) != 2 {
		t.Errorf("record %+v", p2)
	}

	// Another file, named by --page: still the same page; an empty source
	// removes a supporting file.
	other := writeFile(t, filepath.Join(src, "copy.html"), "<html><body>three</body></html>")
	p3, created, err := Publish(home, PublishOptions{File: other, Page: p.ID, Files: map[string]string{"img/a.png": ""}, Title: "Renamed"})
	if err != nil || created || p3.ID != p.ID || p3.Version != 3 || p3.Title != "Renamed" {
		t.Fatalf("--page: %+v, %v", p3, err)
	}
	if _, err := os.Stat(filepath.Join(CurrentDir(home, p3), "img", "a.png")); err == nil {
		t.Error("a removed supporting file is still there")
	}

	// --new: the same file makes another page.
	p4, created, err := Publish(home, PublishOptions{File: page, New: true})
	if err != nil || !created || p4.ID == p.ID {
		t.Fatalf("--new: %+v, %v", p4, err)
	}
	list, _ := List(home)
	if len(list) != 2 || list[0].ID != p4.ID {
		t.Errorf("list %v", list)
	}
}

func TestPublish_RefusesBadInput(t *testing.T) {
	home := t.TempDir()
	src := t.TempDir()
	page := writeFile(t, filepath.Join(src, "p.html"), "<title>x</title>")
	md := writeFile(t, filepath.Join(src, "p.md"), "# x")
	img := writeFile(t, filepath.Join(src, "a.png"), "x")
	if _, _, err := Publish(home, PublishOptions{File: md}); err == nil {
		t.Error("a Markdown file was published")
	}
	for _, bad := range []string{"../a.png", "/abs.png", ".hidden", "a/../b.png", PageFile, "v/1/x.png", ""} {
		if _, _, err := Publish(home, PublishOptions{File: page, Files: map[string]string{bad: img}}); err == nil {
			t.Errorf("published path %q was accepted", bad)
		}
	}
	if _, _, err := Publish(home, PublishOptions{File: page, Files: map[string]string{"a.png": filepath.Join(src, "missing.png")}}); err == nil {
		t.Error("a missing supporting file was accepted")
	}
	if _, _, err := Publish(home, PublishOptions{File: page, Page: "no-such-page"}); err == nil {
		t.Error("an unknown --page was accepted")
	}
	if list, _ := List(home); len(list) != 0 {
		t.Errorf("a refused publish left pages: %v", list)
	}
}

func TestPageID_FallsBackForTitlesWithoutLatinLetters(t *testing.T) {
	// A title in another script ("page" in Greek) has no slug.
	id, err := NewPageID("Σελίδα")
	if err != nil || !strings.HasPrefix(id, "page-") || !ValidPageID(id) {
		t.Errorf("id %q, %v", id, err)
	}
	long, _ := NewPageID(strings.Repeat("architecture ", 10))
	if !ValidPageID(long) || len(long) > 50 {
		t.Errorf("long id %q", long)
	}
}

func TestDelete_RemovesEveryVersion(t *testing.T) {
	home := t.TempDir()
	page := writeFile(t, filepath.Join(t.TempDir(), "p.html"), "<title>Gone</title>")
	p, _, err := Publish(home, PublishOptions{File: page})
	if err != nil {
		t.Fatal(err)
	}
	if err := Delete(home, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := Get(home, p.ID); err == nil {
		t.Error("a deleted page still reads")
	}
	if err := Delete(home, "../etc"); err == nil {
		t.Error("a bad id was deleted")
	}
}

func TestProjects_KeyRegisterAndPrune(t *testing.T) {
	home := t.TempDir()
	a := filepath.Join(t.TempDir(), "claude-bts")
	b := filepath.Join(t.TempDir(), "claude-bts")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	ka, kb := ProjectKey(a), ProjectKey(b)
	if ka == kb || !strings.HasPrefix(ka, "claude-bts-") || ProjectKey(a) != ka {
		t.Fatalf("keys %q %q", ka, kb)
	}
	if k, err := RegisterProject(home, a); err != nil || k != ka {
		t.Fatalf("register %q, %v", k, err)
	}
	if _, err := RegisterProject(home, b); err != nil {
		t.Fatal(err)
	}
	if ProjectRoot(home, ka) != a || ProjectRoot(home, "nope") != "" {
		t.Error("root lookup")
	}
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}
	if ProjectRoot(home, kb) != "" {
		t.Error("a removed worktree still resolves")
	}
	gone, err := PruneProjects(home)
	if err != nil || len(gone) != 1 || gone[0] != kb {
		t.Errorf("pruned %v, %v", gone, err)
	}
}

func TestTokenAndLinks(t *testing.T) {
	home := t.TempDir()
	t1, err := Token(home)
	if err != nil || len(t1) != 32 {
		t.Fatalf("token %q, %v", t1, err)
	}
	if t2, _ := Token(home); t2 != t1 {
		t.Error("the token changed between reads")
	}
	if fi, _ := os.Stat(tokenPath(home)); fi.Mode().Perm() != 0600 {
		t.Errorf("token file mode %v", fi.Mode().Perm())
	}
	t3, _ := RotateToken(home)
	if t3 == t1 {
		t.Error("rotate kept the token")
	}
	l := LinksFor(Config{}, "tok", "/p/x/")
	if l.Local != "http://127.0.0.1:7337/p/x/?t=tok" || l.Record != "http://127.0.0.1:7337/p/x/" || l.Public != "" {
		t.Errorf("local links %+v", l)
	}
	l = LinksFor(Config{Port: 9000, PublicURL: "https://mac.tail.ts.net:9000/"}, "tok", "/")
	if l.Public != "https://mac.tail.ts.net:9000/?t=tok" || l.Local != "http://127.0.0.1:9000/?t=tok" {
		t.Errorf("tailnet links %+v", l)
	}
	if h := (Config{PublicURL: "https://mac.tail.ts.net:9000"}).PublicHost(); h != "mac.tail.ts.net:9000" {
		t.Errorf("public host %q", h)
	}
}
