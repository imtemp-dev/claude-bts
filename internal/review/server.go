package review

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// `bts review serve` serves a project's local review pages
// (review.surface: local): each subject's page.html, the review kit, and
// a small JSON API over the page data in DataPath.  It binds 127.0.0.1
// only, and every request must carry the server's token (query `t`, the
// cookie the page load sets, or the X-BTS-Token header), come to its own
// host name (no DNS rebinding) and, when it says where it comes from, come
// from the page's own origin.

// ServerInfo is what a running server records in .bts/local/review-server.json.
type ServerInfo struct {
	PID     int    `json:"pid"`
	Port    int    `json:"port"`
	Token   string `json:"token"`
	Version string `json:"version"`
	Root    string `json:"root"`
	Started string `json:"started"`
}

// ServerInfoPath is where a project's server records itself.
func ServerInfoPath(root string) string {
	return filepath.Join(state.LocalPath(root), "review-server.json")
}

// ReadServerInfo reads the project's server record; nil when there is none.
func ReadServerInfo(root string) (*ServerInfo, error) {
	var info ServerInfo
	if err := state.ReadJSON(ServerInfoPath(root), &info); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return &info, nil
}

// PageURL is the link to a subject's page on a running server, token
// included: the link the user opens.
func (info *ServerInfo) PageURL(subject string) string {
	return fmt.Sprintf("http://127.0.0.1:%d/p/%s/?t=%s", info.Port, url.PathEscape(subject), info.Token)
}

