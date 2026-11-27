package review

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// localPage is the fixture with a local page attached for the intent
// point, its page.html written.
func localPage(t *testing.T) (string, Subject) {
	t.Helper()
	root := fixture(t)
	sub := RecipeSubject(root, rid)
	if _, err := PageBrief(sub, "intent"); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(sub.Work, "page.html"),
		`<html><head><script type="application/json" id="bts-review-config">{"recipe":"`+rid+`","kit":"3"}</script></head><body data-note="x">page</body></html>`)
	if _, err := AttachLocal(sub, "http://127.0.0.1:1/r/p-000000/"+rid+"/", "intent"); err != nil {
		t.Fatal(err)
	}
	return root, sub
}

func TestPageData_PutMergeDeleteAndIDs(t *testing.T) {
	_, sub := localPage(t)
	if err := PutDoc(sub, "notes", "n-1", map[string]interface{}{"text": "a", "status": "draft"}); err != nil {
		t.Fatal(err)
	}
	if err := MergeDoc(sub, "notes", "n-1", map[string]interface{}{"status": "submitted"}); err != nil {
		t.Fatal(err)
	}
	d, err := ReadDoc(sub, "notes", "n-1")
	if err != nil || d["text"] != "a" || d["status"] != "submitted" {
		t.Fatalf("merge kept %v, %v", d, err)
	}
	if err := DeleteDoc(sub, "notes", "n-1"); err != nil {
		t.Fatal(err)
	}
	if d, _ := ReadDoc(sub, "notes", "n-1"); d != nil {
		t.Fatalf("deleted note still reads %v", d)
	}
	for _, bad := range []string{"../x", ".hidden", "a/b", "", strings.Repeat("a", 200)} {
		if err := PutDoc(sub, "notes", bad, map[string]interface{}{}); err == nil {
			t.Errorf("id %q was accepted", bad)
		}
	}
	if err := PutDoc(sub, "secrets", "x", map[string]interface{}{}); err == nil {
		t.Error("an unknown collection was accepted")
	}
}

func TestLocalRound_ImportReadsDataAndMarksPicked(t *testing.T) {
	_, sub := localPage(t)
	_ = PutDoc(sub, "notes", "n-1", map[string]interface{}{"label": "Purpose", "text": "Why?", "kind": "note", "status": "submitted", "round": 1})
	_ = PutDoc(sub, "rounds", "r-1", map[string]interface{}{"round": 1, "ids": []string{"n-1"}, "status": "submitted", "kind": "review"})
	if got, _ := SubmittedRounds(sub); len(got) != 1 || got[0] != 1 {
		t.Fatalf("submitted rounds %v", got)
	}
	rounds, err := Import(sub, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rounds) != 1 || len(rounds[0].Items) != 1 || rounds[0].Items[0].Text != "Why?" {
		t.Fatalf("imported %+v", rounds)
	}
	r, _ := ReadDoc(sub, "rounds", "r-1")
	if r["status"] != "picked" || r["picked"] == nil {
		t.Errorf("the page's round is %v, want picked", r)
	}
	if got, _ := SubmittedRounds(sub); len(got) != 0 {
		t.Errorf("a picked round still reads as waiting: %v", got)
	}
	// The session's answer lands on the page, not in an upload file.
	if err := Dispose(sub, 1, "n-1", Answered, "explained"); err != nil {
		t.Fatal(err)
	}
	if _, err := ReplyNote(sub, "n-1", Answered, "Because the page is local."); err != nil {
		t.Fatal(err)
	}
	if _, err := DoneWith(sub, 1, "One question answered."); err != nil {
		t.Fatal(err)
	}
	n, _ := ReadDoc(sub, "notes", "n-1")
	if n["status"] != Answered || n["reply"] != "Because the page is local." {
		t.Errorf("note on the page: %v", n)
	}
	r, _ = ReadDoc(sub, "rounds", "r-1")
	if r["status"] != "processed" || r["summary"] != "One question answered." {
		t.Errorf("round on the page: %v", r)
	}
	if _, err := os.Stat(UploadDir(sub)); err == nil {
		t.Error("a local page wrote upload files")
	}
}

