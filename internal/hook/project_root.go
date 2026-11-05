package hook

import (
	"os"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// ProjectEnv is set by Claude Code, in every hook's environment, to the root
// of the project the session started in.
const ProjectEnv = "CLAUDE_PROJECT_DIR"

// ProjectRoot is the bts project a hook acts on.
//
// It is found from CLAUDE_PROJECT_DIR, the project whose settings register
// the hooks, before the payload's cwd.  Claude Code's cwd follows every `cd`
// the session makes.  On true-shark the session worked in its scratchpad,
// so 63 hook runs resolved no project and did nothing, silently; one of
// them was the Stop that carried <bts>IMPLEMENT DONE</bts> (trial problem
// #85, AGEN-69).  The cwd remains the fallback for a payload piped by hand,
// where CLAUDE_PROJECT_DIR is not set, or names a directory with no bts.
// Paths in the payload are still resolved against the cwd (projectRelPath,
// outsideProject).
func (in *HookInput) ProjectRoot() (string, error) {
	if dir := os.Getenv(ProjectEnv); dir != "" {
		if root, err := state.FindRoot(dir); err == nil {
			return root, nil
		}
	}
	return state.FindRoot(in.CWD)
}
