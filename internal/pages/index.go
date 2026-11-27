package pages

import (
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/review"
)

// The index at / is the one bookmark a phone needs: the general pages,
// newest first, then each registered project's review pages with whether a
// round waits for a session.

type indexPage struct {
	Title   string
	Href    string
	Meta    string
	Updated string
}

type indexProject struct {
	Name    string
	Root    string
	Reviews []indexPage
}

type indexData struct {
	Pages    []indexPage
	Projects []indexProject
}

// ReviewEntry is one review page a project holds.
type ReviewEntry struct {
	Key   string // the subject key: a recipe id, or feature:<id>
	Title string
	Note  string // round state, for the index
	Mod   time.Time
}

// ProjectReviews lists the review pages in a project's local state, the
// most recently changed first.
func ProjectReviews(root string, resolve func(root, key string) (review.Subject, error)) []ReviewEntry {
	var out []ReviewEntry
	for _, kind := range []string{"recipes", "features"} {
		matches, _ := filepath.Glob(filepath.Join(root, ".bts", "local", kind, "*", "review", "page.html"))
		for _, m := range matches {
			id := filepath.Base(filepath.Dir(filepath.Dir(m)))
			key := id
			if kind == "features" {
				key = "feature:" + id
			}
			e := ReviewEntry{Key: key, Title: id}
			if fi, err := os.Stat(m); err == nil {
				e.Mod = fi.ModTime()
			}
			if doc, err := os.ReadFile(m); err == nil {
				if t := HTMLTitle(doc); t != "" {
					e.Title = t
				}
			}
			if resolve != nil {
				if sub, err := resolve(root, key); err == nil {
					if rounds, _ := review.SubmittedRounds(sub); len(rounds) > 0 {
						e.Note = "a round waits for a session"
					} else if review.SessionWaiting(sub) {
						e.Note = "a session is waiting"
					}
				}
			}
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Mod.After(out[j].Mod) })
	return out
}

func (s *server) index(w http.ResponseWriter, r *http.Request, cfg Config) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		s.fail(w, http.StatusMethodNotAllowed, "the index is read-only")
		return
	}
	var d indexData
	list, _ := List(s.opts.Home)
	for _, p := range list {
		meta := "v" + strconv.Itoa(p.Version)
		if p.Description != "" {
			meta += " · " + p.Description
		}
		d.Pages = append(d.Pages, indexPage{Title: p.Title, Href: PagePath(p.ID), Meta: meta, Updated: ShortTime(p.Updated)})
	}
	_, _ = PruneProjects(s.opts.Home)
	projects, _ := Projects(s.opts.Home)
	keys := make([]string, 0, len(projects))
	for k := range projects {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		root := projects[k]
		entries := ProjectReviews(root, s.opts.ResolveReview)
		if len(entries) == 0 {
			continue
		}
		ip := indexProject{Name: filepath.Base(root), Root: root}
		for _, e := range entries {
			meta := e.Key
			if e.Note != "" {
				meta += " · " + e.Note
			}
			href := "/r/" + k + "/" + url.PathEscape(e.Key) + "/"
			ip.Reviews = append(ip.Reviews, indexPage{Title: e.Title, Href: href, Meta: meta, Updated: e.Mod.UTC().Format("2006-01-02 15:04") + "Z"})
		}
		d.Projects = append(d.Projects, ip)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = indexTmpl.Execute(w, d)
}

// ShortTime shows a record's timestamp to the minute, in UTC.
func ShortTime(rfc string) string {
	t, err := time.Parse(time.RFC3339, rfc)
	if err != nil {
		return rfc
	}
	return t.UTC().Format("2006-01-02 15:04") + "Z"
}

var indexTmpl = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>bts pages</title>
<style>
:root { --bg: #fbfaf7; --fg: #1f1d1a; --muted: #6b665e; --line: #e4e0d8; --link: #1d4ed8; --card: #ffffff; }
@media (prefers-color-scheme: dark) { :root { --bg: #161513; --fg: #ece9e3; --muted: #a39d93; --line: #2e2b27; --link: #8ab4ff; --card: #1e1c19; } }
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); color: var(--fg); font: 16px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
main { max-width: 760px; margin: 0 auto; padding: 24px 16px 48px; }
h1 { font-size: 1.4rem; margin: 0 0 4px; }
h2 { font-size: 1rem; margin: 28px 0 8px; color: var(--muted); font-weight: 600; text-transform: uppercase; letter-spacing: .04em; }
p.sub { margin: 0; color: var(--muted); font-size: .9rem; }
ul { list-style: none; margin: 0; padding: 0; }
li { background: var(--card); border: 1px solid var(--line); border-radius: 10px; margin: 8px 0; }
li a { display: block; padding: 12px 14px; color: inherit; text-decoration: none; }
li a:hover .t, li a:focus .t { color: var(--link); text-decoration: underline; }
.t { font-weight: 600; overflow-wrap: anywhere; }
.m { color: var(--muted); font-size: .85rem; overflow-wrap: anywhere; }
.empty { color: var(--muted); }
</style></head>
<body><main>
<h1>bts pages</h1>
<p class="sub">Served from this machine by bts.</p>
<h2>Pages</h2>
{{if .Pages}}<ul>{{range .Pages}}<li><a href="{{.Href}}"><div class="t">{{.Title}}</div><div class="m">{{.Meta}} · {{.Updated}}</div></a></li>{{end}}</ul>
{{else}}<p class="empty">No pages yet. A session publishes one with <code>bts page publish</code>.</p>{{end}}
{{range .Projects}}<h2>Reviews · {{.Name}}</h2>
<ul>{{range .Reviews}}<li><a href="{{.Href}}"><div class="t">{{.Title}}</div><div class="m">{{.Meta}} · {{.Updated}}</div></a></li>{{end}}</ul>
{{end}}
</main></body></html>
`))