func TestLocalProposal_GoesStraightToThePage(t *testing.T) {
	_, sub := localPage(t)
	_ = PutDoc(sub, "notes", "n-1", map[string]interface{}{"label": "Purpose", "text": "Say more.", "kind": "note", "status": "submitted", "round": 1})
	_ = PutDoc(sub, "rounds", "r-1", map[string]interface{}{"round": 1, "ids": []string{"n-1"}, "status": "submitted"})
	if _, err := Import(sub, "", 0); err != nil {
		t.Fatal(err)
	}
	p, _, err := Propose(sub, 1, Proposal{Note: "n-1", Target: "intent.md", Summary: "Purpose says more"})
	if err != nil {
		t.Fatal(err)
	}
	d, _ := ReadDoc(sub, "proposals", p.ID)
	if d["status"] != PStatusProposed || d["target"] != "intent.md" {
		t.Errorf("proposal on the page: %v", d)
	}
}

func TestArtifactPage_StillWritesUploadFiles(t *testing.T) {
	root := fixture(t)
	sub := RecipeSubject(root, rid)
	if _, err := PageBrief(sub, "intent"); err != nil {
		t.Fatal(err)
	}
	if _, err := Attach(sub, "https://claude.ai/artifact/x", "intent"); err != nil {
		t.Fatal(err)
	}
	path, err := ReplyNote(sub, "n-1", Answered, "ok")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, UploadDir(sub)) {
		t.Errorf("an artifact page's reply went to %s, not the upload directory", path)
	}
	if IsLocal(sub) {
		t.Error("a claude.ai page reads as local")
	}
}

func TestInjectConfig_AddsTheLocalTransport(t *testing.T) {
	page := []byte(`<script type="application/json" id="bts-review-config">{"recipe":"r-1","note":"</b>"}</script>`)
	out := string(injectConfig(page, "/r/p-1/r-1/api/"))
	if !strings.Contains(out, `"transport":"local"`) || !strings.Contains(out, `"api":"/r/p-1/r-1/api/"`) {
		t.Errorf("config not injected: %s", out)
	}
	if strings.Contains(out, `"</b>"`) {
		t.Errorf("a </ in the config can close the script element: %s", out)
	}
	if got := string(SetPageConfig([]byte("<p>no config</p>"), map[string]interface{}{"transport": "static"})); got != "<p>no config</p>" {
		t.Errorf("a page without a config changed: %s", got)
	}
}

const subjectBase = "/r/p-1/" + rid + "/"

