package review

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
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
	if _, err := AttachLocal(sub, "http://127.0.0.1:1/p/"+rid+"/?t=x", "intent"); err != nil {
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
	out := string(injectConfig(page, "r-1"))
	if !strings.Contains(out, `"transport":"local"`) || !strings.Contains(out, `"api":"/api/r-1/"`) {
		t.Errorf("config not injected: %s", out)
	}
	if strings.Contains(out, `"</b>"`) {
		t.Errorf("a </ in the config can close the script element: %s", out)
	}
}

// startServer runs Serve on a free port for the test.
func startServer(t *testing.T, root string, sub Subject) ServerInfo {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan ServerInfo, 1)
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, ServeOptions{
			Root: root, Version: "test", Poll: 50 * time.Millisecond,
			Kit: fstest.MapFS{"bts-review-kit.js": {Data: []byte("/* kit */")}, "bts-review-kit.css": {Data: []byte("/* css */")}},
			Resolve: func(key string) (Subject, error) {
				if key != SubjectKey(sub) {
					return Subject{}, fmt.Errorf("no review %q", key)
				}
				return sub, nil
			},
			Ready: func(info ServerInfo) { ready <- info },
		})
	}()
	var info ServerInfo
	select {
	case info = <-ready:
	case err := <-done:
		t.Fatalf("server exited: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not start")
	}
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return info
}

func do(t *testing.T, method, rawURL string, body string, hdr map[string]string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, rawURL, strings.NewReader(body))
	for k, v := range hdr {
		if strings.EqualFold(k, "Host") {
			req.Host = v
			continue
		}
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

func TestServer_RefusesWithoutTokenHostOrOrigin(t *testing.T) {
	root, sub := localPage(t)
	info := startServer(t, root, sub)
	base := fmt.Sprintf("http://127.0.0.1:%d", info.Port)
	page := base + "/p/" + url.PathEscape(rid) + "/"
	if code, _ := do(t, "GET", page, "", nil); code != http.StatusForbidden {
		t.Errorf("no token: %d", code)
	}
	if code, _ := do(t, "GET", page+"?t=wrong", "", nil); code != http.StatusForbidden {
		t.Errorf("wrong token: %d", code)
	}
	if code, _ := do(t, "GET", page+"?t="+info.Token, "", map[string]string{"Host": "evil.example:80"}); code != http.StatusForbidden {
		t.Errorf("rebound host: %d", code)
	}
	doc := base + "/api/" + url.PathEscape(rid) + "/doc/notes/n-1?t=" + info.Token
	if code, _ := do(t, "PUT", doc, `{"text":"x"}`, map[string]string{"Origin": "http://evil.example"}); code != http.StatusForbidden {
		t.Errorf("cross-origin write: %d", code)
	}
	if d, _ := ReadDoc(sub, "notes", "n-1"); d != nil {
		t.Error("a refused write reached the page data")
	}
	if code, _ := do(t, "GET", base+"/p/r-999/?t="+info.Token, "", nil); code != http.StatusNotFound {
		t.Errorf("unknown subject: %d", code)
	}
}

func TestServer_PageKitAndDocumentRoundTrip(t *testing.T) {
	root, sub := localPage(t)
	info := startServer(t, root, sub)
	base := fmt.Sprintf("http://127.0.0.1:%d", info.Port)
	key := url.PathEscape(rid)
	code, body := do(t, "GET", base+"/p/"+key+"/?t="+info.Token, "", nil)
	if code != http.StatusOK || !strings.Contains(body, `"transport":"local"`) {
		t.Fatalf("page %d: %s", code, body)
	}
	if code, body := do(t, "GET", base+"/p/"+key+"/bts-review-kit.js?t="+info.Token, "", nil); code != http.StatusOK || body != "/* kit */" {
		t.Errorf("kit %d: %s", code, body)
	}
	if code, _ := do(t, "GET", base+"/p/"+key+"/data?t="+info.Token, "", nil); code != http.StatusNotFound {
		t.Errorf("the page data directory is served: %d", code)
	}
	origin := map[string]string{"Origin": base, "Content-Type": "application/json"}
	doc := base + "/api/" + key + "/doc/notes/n-1?t=" + info.Token
	if code, body := do(t, "PUT", doc, `{"text":"hello","status":"draft"}`, origin); code != http.StatusOK {
		t.Fatalf("put %d: %s", code, body)
	}
	if code, _ := do(t, "PATCH", doc, `{"status":"submitted"}`, origin); code != http.StatusOK {
		t.Fatalf("patch %d", code)
	}
	_, st := do(t, "GET", base+"/api/"+key+"/state?t="+info.Token, "", nil)
	var state map[string]interface{}
	if err := json.Unmarshal([]byte(st), &state); err != nil {
		t.Fatal(err)
	}
	notes, _ := state["notes"].(map[string]interface{})
	n, _ := notes["n-1"].(map[string]interface{})
	if n["text"] != "hello" || n["status"] != "submitted" {
		t.Errorf("state %s", st)
	}
	if code, _ := do(t, "PUT", doc, strings.Repeat("x", maxBody+10), origin); code != http.StatusRequestEntityTooLarge && code != http.StatusBadRequest {
		t.Errorf("an oversized body: %d", code)
	}
}

func TestServer_EventStreamPushesACommandsWrite(t *testing.T) {
	root, sub := localPage(t)
	info := startServer(t, root, sub)
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/%s/events?t=%s", info.Port, url.PathEscape(rid), info.Token))
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

func TestServer_IdleExitRemovesItsRecord(t *testing.T) {
	root, sub := localPage(t)
	info := startServer(t, root, sub)
	got, err := ReadServerInfo(root)
	if err != nil || got == nil || got.Port != info.Port || got.Token != info.Token {
		t.Fatalf("record %+v, %v", got, err)
	}
	if !got.Healthy() {
		t.Error("a running server reads as unhealthy")
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
