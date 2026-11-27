package pages

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// Export turns a page into one HTML file that opens with no server: the way
// pandoc's --embed-resources does it, linked stylesheets, scripts, images
// and media become inline elements and data: URIs.  Remote files stay links
// unless EmbedRemote is set, an element marked data-external="1" is left
// alone, and what cannot be embedded is reported rather than guessed.

// ExportOptions configure Export.
type ExportOptions struct {
	// Resolve reads a local reference (relative to the page); an error
	// means it is missing.
	Resolve func(ref string) ([]byte, error)
	// EmbedRemote downloads http(s) files and inlines them too.
	EmbedRemote bool
	// Fetch downloads a remote file; nil uses a plain HTTP GET.
	Fetch func(u string) ([]byte, error)
	// Drop, when set, removes a script whose src it matches, with its
	// element (a review page's kit, which has nothing to talk to).
	Drop func(src string) bool
	// LocalHosts are host names a teammate cannot reach (127.0.0.1,
	// localhost, the tailnet name); a link to one is reported.
	LocalHosts []string
	// Footer, when set, is a line added at the end of the body.
	Footer string
}

// ExportReport says what Export embedded and what it could not.
type ExportReport struct {
	Embedded []string
	Remote   []string // left as links
	Missing  []string
	Warnings []string
}

func (r *ExportReport) add(list *[]string, s string) {
	for _, x := range *list {
		if x == s {
			return
		}
	}
	*list = append(*list, s)
}

const maxRemote = 20 << 20

func httpFetch(u string) ([]byte, error) {
	c := http.Client{Timeout: 30 * time.Second}
	resp, err := c.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRemote+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxRemote {
		return nil, fmt.Errorf("over %d MB", maxRemote>>20)
	}
	return data, nil
}

// DirResolver reads references relative to dir, never leaving it and never
// reading a dot file or the review page data.
func DirResolver(dir string) func(string) ([]byte, error) {
	return func(ref string) ([]byte, error) {
		clean := path.Clean(ref)
		if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." || strings.HasPrefix(clean, "/") {
			return nil, fmt.Errorf("%s is outside the page's folder", ref)
		}
		for _, seg := range strings.Split(clean, "/") {
			if strings.HasPrefix(seg, ".") {
				return nil, fmt.Errorf("%s: dot files are not exported", ref)
			}
		}
		if clean == "data" || strings.HasPrefix(clean, "data/") {
			if _, err := os.Stat(filepath.Join(dir, "page.html")); err == nil {
				return nil, fmt.Errorf("%s is review page data", ref)
			}
		}
		return os.ReadFile(filepath.Join(dir, filepath.FromSlash(clean)))
	}
}

type refKind int

const (
	refSkip refKind = iota // data:, a fragment, javascript:, empty
	refLocal
	refRemote
)