// subjectServer serves one subject under subjectBase, as the user's page
// server hands it over once the host, token and origin passed.
func subjectServer(t *testing.T, sub Subject) *httptest.Server {
	t.Helper()
	opts := SubjectOptions{
		Base: subjectBase, Poll: 50 * time.Millisecond,
		Kit: fstest.MapFS{"bts-review-kit.js": {Data: []byte("/* kit */")}, "bts-review-kit.css": {Data: []byte("/* css */")}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest, ok := strings.CutPrefix(r.URL.Path, subjectBase)
		if !ok {
			http.NotFound(w, r)
			return
		}
		ServeSubject(w, r, sub, rest, opts)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func do(t *testing.T, method, rawURL string, body string, hdr map[string]string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, rawURL, strings.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestServeSubject_PageKitAndDocumentRoundTrip(t *testing.T) {
	_, sub := localPage(t)
	srv := subjectServer(t, sub)
	base := srv.URL + subjectBase
	code, body := do(t, "GET", base, "", nil)
	if code != http.StatusOK || !strings.Contains(body, `"transport":"local"`) || !strings.Contains(body, `"api":"`+subjectBase+`api/"`) {
		t.Fatalf("page %d: %s", code, body)
	}
	if code, body := do(t, "GET", base+"bts-review-kit.js", "", nil); code != http.StatusOK || body != "/* kit */" {
		t.Errorf("kit %d: %s", code, body)
	}
	if code, _ := do(t, "GET", base+"data", "", nil); code != http.StatusNotFound {
		t.Errorf("the page data directory is served: %d", code)
	}
	if code, _ := do(t, "POST", base, "", nil); code != http.StatusMethodNotAllowed {
		t.Errorf("a write to the page: %d", code)
	}
	ctype := map[string]string{"Content-Type": "application/json"}
	doc := base + "api/doc/notes/n-1"
	if code, body := do(t, "PUT", doc, `{"text":"hello","status":"draft"}`, ctype); code != http.StatusOK {
		t.Fatalf("put %d: %s", code, body)
	}
	if code, _ := do(t, "PATCH", doc, `{"status":"submitted"}`, ctype); code != http.StatusOK {
		t.Fatalf("patch %d", code)
	}
	_, st := do(t, "GET", base+"api/state", "", nil)
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(st), &got); err != nil {
		t.Fatal(err)
	}
	notes, _ := got["notes"].(map[string]interface{})
	n, _ := notes["n-1"].(map[string]interface{})
	if n["text"] != "hello" || n["status"] != "submitted" {
		t.Errorf("state %s", st)
	}
	if code, _ := do(t, "PUT", doc, strings.Repeat("x", maxBody+10), ctype); code != http.StatusRequestEntityTooLarge && code != http.StatusBadRequest {
		t.Errorf("an oversized body: %d", code)
	}
	if code, _ := do(t, "PUT", base+"api/doc/secrets/x", `{}`, ctype); code != http.StatusBadRequest {
		t.Errorf("an unknown collection: %d", code)
	}
}

func TestServeSubject_EventStreamPushesACommandsWrite(t *testing.T) {
	_, sub := localPage(t)
	srv := subjectServer(t, sub)
	resp, err := http.Get(srv.URL + subjectBase + "api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	events := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		ev := ""
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "event: ") {
				ev = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				events <- ev + " " + strings.TrimPrefix(line, "data: ")
			}
		}
		close(events)
	}()
	next := func() string {
		select {
		case e := <-events:
			return e
		case <-time.After(5 * time.Second):
			t.Fatal("no event")
			return ""
		}
	}
	if first := next(); !strings.HasPrefix(first, "state ") {
		t.Fatalf("first event %q", first)
	}
	// A session's command writes; the open page hears it.
	if _, err := ReplyNote(sub, "n-9", Answered, "pushed"); err != nil {
		t.Fatal(err)
	}
	for {
		e := next()
		if strings.HasPrefix(e, "state ") && strings.Contains(e, `"pushed"`) {
			break
		}
	}
	// Rewriting page.html tells the page to reload.
	write(t, filepath.Join(sub.Work, "page.html"), "<html>v2</html>")
	for {
		if e := next(); strings.HasPrefix(e, "reload ") {
			break
		}
	}
}

// A legacy per-project server record whose pid does not answer with its
// token is never signalled (the system may have reused the pid), and the
// record goes.
func TestStopLegacyServer_SignalsOnlyAServerThatAnswers(t *testing.T) {
	root := fixture(t)
	rec := filepath.Join(state.LocalPath(root), "review-server.json")
	write(t, rec, fmt.Sprintf(`{"pid": %d, "port": 1, "token": "x"}`, os.Getpid()))
	running, err := StopLegacyServer(root)
	if err != nil || running {
		t.Fatalf("stopped %v, %v: this test process was taken for the old server", running, err)
	}
	if _, err := os.Stat(rec); err == nil {
		t.Error("the legacy record is still there")
	}
	if running, err := StopLegacyServer(root); err != nil || running {
		t.Errorf("no record: %v, %v", running, err)
	}
}

func TestWaiting_IsSeenForAMinute(t *testing.T) {
	_, sub := localPage(t)
	if SessionWaiting(sub) {
		t.Fatal("waiting before anyone waits")
	}
	if err := TouchWaiting(sub); err != nil {
		t.Fatal(err)
	}
	if !SessionWaiting(sub) {
		t.Error("a fresh wait is not seen")
	}
	ClearWaiting(sub)
	if SessionWaiting(sub) {
		t.Error("a cleared wait is still seen")
	}
}
