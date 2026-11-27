package pages

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/review"
)

// The server binds 127.0.0.1 only.  Every request must come to one of its
// own host names (127.0.0.1 or localhost on its port, or the tailnet name
// `tailscale serve` forwards, which keeps DNS rebinding out), carry the
// token (query `t`, the cookie a page load sets, or the X-BTS-Token
// header), and, for a write that says where it comes from, come from the
// page's own origin.

// ServerInfo is server.json: the running server.
type ServerInfo struct {
	PID     int    `json:"pid"`
	Port    int    `json:"port"`
	Version string `json:"version"`
	// Exe is the binary the server runs, so only that binary replaces it
	// with another version.
	Exe string `json:"exe,omitempty"`
	// Managed is "launchd" when a LaunchAgent keeps the server running.
	Managed string `json:"managed,omitempty"`
	Started string `json:"started"`
}

func serverInfoPath(home string) string { return filepath.Join(home, "server.json") }

// ReadServerInfo reads server.json; nil when there is none.
func ReadServerInfo(home string) (*ServerInfo, error) {
	var info ServerInfo
	if err := readJSON(serverInfoPath(home), &info); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return &info, nil
}

// Healthy says whether the recorded server is up and answers with token.
func (info *ServerInfo) Healthy(token string) bool {
	if info == nil || info.Port == 0 || !pidAlive(info.PID) {
		return false
	}
	return answers(info.Port, token)
}

