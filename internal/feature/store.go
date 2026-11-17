// Package feature is the level between a project and its recipes: one
// feature, several items, each item worked as a recipe in its own worktree.
//
// A feature lives in a store outside every worktree, because the repositories
// bts visits may track no .bts/ and a Multica task's worktree is deleted after
// its issue closes.  Each file in a feature has one writer: the manager writes
// feature.json and the documents beside it; a child writes only its own
// attempt directory under items/.  An item's progress is derived from what
// the children wrote, never stored twice.
package feature

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// StoreEnv overrides the store's location.
const StoreEnv = "BTS_FEATURE_STORE"

// StoreRoot is where features live: $BTS_FEATURE_STORE, else ~/.bts/features.
func StoreRoot() (string, error) {
	if d := strings.TrimSpace(os.Getenv(StoreEnv)); d != "" {
		return filepath.Abs(d)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("no home directory for the feature store; set %s", StoreEnv)
	}
	return filepath.Join(home, ".bts", "features"), nil
}

// RepoKey turns a remote URL into the store directory for its repository:
// host, owner and name in lower case, without user, port, ".git" or a
// trailing slash, so the SSH and HTTPS forms of one repository agree.  It
// returns "" for anything that is not a remote URL (a local path, file://).
func RepoKey(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	var host, path string
	switch {
	case strings.Contains(s, "://"):
		u, err := url.Parse(s)
		if err != nil || u.Scheme == "file" {
			return ""
		}
		host, path = u.Hostname(), u.Path
	case strings.Contains(s, ":") && !strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "."):
		// scp form: [user@]host:owner/name
		i := strings.Index(s, ":")
		host, path = s[:i], s[i+1:]
		if j := strings.LastIndex(host, "@"); j >= 0 {
			host = host[j+1:]
		}
	default:
		return ""
	}
	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, ".git")
	path = strings.Trim(path, "/")
	if host == "" || path == "" {
		return ""
	}
	key := strings.ToLower(host + "/" + path)
	if !ValidKey(key) {
		return ""
	}
	return key
}

// ValidKey reports whether key is safe as a path under the store: no empty,
// "." or ".." segment, so a feature directory cannot land outside it.
func ValidKey(key string) bool {
	if key == "" || strings.HasPrefix(key, "/") {
		return false
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.ContainsAny(seg, "\\\x00") {
			return false
		}
	}
	return true
}

// Repo is the repository a command runs in.
type Repo struct {
	URL       string `json:"url,omitempty"`
	Key       string `json:"key"`
	CommonDir string `json:"-"`
}

// RepoAt describes the git repository holding dir.  With no origin the key
// is local/<name>-<hash of the git common dir>, which every worktree of one
// clone shares.
func RepoAt(dir string) (*Repo, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return nil, fmt.Errorf("%s is not inside a git repository", dir)
	}
	common := strings.TrimSpace(string(out))
	r := &Repo{CommonDir: common}
	if u, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output(); err == nil {
		r.URL = strings.TrimSpace(string(u))
	}
	r.Key = RepoKey(r.URL)
	if r.Key == "" {
		name := filepath.Base(common)
		if name == ".git" {
			name = filepath.Base(filepath.Dir(common))
		}
		name = strings.TrimSuffix(name, ".git")
		sum := sha256.Sum256([]byte(common))
		r.Key = "local/" + slugOr(name, "repo") + "-" + hex.EncodeToString(sum[:])[:8]
	}
	return r, nil
}

// DefaultBase is the branch origin/HEAD names, else "main".
func DefaultBase(dir string) string {
	if out, err := exec.Command("git", "-C", dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD").Output(); err == nil {
		if b := strings.TrimPrefix(strings.TrimSpace(string(out)), "origin/"); b != "" {
			return b
		}
	}
	return "main"
}

func slugOr(s, fallback string) string {
	if v := state.Slugify(s); v != "" {
		return v
	}
	return fallback
}

var idPrefix = regexp.MustCompile(`^f-\d+`)

// idsDir holds one directory per feature number, f-NNN, made with os.Mkdir
// so two sessions cannot take the same number, whatever repository each is
// in.  Its "dir" file names the feature's directory.
func idsDir(store string) string { return filepath.Join(store, ".ids") }

