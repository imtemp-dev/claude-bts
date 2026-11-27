package pages

import (
	"crypto/rand"
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A general page is <home>/<id>/: page.json, and v/<n>/ holding version n's
// files, the page itself as index.html beside the files it loads.  Every
// version is kept.  Publishing follows the Artifact tool's rules, which
// sessions already know: a named page is updated; a file an earlier publish
// used updates that page; anything else makes a new page.

// MaxFile is the largest page or supporting file a publish takes, the
// Artifact tool's own limit.
const MaxFile = 16 << 20

// PageFile is the name a version keeps its page under.
const PageFile = "index.html"

// Page is page.json.
type Page struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Version     int       `json:"version"`
	Created     string    `json:"created"`
	Updated     string    `json:"updated"`
	Source      string    `json:"source,omitempty"` // the file last published, absolute
	From        string    `json:"from,omitempty"`   // the working directory it was published from
	Versions    []Version `json:"versions"`
}

// Version is one publish of a page.
type Version struct {
	N      int    `json:"n"`
	At     string `json:"at"`
	Title  string `json:"title"`
	Source string `json:"source,omitempty"`
}

var pageIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// ValidPageID says whether id can name a page.
func ValidPageID(id string) bool { return pageIDRe.MatchString(id) }

func pageDir(home, id string) string { return filepath.Join(home, id) }

// VersionDir is where version n of page id keeps its files.
func VersionDir(home, id string, n int) string {
	return filepath.Join(pageDir(home, id), "v", strconv.Itoa(n))
}

// CurrentDir is where the page's current version keeps its files.
func CurrentDir(home string, p *Page) string { return VersionDir(home, p.ID, p.Version) }

// ErrNoPage is returned for a page id the store does not hold.
var ErrNoPage = errors.New("no such page")

// Get reads a page's record.
func Get(home, id string) (*Page, error) {
	if !ValidPageID(id) {
		return nil, fmt.Errorf("%w: %q", ErrNoPage, id)
	}
	var p Page
	if err := readJSON(filepath.Join(pageDir(home, id), "page.json"), &p); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrNoPage, id)
		}
		return nil, err
	}
	return &p, nil
}