func answers(port int, token string) bool {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/healthz", port), nil)
	if err != nil {
		return false
	}
	req.Header.Set(tokenHeader, token)
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func pidAlive(pid int) bool {
	return pid > 0 && syscall.Kill(pid, 0) == nil
}

// ServeOptions configure Serve.
type ServeOptions struct {
	Home    string
	Version string
	Exe     string
	Managed string
	// Kit holds the review kit's files by base name.
	Kit fs.FS
	// ResolveReview turns a project root and a subject key (a recipe id,
	// or "feature:<id>") into its review.
	ResolveReview func(root, key string) (review.Subject, error)
	// Idle is how long the server runs with no request and no open page
	// before it exits; 0 means never.
	Idle time.Duration
	// Addr is the listen address; "" is 127.0.0.1 on the configured port.
	Addr string
	// Ready, when set, is called with the record once the server listens.
	Ready func(ServerInfo)
	// Poll is how often an open review page's data is checked.
	Poll time.Duration
}

type server struct {
	opts   ServeOptions
	port   int
	active atomic.Int64 // unix nanoseconds of the last request
	open   atomic.Int64 // open event streams
}

// Serve runs the page server until ctx ends or it has been idle for
// opts.Idle.  It records itself in server.json and removes the record on
// exit while the record is still its own.
func Serve(ctx context.Context, opts ServeOptions) error {
	if opts.Home == "" {
		return errors.New("serve: no pages home")
	}
	if opts.Poll <= 0 {
		opts.Poll = time.Second
	}
	cfg, err := LoadConfig(opts.Home)
	if err != nil {
		return err
	}
	if _, err := Token(opts.Home); err != nil {
		return err
	}
	addr := opts.Addr
	if addr == "" {
		addr = fmt.Sprintf("127.0.0.1:%d", cfg.ListenPort())
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w (another program holds the port? set \"port\" in %s)", addr, err, configPath(opts.Home))
	}
	s := &server{opts: opts, port: ln.Addr().(*net.TCPAddr).Port}
	s.active.Store(time.Now().UnixNano())
	info := ServerInfo{PID: os.Getpid(), Port: s.port, Version: opts.Version, Exe: opts.Exe, Managed: opts.Managed, Started: time.Now().UTC().Format(time.RFC3339)}
	if err := writeJSON(serverInfoPath(opts.Home), info, 0644); err != nil {
		ln.Close()
		return err
	}
	defer func() {
		if cur, _ := ReadServerInfo(opts.Home); cur != nil && cur.PID == info.PID && cur.Port == info.Port {
			_ = os.Remove(serverInfoPath(opts.Home))
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

const (
	tokenCookie = "bts_pages_t"
	tokenHeader = "X-BTS-Token"
)

// sameToken compares in constant time: the server is reachable from the
// tailnet, not only from this machine.
func sameToken(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// hostKind says how a request's Host is reached: "local" for 127.0.0.1 or
// localhost on the server's port, "public" for the tailnet name, "" for
// anything else.
func (s *server) hostKind(r *http.Request, cfg Config) string {
	p := strconv.Itoa(s.port)
	if r.Host == "127.0.0.1:"+p || r.Host == "localhost:"+p {
		return "local"
	}
	if ph := cfg.PublicHost(); ph != "" {
		if strings.EqualFold(r.Host, ph) {
			return "public"
		}
		if !strings.Contains(ph, ":") && strings.EqualFold(r.Host, ph+":443") {
			return "public"
		}
	}
	return ""
}

// originOK says whether a write's Origin is the page's own: http for the
// loopback names, https for the tailnet name (the proxy ends TLS, so the
// server sees http while the browser is on https).
func originOK(r *http.Request, kind string) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	scheme := "http://"
	if kind == "public" {
		scheme = "https://"
	}
	return strings.EqualFold(o, scheme+r.Host)
}

func (s *server) jsonOut(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *server) fail(w http.ResponseWriter, code int, msg string) {
	s.jsonOut(w, code, map[string]string{"error": msg})
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.active.Store(time.Now().UnixNano())
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	cfg, err := LoadConfig(s.opts.Home)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	kind := s.hostKind(r, cfg)
	if kind == "" {
		s.fail(w, http.StatusForbidden, "wrong host")
		return
	}
	token, err := Token(s.opts.Home)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	viaQuery := false
	switch {
	case r.URL.Query().Get("t") != "":
		viaQuery = sameToken(r.URL.Query().Get("t"), token)
		if !viaQuery {
			s.fail(w, http.StatusForbidden, "wrong token: open a link bts printed since the token last changed")
			return
		}
	case r.Header.Get(tokenHeader) != "":
		if !sameToken(r.Header.Get(tokenHeader), token) {
			s.fail(w, http.StatusForbidden, "wrong token")
			return
		}
	default:
		c, err := r.Cookie(tokenCookie)
		if err != nil || !sameToken(c.Value, token) {
			s.fail(w, http.StatusForbidden, "missing or old token: open a link bts printed (bts page open, bts review open)")
			return
		}
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && !originOK(r, kind) {
		s.fail(w, http.StatusForbidden, "cross-origin request")
		return
	}
	if viaQuery {
		// The token moves into a cookie, so the page's own loads and API
		// calls carry it.  Lax, not Strict: a link opened from a chat app
		// is a cross-site navigation, and the redirect below keeps it one.
		http.SetCookie(w, &http.Cookie{Name: tokenCookie, Value: token, Path: "/", HttpOnly: true,
			SameSite: http.SameSiteLaxMode, Secure: kind == "public", MaxAge: 365 * 24 * 3600})
		// A page load drops the token from the address bar, so it stays out
		// of history, bookmarks and screenshots.
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/") {
			q := r.URL.Query()
			q.Del("t")
			u := *r.URL
			u.RawQuery = q.Encode()
			w.Header().Set("Cache-Control", "no-store")
			http.Redirect(w, r, u.RequestURI(), http.StatusSeeOther)
			return
		}
	}
	switch p := r.URL.Path; {
	case p == "/healthz":
		s.jsonOut(w, http.StatusOK, map[string]interface{}{"ok": true, "version": s.opts.Version})
	case p == "/" || p == "/index.html":
		s.index(w, r, cfg)
	case strings.HasPrefix(p, "/p/"):
		s.page(w, r, strings.TrimPrefix(p, "/p/"))
	case strings.HasPrefix(p, "/r/"):
		s.reviewPage(w, r, strings.TrimPrefix(p, "/r/"))
	default:
		http.NotFound(w, r)
	}
}

// page serves /p/<id>/<file> from the current version, and
// /p/<id>/v/<n>/<file> from version n.
func (s *server) page(w http.ResponseWriter, r *http.Request, rest string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		s.fail(w, http.StatusMethodNotAllowed, "pages are read-only")
		return
	}
	id, rest, ok := strings.Cut(rest, "/")
	if !ok {
		http.Redirect(w, r, "/p/"+id+"/", http.StatusFound)
		return
	}
	p, err := Get(s.opts.Home, id)
	if err != nil {
		s.fail(w, http.StatusNotFound, err.Error())
		return
	}
	n := p.Version
	if v, after, ok := strings.Cut(rest, "/"); ok && v == "v" {
		num, tail, slash := strings.Cut(after, "/")
		k, err := strconv.Atoi(num)
		if err != nil || k < 1 || k > p.Version {
			s.fail(w, http.StatusNotFound, fmt.Sprintf("page %s has versions 1 to %d", id, p.Version))
			return
		}
		if !slash {
			http.Redirect(w, r, "/p/"+id+"/v/"+num+"/", http.StatusFound)
			return
		}
		n, rest = k, tail
	}
	name := rest
	if name == "" {
		name = PageFile
	}
	clean := path.Clean("/" + name)[1:]
	if clean != name || strings.HasPrefix(clean, ".") || strings.Contains(clean, "/.") {
		http.NotFound(w, r)
		return
	}
	full := filepath.Join(VersionDir(s.opts.Home, id, n), filepath.FromSlash(clean))
	f, err := os.Open(full)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}

// reviewPage serves /r/<project>/<subject>/... through the review package.
func (s *server) reviewPage(w http.ResponseWriter, r *http.Request, rest string) {
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		http.NotFound(w, r)
		return
	}
	proj := parts[0]
	key, err := url.PathUnescape(parts[1])
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if len(parts) == 2 {
		http.Redirect(w, r, "/r/"+proj+"/"+parts[1]+"/", http.StatusFound)
		return
	}
	root := ProjectRoot(s.opts.Home, proj)
	if root == "" {
		s.fail(w, http.StatusNotFound, "project "+proj+" is not registered here, or its directory is gone")
		return
	}
	if s.opts.ResolveReview == nil {
		http.NotFound(w, r)
		return
	}
	sub, err := s.opts.ResolveReview(root, key)
	if err != nil {
		s.fail(w, http.StatusNotFound, err.Error())
		return
	}
	review.ServeSubject(w, r, sub, parts[2], review.SubjectOptions{
		Base:  ReviewPath(proj, sub),
		Kit:   s.opts.Kit,
		Poll:  s.opts.Poll,
		Touch: func() { s.active.Store(time.Now().UnixNano()) },
		Stream: func() func() {
			s.open.Add(1)
			return func() { s.open.Add(-1) }
		},
	})
}

// ReviewPath is the URL path of a subject's review page in project proj.
func ReviewPath(proj string, sub review.Subject) string {
	return "/r/" + proj + "/" + url.PathEscape(review.SubjectKey(sub)) + "/"
}

// PagePath is the URL path of a general page.
func PagePath(id string) string { return "/p/" + id + "/" }