// allocate reserves the next feature number and makes the feature's
// directory under the repository key.
func allocate(store, key, title string) (id, dir string, err error) {
	ids := idsDir(store)
	if err := os.MkdirAll(ids, 0755); err != nil {
		return "", "", err
	}
	n := 1
	if entries, err := os.ReadDir(ids); err == nil {
		for _, e := range entries {
			var k int
			if _, err := fmt.Sscanf(e.Name(), "f-%d", &k); err == nil && k >= n {
				n = k + 1
			}
		}
	}
	for ; ; n++ {
		num := fmt.Sprintf("f-%03d", n)
		if err := os.Mkdir(filepath.Join(ids, num), 0755); err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", "", err
		}
		id = num + "-" + slugOr(title, "feature")
		rel := filepath.Join(filepath.FromSlash(key), id)
		dir = filepath.Join(store, rel)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", "", err
		}
		// Relative to the store, so the store can be moved.
		if err := os.WriteFile(filepath.Join(ids, num, "dir"), []byte(filepath.ToSlash(rel)+"\n"), 0644); err != nil {
			return "", "", err
		}
		return id, dir, nil
	}
}

// resolveIDDir reads an .ids entry: a path relative to the store, or an
// absolute one.
func resolveIDDir(store, entry string) string {
	data, err := os.ReadFile(filepath.Join(idsDir(store), entry, "dir"))
	if err != nil {
		return ""
	}
	p := filepath.FromSlash(strings.TrimSpace(string(data)))
	if !filepath.IsAbs(p) {
		p = filepath.Join(store, p)
	}
	return p
}

// idNumber is a feature id's number: 3 for "f-3", "f-003" and
// "f-003-slug".
func idNumber(id string) (int, bool) {
	id = strings.TrimSpace(id)
	if idPrefix.FindString(id) == "" {
		return 0, false
	}
	var k int
	if _, err := fmt.Sscanf(id, "f-%d", &k); err != nil {
		return 0, false
	}
	return k, true
}

// SameID reports whether two feature ids name the same feature, however
// each is written ("f-3", "f-003", "f-003-slug").  A comparison of the
// "f-NNN" prefix as text missed a short or unpadded id (trial problem
// #152).
func SameID(a, b string) bool {
	na, oka := idNumber(a)
	nb, okb := idNumber(b)
	return oka && okb && na == nb
}

// Find returns the directory of the feature with id ("f-001", "f-1" or
// "f-001-slug"), from anywhere: features are found by id, never by the
// repository a command happens to run in.
func Find(id string) (string, error) {
	store, err := StoreRoot()
	if err != nil {
		return "", err
	}
	k, ok := idNumber(id)
	if !ok {
		return "", fmt.Errorf("%q is not a feature id (f-NNN or f-NNN-slug)", id)
	}
	num := fmt.Sprintf("f-%03d", k)
	dir := resolveIDDir(store, num)
	if dir == "" {
		return "", fmt.Errorf("no feature %s in the store %s", num, store)
	}
	if _, err := os.Stat(filepath.Join(dir, FeatureFile)); err != nil {
		return "", fmt.Errorf("feature %s: %s has no %s", num, dir, FeatureFile)
	}
	return dir, nil
}

// All returns every feature directory in the store, by id.
func All() ([]string, error) {
	store, err := StoreRoot()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(idsDir(store))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		dir := resolveIDDir(store, e.Name())
		if dir == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, FeatureFile)); err == nil {
			dirs = append(dirs, dir)
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}

// lockTimeout bounds the wait for the lock a manager command holds while it
// changes feature.json.
const lockTimeout = 5 * time.Second

// lockFor takes an exclusive flock on the feature's .lock file, waiting up
// to wait, and returns its release.  The kernel drops the lock when the
// process ends, so a crashed command leaves nothing to clean up and no stale
// rule is needed.
func lockFor(dir string, wait time.Duration) (func(), error) {
	f, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}, nil
		}
		if err != syscall.EWOULDBLOCK {
			_ = f.Close()
			return nil, err
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("%s is locked by another bts feature command; retry", dir)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
