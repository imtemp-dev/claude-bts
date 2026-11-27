package pages

import (
	"encoding/base64"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestExport_InlinesLocalFilesAndReportsTheRest(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "css", "site.css"), `body{background:url("../img/bg.png")} h1{color:red}`)
	writeFile(t, filepath.Join(dir, "img", "bg.png"), "BG")
	writeFile(t, filepath.Join(dir, "img", "a.png"), "IMG")
	writeFile(t, filepath.Join(dir, "app.js"), `console.log("</script> inside");`)
	writeFile(t, filepath.Join(dir, ".secret"), "nope")
	doc := `<!doctype html><html><head><title>T</title>
<link rel="stylesheet" href="css/site.css" media="screen">
<link rel="stylesheet" href="https://cdn.example/lib.css">
<script src="app.js" integrity="sha-x" crossorigin="anonymous"></script>
<script src="https://cdnjs.cloudflare.com/lib.js"></script>
<script src="bts-review-kit.js"></script>
<style>.x{background:url(img/a.png)}</style>
</head><body>
<img src="img/a.png" alt="a &amp; b"><img src="data:image/png;base64,AA==">
<img src="missing.png"><img src="/root.png"><img src=".secret">
<img src="img/a.png" data-external="1">
<a href="http://127.0.0.1:7337/p/x/">local</a><a href="other.html">other</a><a href="#top">top</a>
<script>fetch("data.json")</script>
<p>script text "<img src=img/a.png>" stays</p>
</body></html>`
	out, rep, err := Export([]byte(doc), ExportOptions{
		Resolve:    DirResolver(dir),
		Drop:       func(src string) bool { return src == "bts-review-kit.js" },
		LocalHosts: []string{"127.0.0.1", "localhost"},
		Footer:     "T · v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	b64 := func(x string) string { return base64.StdEncoding.EncodeToString([]byte(x)) }
	for _, want := range []string{
		`<style media="screen">body{background:url(data:image/png;base64,` + b64("BG") + `)} h1{color:red}</style>`,
		`<link rel="stylesheet" href="https://cdn.example/lib.css">`,
		`<script>console.log("<\/script> inside");</script>`,
		`<script src="https://cdnjs.cloudflare.com/lib.js"></script>`,
		`.x{background:url(data:image/png;base64,` + b64("IMG") + `)}`,
		`<img src="data:image/png;base64,` + b64("IMG") + `" alt="a &amp; b">`,
		`<img src="data:image/png;base64,AA==">`,
		`<img src="missing.png">`,
		`<img src="img/a.png" data-external="1">`,
		`<footer data-bts-export`,
		`T · v1</footer></body>`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("export lacks %q\n%s", want, s)
		}
	}
	if strings.Contains(s, "bts-review-kit.js") {
		t.Error("the dropped script is still there")
	}
	if strings.Contains(s, "nope") {
		t.Error("a dot file was embedded")
	}
	joined := strings.Join(rep.Missing, "|")
	for _, want := range []string{"missing.png", "/root.png", ".secret"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing list %v lacks %s", rep.Missing, want)
		}
	}
	if len(rep.Remote) != 2 {
		t.Errorf("remote %v", rep.Remote)
	}
	warn := strings.Join(rep.Warnings, "|")
	for _, want := range []string{"127.0.0.1:7337", "other.html", "run time"} {
		if !strings.Contains(warn, want) {
			t.Errorf("warnings %v lack %s", rep.Warnings, want)
		}
	}
	if strings.Contains(warn, "#top") {
		t.Errorf("a fragment link was reported: %v", rep.Warnings)
	}
}

func TestExport_EmbedRemote(t *testing.T) {
	fetched := map[string]string{
		"https://cdn.example/css/lib.css":   `.i{background:url(icons.png)}`,
		"https://cdn.example/css/icons.png": "ICON",
		"https://cdn.example/lib.js":        "var lib=1;",
	}
	fetch := func(u string) ([]byte, error) {
		if v, ok := fetched[u]; ok {
			return []byte(v), nil
		}
		return nil, errors.New("HTTP 404")
	}
	doc := `<link rel="stylesheet" href="https://cdn.example/css/lib.css"><script src="//cdn.example/lib.js"></script><img src="https://cdn.example/gone.png">`
	out, rep, err := Export([]byte(doc), ExportOptions{Resolve: DirResolver(t.TempDir()), EmbedRemote: true, Fetch: fetch})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `<style>.i{background:url(data:image/png;base64,`+base64.StdEncoding.EncodeToString([]byte("ICON"))+`)}</style>`) {
		t.Errorf("remote stylesheet: %s", s)
	}
	if !strings.Contains(s, `<script>var lib=1;</script>`) {
		t.Errorf("protocol-relative script: %s", s)
	}
	if len(rep.Missing) != 1 || !strings.Contains(rep.Missing[0], "gone.png") {
		t.Errorf("missing %v", rep.Missing)
	}
}

func TestDirResolver_StaysInsideTheFolder(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "page.html"), "x")
	writeFile(t, filepath.Join(dir, "data", "notes", "n.json"), "{}")
	writeFile(t, filepath.Join(dir, "ok.css"), "ok")
	r := DirResolver(dir)
	if d, err := r("ok.css"); err != nil || string(d) != "ok" {
		t.Errorf("ok.css: %q, %v", d, err)
	}
	for _, bad := range []string{"../x", "/etc/passwd", "data/notes/n.json", ".git/config", "a/../../x"} {
		if _, err := r(bad); err == nil {
			t.Errorf("%s was read", bad)
		}
	}
}
