package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// A local review page (review.surface: local) is served by the user's page
// server (internal/pages), which checks the host, the token and the origin
// and hands each subject's requests here: the subject's page.html with the
// local transport injected into its config, the review kit, other files
// beside the page, and a small JSON API over the page data in DataPath.

// SubjectOptions configure ServeSubject.
type SubjectOptions struct {
	// Base is the subject's URL path, ending in "/" (/r/<project>/<subject>/).
	Base string
	// Kit holds the review kit's files by base name.
	Kit fs.FS
	// Poll is how often an open page's data is checked for changes.
	Poll time.Duration
	// Touch, when set, is called while an event stream is open, so the
	// server does not count an open page as idle.
	Touch func()
	// Stream, when set, is called when an event stream opens; the function
	// it returns, when it closes.
	Stream func() func()
}

// ServeSubject serves rest, the request path below o.Base, for sub.
func ServeSubject(w http.ResponseWriter, r *http.Request, sub Subject, rest string, o SubjectOptions) {
	if o.Poll <= 0 {
		o.Poll = time.Second
	}
	if a, ok := strings.CutPrefix(rest, "api/"); ok {
		serveAPI(w, r, sub, a, o)
		return
	}
	servePage(w, r, sub, rest, o)
}

func jsonOut(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	jsonOut(w, code, map[string]string{"error": msg})
}

var configRe = regexp.MustCompile(`(?s)(<script[^>]*id="bts-review-config"[^>]*>)(.*?)(</script>)`)

// SetPageConfig sets fields in the page's bts-review-config block; a page
// without one comes back unchanged.
func SetPageConfig(page []byte, set map[string]interface{}) []byte {
	m := configRe.FindSubmatchIndex(page)
	if m == nil {
		return page
	}
	cfg := map[string]interface{}{}
	_ = json.Unmarshal(page[m[4]:m[5]], &cfg)
	for k, v := range set {
		cfg[k] = v
	}
	out, err := json.Marshal(cfg)
	if err != nil {
		return page
	}
	// The config sits in a script element: keep "</" from closing it.
	safe := strings.ReplaceAll(string(out), "</", `<\/`)
	var b strings.Builder
	b.Write(page[:m[4]])
	b.WriteString(safe)
	b.Write(page[m[5]:])
	return []byte(b.String())
}

// injectConfig puts the local transport into the page's config block, so
// the kit talks to the page server instead of claude.ai.
func injectConfig(page []byte, api string) []byte {
	return SetPageConfig(page, map[string]interface{}{"transport": "local", "api": api})
}

func servePage(w http.ResponseWriter, r *http.Request, sub Subject, rest string, o SubjectOptions) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		fail(w, http.StatusMethodNotAllowed, "pages are read-only")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	switch rest {
	case "", "index.html", "page.html":
		page, err := os.ReadFile(filepath.Join(sub.Work, "page.html"))
		if err != nil {
			fail(w, http.StatusNotFound, "no page yet: write "+filepath.Join(sub.Work, "page.html")+" (bts review page prints what it shows)")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(injectConfig(page, o.Base+"api/"))
	case "bts-review-kit.js", "bts-review-kit.css":
		if o.Kit == nil {
			http.NotFound(w, r)
			return
		}
		data, err := fs.ReadFile(o.Kit, rest)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		ctype := "text/css; charset=utf-8"
		if strings.HasSuffix(rest, ".js") {
			ctype = "text/javascript; charset=utf-8"
		}
		w.Header().Set("Content-Type", ctype)
		_, _ = w.Write(data)
	default:
		// Other files the session wrote beside the page (an image), one
		// level deep, never the page data or a dot file.
		name := path.Clean(rest)
		if strings.Contains(name, "/") || strings.HasPrefix(name, ".") || name == DataDir {
			http.NotFound(w, r)
			return
		}
		full := filepath.Join(sub.Work, name)
		if fi, err := os.Stat(full); err != nil || !fi.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, full)
	}
}

const maxBody = 1 << 20

func serveAPI(w http.ResponseWriter, r *http.Request, sub Subject, rest string, o SubjectOptions) {
	switch {
	case rest == "state" && r.Method == http.MethodGet:
		st, err := pageState(sub)
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonOut(w, http.StatusOK, st)
	case rest == "events" && r.Method == http.MethodGet:
		events(w, r, sub, o)
	case strings.HasPrefix(rest, "doc/"):
		parts := strings.Split(strings.TrimPrefix(rest, "doc/"), "/")
		if len(parts) != 2 || !validCollection(parts[0]) || !ValidDocID(parts[1]) {
			fail(w, http.StatusBadRequest, "doc/<notes|rounds|proposals>/<id>")
			return
		}
		doc(w, r, sub, parts[0], parts[1])
	default:
		http.NotFound(w, r)
	}
}