// Healthy says whether the recorded server is up and answers with its
// token.
func (info *ServerInfo) Healthy() bool {
	if info == nil || info.Port == 0 || !pidAlive(info.PID) {
		return false
	}
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz?t=%s", info.Port, info.Token))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// StopServer stops the project's recorded server and removes its record.
// It reports whether one was running.
func StopServer(root string) (bool, error) {
	info, err := ReadServerInfo(root)
	if err != nil || info == nil {
		return false, err
	}
	running := pidAlive(info.PID)
	if running {
		if err := syscall.Kill(info.PID, syscall.SIGTERM); err != nil {
			return false, err
		}
		for i := 0; i < 50 && pidAlive(info.PID); i++ {
			time.Sleep(100 * time.Millisecond)
		}
	}
	_ = os.Remove(ServerInfoPath(root))
	return running, nil
}

// SubjectKey is how a server URL names a subject: the recipe id, or
// "feature:<id>".
func SubjectKey(sub Subject) string {
	if sub.Kind == KindFeature {
		return "feature:" + sub.ID
	}
	return sub.ID
}

// ServeOptions configure Serve.
type ServeOptions struct {
	Root    string
	Version string
	// Resolve turns a subject key into its review.
	Resolve func(key string) (Subject, error)
	// Kit holds the review kit's files by base name.
	Kit fs.FS
	// Idle is how long the server runs with no request and no open page
	// before it exits; 0 means never.
	Idle time.Duration
	// Addr is the listen address; "" is 127.0.0.1 on a free port.
	Addr string
	// Ready, when set, is called with the record once the server listens.
	Ready func(ServerInfo)
	// Poll is how often an open page's data is checked for changes.
	Poll time.Duration
}

type server struct {
	opts   ServeOptions
	token  string
	port   int
	active atomic.Int64 // unix nanoseconds of the last request
	open   atomic.Int64 // open event streams
}

func newToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Serve runs the server until ctx ends or it has been idle for opts.Idle.
// It records itself in ServerInfoPath and removes the record on exit.
func Serve(ctx context.Context, opts ServeOptions) error {
	if opts.Resolve == nil {
		return errors.New("serve: no subject resolver")
	}
	if opts.Poll <= 0 {
		opts.Poll = time.Second
	}
	addr := opts.Addr
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	token, err := newToken()
	if err != nil {
		ln.Close()
		return err
	}
	s := &server{opts: opts, token: token, port: ln.Addr().(*net.TCPAddr).Port}
	s.active.Store(time.Now().UnixNano())
	info := ServerInfo{PID: os.Getpid(), Port: s.port, Token: token, Version: opts.Version, Root: opts.Root, Started: now()}
	if err := os.MkdirAll(filepath.Dir(ServerInfoPath(opts.Root)), 0755); err != nil {
		ln.Close()
		return err
	}
	if err := writeAtomic(ServerInfoPath(opts.Root), info); err != nil {
		ln.Close()
		return err
	}
	defer func() {
		// Only remove the record if it is still ours: a newer server may
		// have replaced it.
		if cur, _ := ReadServerInfo(opts.Root); cur != nil && cur.PID == info.PID && cur.Port == info.Port {
			_ = os.Remove(ServerInfoPath(opts.Root))
		}
	}()
	if opts.Ready != nil {
		opts.Ready(info)
	}
	srv := &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if opts.Idle > 0 {
		go func() {
			t := time.NewTicker(time.Minute)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if s.open.Load() == 0 && time.Since(time.Unix(0, s.active.Load())) > opts.Idle {
						cancel()
						return
					}
				}
			}
		}()
	}
	go func() {
		<-ctx.Done()
		sh, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		_ = srv.Shutdown(sh)
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

const tokenCookie = "bts_review_t"

func (s *server) authorized(r *http.Request) bool {
	if t := r.URL.Query().Get("t"); t != "" {
		return t == s.token
	}
	if t := r.Header.Get("X-BTS-Token"); t != "" {
		return t == s.token
	}
	if c, err := r.Cookie(tokenCookie); err == nil {
		return c.Value == s.token
	}
	return false
}

func (s *server) hostOK(r *http.Request) bool {
	p := strconv.Itoa(s.port)
	return r.Host == "127.0.0.1:"+p || r.Host == "localhost:"+p
}

func (s *server) originOK(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	return o == "http://"+r.Host
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

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.active.Store(time.Now().UnixNano())
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !s.hostOK(r) {
		fail(w, http.StatusForbidden, "wrong host")
		return
	}
	if !s.authorized(r) {
		fail(w, http.StatusForbidden, "missing or wrong token: open the link `bts review open` printed")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && !s.originOK(r) {
		fail(w, http.StatusForbidden, "cross-origin request")
		return
	}
	switch {
	case r.URL.Path == "/healthz":
		jsonOut(w, http.StatusOK, map[string]interface{}{"ok": true, "version": s.opts.Version})
	case strings.HasPrefix(r.URL.Path, "/p/"):
		s.servePage(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/"):
		s.serveAPI(w, r)
	default:
		http.NotFound(w, r)
	}
}

// splitSubject splits "/<prefix>/<key>/<rest>" into the key and the rest.
func splitSubject(p, prefix string) (string, string, bool) {
	rest := strings.TrimPrefix(p, prefix)
	i := strings.Index(rest, "/")
	if i < 0 {
		return "", "", false
	}
	key, err := url.PathUnescape(rest[:i])
	if err != nil || key == "" {
		return "", "", false
	}
	return key, rest[i+1:], true
}

var configRe = regexp.MustCompile(`(?s)(<script[^>]*id="bts-review-config"[^>]*>)(.*?)(</script>)`)

// injectConfig puts the local transport into the page's config block, so
// the kit talks to this server instead of claude.ai.
func injectConfig(page []byte, key string) []byte {
	m := configRe.FindSubmatchIndex(page)
	if m == nil {
		return page
	}
	cfg := map[string]interface{}{}
	_ = json.Unmarshal(page[m[4]:m[5]], &cfg)
	cfg["transport"] = "local"
	cfg["api"] = "/api/" + url.PathEscape(key) + "/"
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

func (s *server) servePage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		fail(w, http.StatusMethodNotAllowed, "pages are read-only")
		return
	}
	key, rest, ok := splitSubject(r.URL.Path, "/p/")
	if !ok {
		// "/p/<key>" with no trailing slash: relative kit links need it.
		http.Redirect(w, r, r.URL.Path+"/?"+r.URL.RawQuery, http.StatusFound)
		return
	}
	sub, err := s.opts.Resolve(key)
	if err != nil {
		fail(w, http.StatusNotFound, err.Error())
		return
	}
	// Set the token cookie on every authorized page request, so the
	// page's relative loads (the kit) and its API calls carry it.
	http.SetCookie(w, &http.Cookie{Name: tokenCookie, Value: s.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
	w.Header().Set("Cache-Control", "no-store")
	switch rest {
	case "", "index.html", "page.html":
		page, err := os.ReadFile(filepath.Join(sub.Work, "page.html"))
		if err != nil {
			fail(w, http.StatusNotFound, "no page yet: write "+filepath.Join(sub.Work, "page.html")+" (bts review page prints what it shows)")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(injectConfig(page, key))
	case "bts-review-kit.js", "bts-review-kit.css":
		if s.opts.Kit == nil {
			http.NotFound(w, r)
			return
		}
		data, err := fs.ReadFile(s.opts.Kit, rest)
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

func (s *server) serveAPI(w http.ResponseWriter, r *http.Request) {
	key, rest, ok := splitSubject(r.URL.Path, "/api/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	sub, err := s.opts.Resolve(key)
	if err != nil {
		fail(w, http.StatusNotFound, err.Error())
		return
	}
	switch {
	case rest == "state" && r.Method == http.MethodGet:
		st, err := pageState(sub)
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonOut(w, http.StatusOK, st)
	case rest == "events" && r.Method == http.MethodGet:
		s.events(w, r, sub)
	case strings.HasPrefix(rest, "doc/"):
		parts := strings.Split(strings.TrimPrefix(rest, "doc/"), "/")
		if len(parts) != 2 || !validCollection(parts[0]) || !ValidDocID(parts[1]) {
			fail(w, http.StatusBadRequest, "doc/<notes|rounds|proposals>/<id>")
			return
		}
		s.doc(w, r, sub, parts[0], parts[1])
	default:
		http.NotFound(w, r)
	}
}

func (s *server) doc(w http.ResponseWriter, r *http.Request, sub Subject, collection, id string) {
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
		var doc map[string]interface{}
		if jerr := json.Unmarshal(body, &doc); jerr != nil || doc == nil {
			fail(w, http.StatusBadRequest, "the body is one JSON object")
			return
		}
		if r.Method == http.MethodPut {
			err = PutDoc(sub, collection, id, doc)
		} else {
			err = MergeDoc(sub, collection, id, doc)
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
func (s *server) events(w http.ResponseWriter, r *http.Request, sub Subject) {
	fl, ok := w.(http.Flusher)
	if !ok {
		fail(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	s.open.Add(1)
	defer s.open.Add(-1)
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
	tick := time.NewTicker(s.opts.Poll)
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
			s.active.Store(time.Now().UnixNano())
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
