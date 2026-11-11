package template

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// linkedClaudeDirs are the .claude/ directories whose bts-* entries a session
// root links to.  Hooks are not linked: the session's settings name the
// bts root's scripts by absolute path.
var linkedClaudeDirs = []string{"skills", "agents", "rules", "commands"}

// LinkResult says what LinkSession did.
type LinkResult struct {
	Links    int      // symlinks created or refreshed
	Skipped  []string // session paths left alone because something that is not a symlink is there
	Pointer  string   // path of the pointer file written
	Settings string   // settings file that now registers the hooks
}

// LinkSession connects a Claude session root to the bts project at btsRoot
// when they differ: a Multica run starts Claude in its task's workdir, while
// the repository worktree, and .bts/ inside it, is a directory below.  Every
// bts file stays under btsRoot; the session root gets
//
//   - a symlink for each bts-* entry of btsRoot's .claude/{skills,agents,rules,commands},
//     since Claude loads those only from the directory it started in;
//   - bts's hooks in its .claude/settings.local.json, pointing at btsRoot's scripts;
//   - the pointer file .bts-root, which state.FindRoot follows, so a hook whose
//     CLAUDE_PROJECT_DIR is the session root acts on btsRoot.
//
// It is safe to run again: symlinks are refreshed, and a path that is not a
// symlink is left alone and reported.
func LinkSession(sessionRoot, btsRoot string) (*LinkResult, error) {
	absSession, err := filepath.Abs(sessionRoot)
	if err != nil {
		return nil, err
	}
	absBts, err := filepath.Abs(btsRoot)
	if err != nil {
		return nil, err
	}
	if absSession == absBts {
		return nil, fmt.Errorf("the session root is the bts project itself; there is nothing to link")
	}
	if _, err := os.Stat(filepath.Join(absBts, ".bts")); err != nil {
		return nil, fmt.Errorf("%s has no .bts/; run bts init there first", absBts)
	}

	res := &LinkResult{}
	for _, dir := range linkedClaudeDirs {
		src := filepath.Join(absBts, ".claude", dir)
		entries, err := os.ReadDir(src)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		dstDir := filepath.Join(absSession, ".claude", dir)
		if err := os.MkdirAll(dstDir, 0755); err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), "bts-") {
				continue
			}
			dst := filepath.Join(dstDir, e.Name())
			if fi, err := os.Lstat(dst); err == nil {
				if fi.Mode()&os.ModeSymlink == 0 {
					res.Skipped = append(res.Skipped, dst)
					continue
				}
				if err := os.Remove(dst); err != nil {
					return nil, err
				}
			}
			if err := os.Symlink(filepath.Join(src, e.Name()), dst); err != nil {
				return nil, err
			}
			res.Links++
		}
	}

	if err := MergeSessionHookSettings(absSession, absBts); err != nil {
		return nil, fmt.Errorf("register hooks: %w", err)
	}
	res.Settings = filepath.Join(absSession, ".claude", "settings.local.json")

	target := absBts
	if rel, err := filepath.Rel(absSession, absBts); err == nil && !strings.HasPrefix(rel, "..") {
		target = rel
	}
	res.Pointer = filepath.Join(absSession, state.PointerFile)
	if err := os.WriteFile(res.Pointer, []byte(target+"\n"), 0644); err != nil {
		return nil, err
	}
	return res, nil
}