func doc(w http.ResponseWriter, r *http.Request, sub Subject, collection, id string) {
	var err error
	switch r.Method {
	case http.MethodDelete:
		err = DeleteDoc(sub, collection, id)
	case http.MethodPut, http.MethodPatch:
		body, rerr := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
		if rerr != nil {
			fail(w, http.StatusBadRequest, rerr.Error())
			return
		}
		if len(body) > maxBody {
			fail(w, http.StatusRequestEntityTooLarge, "a page document is at most 1 MiB")
			return
		}
		var d map[string]interface{}
		if jerr := json.Unmarshal(body, &d); jerr != nil || d == nil {
			fail(w, http.StatusBadRequest, "the body is one JSON object")
			return
		}
		if r.Method == http.MethodPut {
			err = PutDoc(sub, collection, id, d)
		} else {
			err = MergeDoc(sub, collection, id, d)
		}
	default:
		fail(w, http.StatusMethodNotAllowed, "PUT, PATCH or DELETE")
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOut(w, http.StatusOK, map[string]bool{"ok": true})
}

// pageState is everything the page shows from its data, plus whether a
// session is waiting for the next round and the page's own stamp.
func pageState(sub Subject) (map[string]interface{}, error) {
	data, err := PageData(sub)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{"waiting": SessionWaiting(sub), "page": PageStamp(sub)}
	for c, docs := range data {
		out[c] = docs
	}
	return out, nil
}

// events streams the page's data as it changes: an initial `state`, then
// a `state` whenever a document changes or a session starts or stops
// waiting, and `reload` when page.html itself changed.
func events(w http.ResponseWriter, r *http.Request, sub Subject, o SubjectOptions) {
	fl, ok := w.(http.Flusher)
	if !ok {
		fail(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	if o.Stream != nil {
		defer o.Stream()()
	}
	var mu sync.Mutex
	send := func(event string, v interface{}) bool {
		mu.Lock()
		defer mu.Unlock()
		data, err := json.Marshal(v)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
			return false
		}
		fl.Flush()
		return true
	}
	lastData, lastPage, lastWait := "", PageStamp(sub), SessionWaiting(sub)
	push := func() bool {
		st, err := pageState(sub)
		if err != nil {
			return true
		}
		lastData = DataStamp(sub)
		return send("state", st)
	}
	if !push() {
		return
	}
	tick := time.NewTicker(o.Poll)
	defer tick.Stop()
	beat := time.NewTicker(15 * time.Second)
	defer beat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-beat.C:
			mu.Lock()
			_, err := fmt.Fprint(w, ": keep-alive\n\n")
			if err == nil {
				fl.Flush()
			}
			mu.Unlock()
			if err != nil {
				return
			}
		case <-tick.C:
			if o.Touch != nil {
				o.Touch()
			}
			if p := PageStamp(sub); p != lastPage {
				lastPage = p
				if !send("reload", map[string]string{"page": p}) {
					return
				}
			}
			wait := SessionWaiting(sub)
			if DataStamp(sub) != lastData || wait != lastWait {
				lastWait = wait
				if !push() {
					return
				}
			}
		}
	}
}

// SubjectKey is how a URL names a subject within its project: the recipe
// id, or "feature:<id>".
func SubjectKey(sub Subject) string {
	if sub.Kind == KindFeature {
		return "feature:" + sub.ID
	}
	return sub.ID
}

// Before v0.38.0 each project ran its own review server and recorded it in
// .bts/local/review-server.json.  The user's page server replaces them;
// StopLegacyServer stops one that is still running.

type legacyServer struct {
	PID   int    `json:"pid"`
	Port  int    `json:"port"`
	Token string `json:"token"`
}

// answers says whether the recorded server still answers with its token,
// so a pid the system reused for another process is never signalled.
func (l legacyServer) answers() bool {
	if l.Port == 0 || l.Token == "" {
		return false
	}
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz?t=%s", l.Port, l.Token))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func legacyServerPath(root string) string {
	return filepath.Join(state.LocalPath(root), "review-server.json")
}

// StopLegacyServer stops the project's per-project review server, if one
// is recorded, and removes its record.  It reports whether one was running.
func StopLegacyServer(root string) (bool, error) {
	var info legacyServer
	if err := state.ReadJSON(legacyServerPath(root), &info); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		_ = os.Remove(legacyServerPath(root))
		return false, nil
	}
	running := info.PID > 0 && syscall.Kill(info.PID, 0) == nil && info.answers()
	if running {
		if err := syscall.Kill(info.PID, syscall.SIGTERM); err != nil {
			return false, err
		}
		for i := 0; i < 50 && syscall.Kill(info.PID, 0) == nil; i++ {
			time.Sleep(100 * time.Millisecond)
		}
	}
	_ = os.Remove(legacyServerPath(root))
	return running, nil
}