var schemeRe = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9+.-]*):`)

func classify(ref string) (refKind, string) {
	r := strings.TrimSpace(ref)
	switch {
	case r == "" || strings.HasPrefix(r, "#"):
		return refSkip, r
	case strings.HasPrefix(r, "//"):
		return refRemote, "https:" + r
	}
	if m := schemeRe.FindStringSubmatch(r); m != nil {
		switch strings.ToLower(m[1]) {
		case "http", "https":
			return refRemote, r
		default:
			// data:, javascript:, mailto:, blob: and the like stay as written.
			return refSkip, r
		}
	}
	// Strip a query or fragment from a local file reference.
	if i := strings.IndexAny(r, "?#"); i >= 0 {
		r = r[:i]
	}
	if r == "" {
		return refSkip, ref
	}
	return refLocal, r
}

func mimeOf(name string, data []byte) string {
	if t := mime.TypeByExtension(strings.ToLower(path.Ext(name))); t != "" {
		return t
	}
	return http.DetectContentType(data)
}

func dataURI(name string, data []byte) string {
	t := mimeOf(name, data)
	if i := strings.Index(t, ";"); i >= 0 {
		t = t[:i]
	}
	return "data:" + t + ";base64," + base64.StdEncoding.EncodeToString(data)
}

type exporter struct {
	o   ExportOptions
	rep *ExportReport
}

// load reads a reference, local or (with EmbedRemote) remote; ok false
// means leave the reference as it is.
func (e *exporter) load(ref string) (data []byte, abs string, ok bool) {
	kind, r := classify(ref)
	switch kind {
	case refLocal:
		if strings.HasPrefix(r, "/") {
			e.rep.add(&e.rep.Missing, r+" (a path from the server's root, which the file has no server for)")
			return nil, "", false
		}
		d, err := e.o.Resolve(r)
		if err != nil {
			e.rep.add(&e.rep.Missing, r)
			return nil, "", false
		}
		e.rep.add(&e.rep.Embedded, r)
		return d, r, true
	case refRemote:
		if !e.o.EmbedRemote {
			e.rep.add(&e.rep.Remote, r)
			return nil, "", false
		}
		fetch := e.o.Fetch
		if fetch == nil {
			fetch = httpFetch
		}
		d, err := fetch(r)
		if err != nil {
			e.rep.add(&e.rep.Missing, r+" ("+err.Error()+")")
			return nil, "", false
		}
		e.rep.add(&e.rep.Embedded, r)
		return d, r, true
	}
	return nil, "", false
}

var cssURLRe = regexp.MustCompile(`url\(\s*(['"]?)([^'")]+?)(['"]?)\s*\)`)

// css inlines the url() references of a stylesheet loaded from base (a
// local path or a remote URL; "" for a style element in the page).  A
// remote stylesheet's relative references become absolute, so they still
// load from where it came from.
func (e *exporter) css(text, base string) string {
	if strings.Contains(text, "@import") {
		e.rep.add(&e.rep.Warnings, "a stylesheet uses @import, which is left to load at run time")
	}
	remoteBase := strings.HasPrefix(base, "http://") || strings.HasPrefix(base, "https://")
	return cssURLRe.ReplaceAllStringFunc(text, func(m string) string {
		sm := cssURLRe.FindStringSubmatch(m)
		ref := sm[2]
		kind, r := classify(ref)
		if kind == refSkip {
			return m
		}
		if kind == refLocal {
			if remoteBase {
				bu, err := url.Parse(base)
				if err != nil {
					return m
				}
				ru, err := url.Parse(ref)
				if err != nil {
					return m
				}
				abs := bu.ResolveReference(ru).String()
				if d, _, ok := e.load(abs); ok {
					return "url(" + dataURI(abs, d) + ")"
				}
				return "url(" + abs + ")"
			}
			if base != "" && !strings.HasPrefix(r, "/") {
				r = path.Join(path.Dir(base), r)
			}
		}
		if d, abs, ok := e.load(r); ok {
			return "url(" + dataURI(abs, d) + ")"
		}
		return m
	})
}

func attr(t html.Token, key string) (string, bool) {
	for _, a := range t.Attr {
		if a.Key == key && a.Namespace == "" {
			return a.Val, true
		}
	}
	return "", false
}

func renderStart(t html.Token, drop map[string]bool, set map[string]string, selfClose bool) string {
	var b strings.Builder
	b.WriteString("<" + t.Data)
	seen := map[string]bool{}
	for _, a := range t.Attr {
		if drop[a.Key] {
			continue
		}
		v := a.Val
		if nv, ok := set[a.Key]; ok {
			v, seen[a.Key] = nv, true
		}
		b.WriteString(" " + a.Key + `="` + html.EscapeString(v) + `"`)
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		if !seen[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString(" " + k + `="` + html.EscapeString(set[k]) + `"`)
	}
	if selfClose {
		b.WriteString(" /")
	}
	b.WriteString(">")
	return b.String()
}

var runtimeLoadRe = regexp.MustCompile(`\bfetch\s*\(|\bimport\s*\(|new\s+EventSource\b|XMLHttpRequest`)

// Export makes doc self-contained.
func Export(doc []byte, o ExportOptions) ([]byte, *ExportReport, error) {
	if o.Resolve == nil {
		return nil, nil, errors.New("export: no resolver")
	}
	e := &exporter{o: o, rep: &ExportReport{}}
	z := html.NewTokenizer(bytes.NewReader(doc))
	var out bytes.Buffer
	rawTag := ""         // the raw-text element we are in (script, style)
	skipText := false    // drop the text of a script whose src was inlined
	dropElement := false // drop a script element entirely, to its end tag
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if errors.Is(z.Err(), io.EOF) {
				break
			}
			return nil, nil, z.Err()
		}
		raw := append([]byte(nil), z.Raw()...)
		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			t := z.Token()
			self := tt == html.SelfClosingTagToken
			if t.Data == "script" || t.Data == "style" {
				rawTag = t.Data
			}
			if v, _ := attr(t, "data-external"); v == "1" {
				out.Write(raw)
				continue
			}
			out.WriteString(e.rewrite(t, raw, self, &skipText, &dropElement))
		case html.EndTagToken:
			t := z.Token()
			if t.Data == rawTag {
				rawTag = ""
			}
			if dropElement && t.Data == "script" {
				dropElement = false
				continue
			}
			skipText = false
			out.Write(raw)
		case html.TextToken:
			if dropElement || skipText {
				continue
			}
			switch rawTag {
			case "style":
				out.WriteString(e.css(string(raw), ""))
			case "script":
				if runtimeLoadRe.Match(raw) {
					e.rep.add(&e.rep.Warnings, "a script loads data at run time (fetch, import, EventSource); those loads fail in the file")
				}
				out.Write(raw)
			default:
				out.Write(raw)
			}
		default:
			out.Write(raw)
		}
	}
	result := out.Bytes()
	if o.Footer != "" {
		foot := `<footer data-bts-export style="margin:32px 16px 16px;font:12px/1.4 -apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;opacity:.65;text-align:center">` + html.EscapeString(o.Footer) + `</footer>`
		low := bytes.ToLower(result)
		if i := bytes.LastIndex(low, []byte("</body>")); i >= 0 {
			result = append(append(append([]byte{}, result[:i]...), foot...), result[i:]...)
		} else {
			result = append(result, foot...)
		}
	}
	sort.Strings(e.rep.Embedded)
	sort.Strings(e.rep.Remote)
	sort.Strings(e.rep.Missing)
	return result, e.rep, nil
}

// rewrite returns the element's start tag with its references inlined, or
// the raw tag unchanged.
func (e *exporter) rewrite(t html.Token, raw []byte, self bool, skipText, dropElement *bool) string {
	switch t.Data {
	case "link":
		rel, _ := attr(t, "rel")
		href, ok := attr(t, "href")
		if !ok {
			return string(raw)
		}
		rels := strings.Fields(strings.ToLower(rel))
		has := func(x string) bool {
			for _, r := range rels {
				if r == x {
					return true
				}
			}
			return false
		}
		switch {
		case has("stylesheet"):
			d, abs, ok := e.load(href)
			if !ok {
				return string(raw)
			}
			open := "<style"
			if m, ok := attr(t, "media"); ok {
				open += ` media="` + html.EscapeString(m) + `"`
			}
			css := strings.ReplaceAll(e.css(string(d), abs), "</style", `<\/style`)
			return open + ">" + css + "</style>"
		case has("icon") || has("apple-touch-icon"):
			if d, abs, ok := e.load(href); ok {
				return renderStart(t, nil, map[string]string{"href": dataURI(abs, d)}, self)
			}
		case has("preload") || has("modulepreload") || has("prefetch"):
			// A hint about a file that is embedded or remote anyway.
			return string(raw)
		}
		return string(raw)
	case "script":
		src, ok := attr(t, "src")
		if !ok {
			return string(raw)
		}
		if e.o.Drop != nil && e.o.Drop(src) {
			*dropElement = true
			return ""
		}
		if typ, _ := attr(t, "type"); strings.EqualFold(typ, "module") {
			e.rep.add(&e.rep.Warnings, "a module script ("+src+") may import other files at run time")
		}
		d, _, ok := e.load(src)
		if !ok {
			return string(raw)
		}
		*skipText = true
		js := strings.ReplaceAll(string(d), "</script", `<\/script`)
		return renderStart(t, map[string]bool{"src": true, "integrity": true, "crossorigin": true}, nil, false) + js
	case "img", "source", "video", "audio", "track", "input", "embed":
		set := map[string]string{}
		for _, k := range []string{"src", "poster"} {
			if v, ok := attr(t, k); ok {
				if d, abs, ok := e.load(v); ok {
					set[k] = dataURI(abs, d)
				}
			}
		}
		if v, ok := attr(t, "srcset"); ok && v != "" {
			e.rep.add(&e.rep.Warnings, "srcset is left as written ("+t.Data+")")
		}
		if len(set) > 0 {
			return renderStart(t, nil, set, self)
		}
	case "a", "iframe", "form":
		for _, k := range []string{"href", "src", "action"} {
			v, ok := attr(t, k)
			if !ok {
				continue
			}
			kind, r := classify(v)
			switch kind {
			case refRemote:
				if u, err := url.Parse(r); err == nil {
					for _, h := range e.o.LocalHosts {
						if strings.EqualFold(u.Hostname(), h) {
							e.rep.add(&e.rep.Warnings, "a link points at "+u.Host+", which a teammate cannot open")
						}
					}
				}
			case refLocal:
				if t.Data == "iframe" {
					e.rep.add(&e.rep.Warnings, "an iframe loads "+r+", which the file does not carry")
				} else if r != "" {
					e.rep.add(&e.rep.Warnings, "a link to "+r+" will not open from the file")
				}
			}
		}
	}
	return string(raw)
}
