package pages

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/review"
)

const publicHost = "mac.tail0000.ts.net:7337"

// startServer runs Serve on a free port with a tailnet URL configured.
func startServer(t *testing.T, home string, resolve func(root, key string) (review.Subject, error)) ServerInfo {
	t.Helper()
	if err := SaveConfig(home, Config{PublicURL: "https://" + publicHost}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan ServerInfo, 1)
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, ServeOptions{
			Home: home, Version: "test", Addr: "127.0.0.1:0", Poll: 50 * time.Millisecond,
			Kit:           fstest.MapFS{"bts-review-kit.js": {Data: []byte("/* kit */")}, "bts-review-kit.css": {Data: []byte("/* css */")}},
			ResolveReview: resolve,
			Ready:         func(info ServerInfo) { ready <- info },
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

type client struct {
	t    *testing.T
	base string
	c    *http.Client
}

// newClient is a browser stand-in: it keeps cookies and does not follow
// redirects, so a test sees them.
func newClient(t *testing.T, port int) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: fmt.Sprintf("http://127.0.0.1:%d", port), c: &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (c *client) do(method, path, body string, hdr map[string]string) (*http.Response, string) {
	c.t.Helper()
	req, _ := http.NewRequest(method, c.base+path, strings.NewReader(body))
	for k, v := range hdr {
		if strings.EqualFold(k, "Host") {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := c.c.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func publishPage(t *testing.T, home, title string) *Page {
	t.Helper()
	dir := t.TempDir()
	page := writeFile(t, filepath.Join(dir, "p.html"), "<html><head><title>"+title+"</title></head><body><img src=\"a.png\"></body></html>")
	img := writeFile(t, filepath.Join(dir, "a.png"), "PNG")
	p, _, err := Publish(home, PublishOptions{File: page, Files: map[string]string{"a.png": img}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestServer_TokenMovesIntoACookieAndLeavesTheAddressBar(t *testing.T) {
	home := t.TempDir()
	p := publishPage(t, home, "First")
	info := startServer(t, home, nil)
	token, _ := Token(home)
	c := newClient(t, info.Port)

	if resp, _ := c.do("GET", PagePath(p.ID), "", nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("no token: %d", resp.StatusCode)
	}
	if resp, _ := c.do("GET", PagePath(p.ID)+"?t=wrong", "", nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("wrong token: %d", resp.StatusCode)
	}
	resp, _ := c.do("GET", PagePath(p.ID)+"?t="+token, "", nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != PagePath(p.ID) {
		t.Fatalf("a page load with the token: %d to %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	ck := resp.Cookies()
	if len(ck) != 1 || ck[0].Name != tokenCookie || !ck[0].HttpOnly || ck[0].SameSite != http.SameSiteLaxMode || ck[0].MaxAge <= 0 || ck[0].Secure {
		t.Errorf("cookie %+v", ck)
	}
	resp, body := c.do("GET", PagePath(p.ID), "", nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "<title>First</title>") {
		t.Fatalf("the page with the cookie: %d %s", resp.StatusCode, body)
	}
	if resp, body := c.do("GET", PagePath(p.ID)+"a.png", "", nil); resp.StatusCode != http.StatusOK || body != "PNG" {
		t.Errorf("a supporting file: %d %q", resp.StatusCode, body)
	}
	if resp, _ := c.do("GET", "/healthz", "", map[string]string{tokenHeader: token}); resp.StatusCode != http.StatusOK {
		t.Errorf("health with the header: %d", resp.StatusCode)
	}
	// A rotated token turns the cookie away.
	if _, err := RotateToken(home); err != nil {
		t.Fatal(err)
	}
	if resp, _ := c.do("GET", PagePath(p.ID), "", nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("an old cookie after rotation: %d", resp.StatusCode)
	}
}

// The tailnet name is accepted (it is what `tailscale serve` forwards, port
// included), any other host is refused, and a write's origin must match the
// scheme the host is reached by.
func TestServer_HostsAndOrigins(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	sub := review.RecipeSubject(root, "r-001-x")
	writeFile(t, filepath.Join(sub.Work, "page.html"), `<html><head><title>Review</title><script type="application/json" id="bts-review-config">{"recipe":"r-001-x"}</script></head><body></body></html>`)
	proj, err := RegisterProject(home, root)
	if err != nil {
		t.Fatal(err)
	}
	info := startServer(t, home, func(r, key string) (review.Subject, error) {
		if r != root || key != "r-001-x" {
			return review.Subject{}, fmt.Errorf("no review %q", key)
		}
		return sub, nil
	})
	token, _ := Token(home)
	c := newClient(t, info.Port)
	tok := map[string]string{tokenHeader: token}
	with := func(extra map[string]string) map[string]string {
		m := map[string]string{tokenHeader: token}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	page := ReviewPath(proj, sub)

	for host, want := range map[string]int{
		fmt.Sprintf("127.0.0.1:%d", info.Port): http.StatusOK,
		fmt.Sprintf("localhost:%d", info.Port): http.StatusOK,
		publicHost:                             http.StatusOK,
		"mac.tail0000.ts.net":                  http.StatusForbidden, // the mapping is on :7337, not 443
		"evil.example:80":                      http.StatusForbidden,
		fmt.Sprintf("127.0.0.2:%d", info.Port): http.StatusForbidden,
	} {
		if resp, _ := c.do("GET", page, "", with(map[string]string{"Host": host})); resp.StatusCode != want {
			t.Errorf("host %s: %d, want %d", host, resp.StatusCode, want)
		}
	}
	_, body := c.do("GET", page, "", tok)
	if !strings.Contains(body, `"api":"`+page+`api/"`) || !strings.Contains(body, `"transport":"local"`) {
		t.Fatalf("review page config: %s", body)
	}

	doc := page + "api/doc/notes/n-1"
	local := fmt.Sprintf("http://127.0.0.1:%d", info.Port)
	for _, tc := range []struct {
		host, origin string
		want         int
	}{
		{fmt.Sprintf("127.0.0.1:%d", info.Port), local, http.StatusOK},
		{fmt.Sprintf("127.0.0.1:%d", info.Port), "", http.StatusOK},
		{publicHost, "https://" + publicHost, http.StatusOK},
		{publicHost, "http://" + publicHost, http.StatusForbidden},
		{fmt.Sprintf("127.0.0.1:%d", info.Port), "https://" + publicHost, http.StatusForbidden},
		{fmt.Sprintf("127.0.0.1:%d", info.Port), "http://evil.example", http.StatusForbidden},
	} {
		h := with(map[string]string{"Host": tc.host, "Content-Type": "application/json"})
		if tc.origin != "" {
			h["Origin"] = tc.origin
		}
		if resp, b := c.do("PUT", doc, `{"text":"x"}`, h); resp.StatusCode != tc.want {
			t.Errorf("write to %s from %q: %d, want %d (%s)", tc.host, tc.origin, resp.StatusCode, tc.want, b)
		}
	}
	// The cookie set through the tailnet name is Secure.
	resp, _ := c.do("GET", page+"?t="+token, "", map[string]string{"Host": publicHost})
	if ck := resp.Cookies(); len(ck) != 1 || !ck[0].Secure {
		t.Errorf("tailnet cookie %+v", ck)
	}
	if resp, _ := c.do("GET", "/r/"+proj+"/r-999/", "", tok); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown subject: %d", resp.StatusCode)
	}
	if resp, _ := c.do("GET", "/r/nope-000000/r-001-x/", "", tok); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown project: %d", resp.StatusCode)
	}
}

func TestServer_VersionsAndPaths(t *testing.T) {
	home := t.TempDir()
	p := publishPage(t, home, "One")
	page := writeFile(t, filepath.Join(t.TempDir(), "p.html"), "<title>Two</title>")
	if _, _, err := Publish(home, PublishOptions{File: page, Page: p.ID}); err != nil {
		t.Fatal(err)
	}
	info := startServer(t, home, nil)
	token, _ := Token(home)
	c := newClient(t, info.Port)
	tok := map[string]string{tokenHeader: token}
	if _, body := c.do("GET", PagePath(p.ID), "", tok); !strings.Contains(body, "Two") {
		t.Errorf("current version: %s", body)
	}
	if _, body := c.do("GET", PagePath(p.ID)+"v/1/", "", tok); !strings.Contains(body, "One") {
		t.Errorf("version 1: %s", body)
	}
	if resp, _ := c.do("GET", PagePath(p.ID)+"v/1", "", tok); resp.StatusCode != http.StatusFound {
		t.Errorf("version without a slash: %d", resp.StatusCode)
	}
	for _, bad := range []string{"v/3/", "v/0/", "v/x/", "../../token", ".hidden", "missing.png"} {
		if resp, _ := c.do("GET", PagePath(p.ID)+bad, "", tok); resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, resp.StatusCode)
		}
	}
	if resp, _ := c.do("PUT", PagePath(p.ID), "x", tok); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("a write to a page: %d", resp.StatusCode)
	}
	if resp, _ := c.do("GET", "/p/"+p.ID, "", tok); resp.StatusCode != http.StatusFound {
		t.Errorf("a page without a slash: %d", resp.StatusCode)
	}
}

func TestServer_IndexListsPagesAndReviews(t *testing.T) {
	home := t.TempDir()
	p := publishPage(t, home, "Architecture &amp; flow")
	root := t.TempDir()
	sub := review.RecipeSubject(root, "r-002-y")
	writeFile(t, filepath.Join(sub.Work, "page.html"), "<title>Scope review</title>")
	if err := review.PutDoc(sub, "rounds", "r-1", map[string]interface{}{"round": 1, "status": "submitted"}); err != nil {
		t.Fatal(err)
	}
	proj, _ := RegisterProject(home, root)
	info := startServer(t, home, func(r, key string) (review.Subject, error) { return review.RecipeSubject(r, key), nil })
	token, _ := Token(home)
	c := newClient(t, info.Port)
	resp, body := c.do("GET", "/", "", map[string]string{tokenHeader: token})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("index %d", resp.StatusCode)
	}
	for _, want := range []string{PagePath(p.ID), "Architecture &amp; flow", "/r/" + proj + "/r-002-y/", "Scope review", "a round waits for a session"} {
		if !strings.Contains(body, want) {
			t.Errorf("index lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, token) {
		t.Error("the index carries the token")
	}
}

func TestServer_RecordAndHealth(t *testing.T) {
	home := t.TempDir()
	info := startServer(t, home, nil)
	got, err := ReadServerInfo(home)
	if err != nil || got == nil || got.Port != info.Port || got.Version != "test" {
		t.Fatalf("record %+v, %v", got, err)
	}
	token, _ := Token(home)
	if !got.Healthy(token) {
		t.Error("a running server reads as unhealthy")
	}
	if got.Healthy("wrong") {
		t.Error("a wrong token reads as healthy")
	}
	raw, _ := os.ReadFile(serverInfoPath(home))
	if strings.Contains(string(raw), token) {
		t.Error("server.json holds the token")
	}
}

func TestEnsure_LeavesAServerFromAnotherBinary(t *testing.T) {
	home := t.TempDir()
	info := startServer(t, home, nil)
	rec, _ := ReadServerInfo(home)
	rec.Exe = "/opt/installed/bts"
	if err := writeJSON(serverInfoPath(home), rec, 0644); err != nil {
		t.Fatal(err)
	}
	got, notice, err := Ensure(EnsureOptions{Home: home, Version: "dev", Exe: "/tmp/go-build/bts"})
	if err != nil || got == nil || got.Port != info.Port || !strings.Contains(notice, "leaves it running") {
		t.Errorf("ensure %+v, %q, %v", got, notice, err)
	}
	if got, notice, err := Ensure(EnsureOptions{Home: home, Version: "test", Exe: "/tmp/go-build/bts"}); err != nil || notice != "" || got.Port != info.Port {
		t.Errorf("same version: %+v, %q, %v", got, notice, err)
	}
}
