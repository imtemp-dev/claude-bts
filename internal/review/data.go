package review

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

// A local page (review.surface: local) keeps what the claude.ai page keeps
// in its `db` capability as files under the subject's work directory:
// data/notes/<id>.json, data/rounds/<id>.json and data/proposals/<id>.json.
// That is the layout readExport reads, so Import takes a local page's data
// as it takes an ArtifactData export.  The page writes through `bts review
// serve`; the session's commands write the same files; the server watches
// them and pushes every change to the page.

// SurfaceLocal marks a page bts serves itself (State.Surface).
const SurfaceLocal = "local"

// DataDir is the page data's directory under the subject's work directory.
const DataDir = "data"

// Collections are the page data's collections.
var Collections = []string{"notes", "rounds", "proposals"}

// waitingFile is touched by `bts review wait` while it waits, so the page
// can say a session will take the next round at once.
const waitingFile = ".waiting"

// DataPath is the subject's page data directory.
func DataPath(sub Subject) string { return filepath.Join(sub.Work, DataDir) }

var docIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// ValidDocID says whether id can name a page document: it becomes a file
// name, so no path separator, no leading dot, nothing long.
func ValidDocID(id string) bool { return docIDRe.MatchString(id) }

func validCollection(c string) bool { return contains(Collections, c) }

func docPath(sub Subject, collection, id string) (string, error) {
	if !validCollection(collection) {
		return "", fmt.Errorf("no collection %q: the page keeps %s", collection, strings.Join(Collections, ", "))
	}
	if !ValidDocID(id) {
		return "", fmt.Errorf("bad document id %q", id)
	}
	return filepath.Join(DataPath(sub), collection, id+".json"), nil
}

// lockData serialises writers of one subject's page data across
// processes: the server and the session's commands.
func lockData(sub Subject) (func(), error) {
	dir := DataPath(sub)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

func writeAtomic(path string, v interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func readDocFile(path string) (map[string]interface{}, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

// ReadDoc reads one page document; nil and no error when it does not exist.
func ReadDoc(sub Subject, collection, id string) (map[string]interface{}, error) {
	path, err := docPath(sub, collection, id)
	if err != nil {
		return nil, err
	}
	m, err := readDocFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return m, err
}

// PutDoc writes a page document whole, replacing what was there.
func PutDoc(sub Subject, collection, id string, doc map[string]interface{}) error {
	path, err := docPath(sub, collection, id)
	if err != nil {
		return err
	}
	unlock, err := lockData(sub)
	if err != nil {
		return err
	}
	defer unlock()
	return writeAtomic(path, doc)
}

// MergeDoc sets fields on a page document, creating it when it does not
// exist; the fields it does not name stay as they were.
func MergeDoc(sub Subject, collection, id string, fields map[string]interface{}) error {
	path, err := docPath(sub, collection, id)
	if err != nil {
		return err
	}
	unlock, err := lockData(sub)
	if err != nil {
		return err
	}
	defer unlock()
	cur, err := readDocFile(path)
	if errors.Is(err, os.ErrNotExist) {
		cur = map[string]interface{}{}
	} else if err != nil {
		return err
	}
	for k, v := range fields {
		cur[k] = v
	}
	return writeAtomic(path, cur)
}

// DeleteDoc removes a page document; deleting one that is gone is no error.
func DeleteDoc(sub Subject, collection, id string) error {
	path, err := docPath(sub, collection, id)
	if err != nil {
		return err
	}
	unlock, err := lockData(sub)
	if err != nil {
		return err
	}
	defer unlock()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// ReadCollection reads every document of a collection, by id.
func ReadCollection(sub Subject, collection string) (map[string]map[string]interface{}, error) {
	if !validCollection(collection) {
		return nil, fmt.Errorf("no collection %q", collection)
	}
	dir := filepath.Join(DataPath(sub), collection)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]map[string]interface{}{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]interface{}{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		m, err := readDocFile(filepath.Join(dir, name))
		if err != nil {
			// A half-written file cannot appear (writes rename into
			// place); a file someone broke by hand is skipped rather
			// than taking the whole page down.
			continue
		}
		out[id] = m
	}
	return out, nil
}

// PageData is every collection of a subject's page data.
func PageData(sub Subject) (map[string]map[string]map[string]interface{}, error) {
	out := map[string]map[string]map[string]interface{}{}
	for _, c := range Collections {
		docs, err := ReadCollection(sub, c)
		if err != nil {
			return nil, err
		}
		out[c] = docs
	}
	return out, nil
}

// DataStamp changes whenever a page document is written or removed, or
// page.html changes: the server compares it to know when to push.
func DataStamp(sub Subject) string {
	h := sha256.New()
	var lines []string
	add := func(path, name string) {
		if fi, err := os.Stat(path); err == nil {
			lines = append(lines, fmt.Sprintf("%s %d %d", name, fi.Size(), fi.ModTime().UnixNano()))
		}
	}
	add(filepath.Join(sub.Work, "page.html"), "page.html")
	for _, c := range Collections {
		dir := filepath.Join(DataPath(sub), c)
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".json") && !strings.HasPrefix(e.Name(), ".") {
				add(filepath.Join(dir, e.Name()), c+"/"+e.Name())
			}
		}
	}
	sort.Strings(lines)
	h.Write([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// PageStamp is page.html's own stamp, so the page reloads only when the
// page itself changed, not on every note.
func PageStamp(sub Subject) string {
	fi, err := os.Stat(filepath.Join(sub.Work, "page.html"))
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d-%d", fi.Size(), fi.ModTime().UnixNano())
}

// TouchWaiting records that a session is waiting for this subject's next
// round (`bts review wait`).
func TouchWaiting(sub Subject) error {
	dir := DataPath(sub)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, waitingFile)
	now := time.Now()
	if err := os.Chtimes(path, now, now); err == nil {
		return nil
	}
	return os.WriteFile(path, []byte(now.UTC().Format(time.RFC3339)+"\n"), 0644)
}

// ClearWaiting says no session is waiting any more.
func ClearWaiting(sub Subject) { _ = os.Remove(filepath.Join(DataPath(sub), waitingFile)) }

// SessionWaiting says whether a `bts review wait` touched the subject
// within the last minute.
func SessionWaiting(sub Subject) bool {
	fi, err := os.Stat(filepath.Join(DataPath(sub), waitingFile))
	return err == nil && time.Since(fi.ModTime()) < time.Minute
}

// SubmittedRounds lists the page's rounds the user sent that no session
// has picked up yet, by round number.
func SubmittedRounds(sub Subject) ([]int, error) {
	rounds, err := ReadCollection(sub, "rounds")
	if err != nil {
		return nil, err
	}
	var out []int
	for _, d := range rounds {
		if str(d, "status") == "submitted" {
			if n := num(d, "round"); n > 0 {
				out = append(out, n)
			}
		}
	}
	sort.Ints(out)
	return out, nil
}

// IsLocal says whether the subject's page is one bts serves itself.
func IsLocal(sub Subject) bool {
	s, err := Load(sub)
	return err == nil && s.Surface == SurfaceLocal
}