// List returns every page, the most recently updated first.
func List(home string) ([]*Page, error) {
	entries, err := os.ReadDir(home)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Page
	for _, e := range entries {
		if !e.IsDir() || !ValidPageID(e.Name()) {
			continue
		}
		if p, err := Get(home, e.Name()); err == nil {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Updated != out[j].Updated {
			return out[i].Updated > out[j].Updated
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// PublishOptions are what `bts page publish` passes.
type PublishOptions struct {
	// File is the page's HTML.
	File string
	// Files maps a published path (what the page references, relative) to
	// its source file; an empty source removes the path from the page.
	// Files the previous version had and this map does not name are kept.
	Files map[string]string
	// Title and Description; the title defaults to the page's <title>, then
	// the file's name.
	Title       string
	Description string
	// Page names the page to update.
	Page string
	// New makes a new page even when File was published before.
	New bool
	// From is the working directory of the publishing command.
	From string
}

var titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

// HTMLTitle is the text of an HTML document's <title>; "" when it has none.
func HTMLTitle(doc []byte) string {
	m := titleRe.FindSubmatch(doc)
	if m == nil {
		return ""
	}
	return strings.Join(strings.Fields(html.UnescapeString(string(m[1]))), " ")
}

var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

const idAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

// NewPageID makes a page id from a title: its slug and four random
// characters, so two pages with one title never meet.
func NewPageID(title string) (string, error) {
	slug := strings.Trim(slugUnsafe.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(slug) > 40 {
		slug = strings.TrimRight(slug[:40], "-")
		if i := strings.LastIndex(slug, "-"); i > 20 {
			slug = slug[:i]
		}
	}
	if slug == "" {
		slug = "page"
	}
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = idAlphabet[int(b[i])%len(idAlphabet)]
	}
	return slug + "-" + string(b), nil
}

// validPublished says whether p can be a supporting file's published path:
// relative, clean, no dot segment, and not the page's own name.
func validPublished(p string) error {
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, `\`) {
		return fmt.Errorf("published path %q: give a relative path such as img/chart.png", p)
	}
	if path.Clean(p) != p {
		return fmt.Errorf("published path %q is not clean", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." || strings.HasPrefix(seg, ".") {
			return fmt.Errorf("published path %q: no .. or dot segments", p)
		}
	}
	if p == PageFile {
		return fmt.Errorf("published path %q is the page itself", p)
	}
	if strings.HasPrefix(p, "v/") {
		return fmt.Errorf("published path %q: v/ holds the page's older versions", p)
	}
	return nil
}

// FindBySource is the most recently updated page last published from the
// file at abs; nil when there is none.
func FindBySource(home, abs string) (*Page, error) {
	list, err := List(home)
	if err != nil {
		return nil, err
	}
	for _, p := range list {
		if p.Source == abs {
			return p, nil
		}
	}
	return nil, nil
}

// Publish creates a page or adds a version to one, and reports whether it
// made a new page.
func Publish(home string, o PublishOptions) (*Page, bool, error) {
	src, err := filepath.Abs(o.File)
	if err != nil {
		return nil, false, err
	}
	if ext := strings.ToLower(filepath.Ext(src)); ext != ".html" && ext != ".htm" {
		return nil, false, fmt.Errorf("%s: a page is an .html file", o.File)
	}
	doc, err := readCapped(src)
	if err != nil {
		return nil, false, err
	}
	for pub, from := range o.Files {
		if err := validPublished(pub); err != nil {
			return nil, false, err
		}
		if from != "" {
			if fi, err := os.Stat(from); err != nil || !fi.Mode().IsRegular() {
				return nil, false, fmt.Errorf("supporting file %s: not a readable file", from)
			}
		}
	}
	unlock, err := lockHome(home)
	if err != nil {
		return nil, false, err
	}
	defer unlock()

	var p *Page
	switch {
	case o.Page != "":
		if p, err = Get(home, o.Page); err != nil {
			return nil, false, err
		}
	case !o.New:
		if p, err = FindBySource(home, src); err != nil {
			return nil, false, err
		}
	}
	title := strings.TrimSpace(o.Title)
	if title == "" {
		title = HTMLTitle(doc)
	}
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	}
	created := p == nil
	// Nanoseconds, so two publishes in one second still order.
	at := time.Now().UTC().Format(time.RFC3339Nano)
	if created {
		id, err := NewPageID(title)
		if err != nil {
			return nil, false, err
		}
		for _, e := os.Stat(pageDir(home, id)); e == nil; _, e = os.Stat(pageDir(home, id)) {
			if id, err = NewPageID(title); err != nil {
				return nil, false, err
			}
		}
		p = &Page{ID: id, Created: at}
	}
	n := p.Version + 1
	dir := VersionDir(home, p.ID, n)
	if err := os.RemoveAll(dir); err != nil {
		return nil, false, err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, false, err
	}
	fail := func(err error) (*Page, bool, error) {
		_ = os.RemoveAll(dir)
		return nil, false, err
	}
	// The previous version's supporting files carry over unless replaced
	// or removed.
	if p.Version > 0 {
		prev := VersionDir(home, p.ID, p.Version)
		err := filepath.WalkDir(prev, func(fp string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(prev, fp)
			rel = filepath.ToSlash(rel)
			if rel == PageFile {
				return nil
			}
			if _, named := o.Files[rel]; named {
				return nil
			}
			return copyFile(fp, filepath.Join(dir, filepath.FromSlash(rel)))
		})
		if err != nil {
			return fail(err)
		}
	}
	for pub, from := range o.Files {
		if from == "" {
			continue
		}
		data, err := readCapped(from)
		if err != nil {
			return fail(err)
		}
		if err := writeFileAtomic(filepath.Join(dir, filepath.FromSlash(pub)), data, 0644); err != nil {
			return fail(err)
		}
	}
	if err := writeFileAtomic(filepath.Join(dir, PageFile), doc, 0644); err != nil {
		return fail(err)
	}
	p.Version = n
	p.Title = title
	if o.Description != "" {
		p.Description = o.Description
	}
	p.Updated = at
	p.Source = src
	if o.From != "" {
		p.From = o.From
	}
	p.Versions = append(p.Versions, Version{N: n, At: at, Title: title, Source: src})
	if err := writeJSON(filepath.Join(pageDir(home, p.ID), "page.json"), p, 0644); err != nil {
		return fail(err)
	}
	return p, created, nil
}

// Delete removes a page and every version of it.
func Delete(home, id string) error {
	if _, err := Get(home, id); err != nil {
		return err
	}
	unlock, err := lockHome(home)
	if err != nil {
		return err
	}
	defer unlock()
	return os.RemoveAll(pageDir(home, id))
}

// VersionFiles lists the files of a page version, relative and with
// forward slashes, the page first.
func VersionFiles(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(fp string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, fp)
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if (out[i] == PageFile) != (out[j] == PageFile) {
			return out[i] == PageFile
		}
		return out[i] < out[j]
	})
	return out, err
}

func readCapped(p string) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFile {
		return nil, fmt.Errorf("%s is over %d MB", p, MaxFile>>20)
	}
	return data, nil
}

func copyFile(from, to string) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return writeFileAtomic(to, data, 0644)
}
