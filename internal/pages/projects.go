package pages

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
)

// A review page lives in its project (.bts/local/{recipes,features}/<id>/
// review/), so the server needs to know the projects whose pages it serves.
// `bts review open` registers its project's root under a short key that
// goes into the page's URL: /r/<project>/<subject>/.

func projectsPath(home string) string { return filepath.Join(home, "projects.json") }

var keyUnsafe = regexp.MustCompile(`[^a-z0-9._-]+`)

// ProjectKey names a project root in URLs: the root's base name and six hex
// digits of its absolute path's SHA-256, so two worktrees of one repository
// get different keys.
func ProjectKey(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	sum := sha256.Sum256([]byte(abs))
	base := strings.Trim(keyUnsafe.ReplaceAllString(strings.ToLower(filepath.Base(abs)), "-"), "-.")
	if base == "" {
		base = "project"
	}
	if len(base) > 40 {
		base = base[:40]
	}
	return base + "-" + hex.EncodeToString(sum[:])[:6]
}

// Projects reads projects.json: key to root.
func Projects(home string) (map[string]string, error) {
	m := map[string]string{}
	err := readJSON(projectsPath(home), &m)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	return m, err
}

// lockHome serialises writers of the store's shared files across
// processes: projects.json and every page directory.
func lockHome(home string) (func(), error) { return lockFile(home, ".lock") }

// lockFile takes an exclusive lock on a file in home, waiting for it.
func lockFile(home, name string) (func(), error) {
	if err := os.MkdirAll(home, 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(home, name), os.O_CREATE|os.O_RDWR, 0644)
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

// RegisterProject records root and returns its key.
func RegisterProject(home, root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	key := ProjectKey(abs)
	unlock, err := lockHome(home)
	if err != nil {
		return "", err
	}
	defer unlock()
	m, err := Projects(home)
	if err != nil {
		return "", err
	}
	if m[key] == abs {
		return key, nil
	}
	m[key] = abs
	return key, writeJSON(projectsPath(home), m, 0644)
}

// ProjectRoot is the root registered under key; "" when there is none or
// its directory is gone.
func ProjectRoot(home, key string) string {
	m, err := Projects(home)
	if err != nil {
		return ""
	}
	root := m[key]
	if root == "" {
		return ""
	}
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return ""
	}
	return root
}

// PruneProjects drops projects whose root is gone (a removed worktree) and
// returns the keys it dropped.
func PruneProjects(home string) ([]string, error) {
	unlock, err := lockHome(home)
	if err != nil {
		return nil, err
	}
	defer unlock()
	m, err := Projects(home)
	if err != nil {
		return nil, err
	}
	var gone []string
	for k, root := range m {
		if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
			gone = append(gone, k)
			delete(m, k)
		}
	}
	if len(gone) == 0 {
		return nil, nil
	}
	sort.Strings(gone)
	return gone, writeJSON(projectsPath(home), m, 0644)
}
