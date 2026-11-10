package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// A recipe belongs to one Claude Code session (docs/research/
// session-ownership-design.md). The session id reaches bts only through
// hook input, so ownership is bound here, from the Bash call that ran
// `bts recipe create` or `bts recipe claim <id>`: from PostToolUse, and
// from PostToolUseFailure, since a compound call can fail after the create
// succeeded (trial problem #124).  The request the CLI run leaves is what
// proves it ran. A session id survives /compact and --resume;
// --fork-session starts a new one.

// ownerRequestMaxAge bounds how long a create or claim request waits for
// the hook of the Bash call that ran it.
const ownerRequestMaxAge = 2 * time.Minute

var (
	recipeCreateRe = regexp.MustCompile(`\bbts\s+recipe\s+create\b`)
	recipeClaimRe  = regexp.MustCompile(`\bbts\s+recipe\s+claim\s+(r-[0-9]+[A-Za-z0-9._-]*)`)
	recipeIDRe     = regexp.MustCompile(`\br-[0-9]{3,}-[A-Za-z0-9._-]+`)
)

// bindOwnership records input's session as the owner of the recipe a
// successful `bts recipe create` made (only while it has no owner), or of
// the recipe `bts recipe claim <id>` names (always), and logs the change.
func bindOwnership(root string, input *HookInput) {
	if input.ToolName != "Bash" || input.SessionID == "" {
		return
	}
	cmd, _ := input.ToolInput["command"].(string)
	// The text only names a candidate.  A binding also needs the request a
	// real `bts recipe claim` or `create` run leaves (state.OwnerRequest):
	// text inside a heredoc or a quoted report ran no bts and left none.
	var id string
	claim := false
	if m := recipeClaimRe.FindStringSubmatch(cmd); m != nil &&
		state.TakeOwnerRequest(root, m[1], "claim", ownerRequestMaxAge) {
		id, claim = m[1], true
	} else if recipeCreateRe.MatchString(cmd) {
		out, _ := input.ToolResponse["stdout"].(string)
		if out == "" {
			// A call that failed after the create carries its output in
			// error, not tool_response (trial problem #124).
			out = input.Error
		}
		for _, cand := range recipeIDRe.FindAllString(out, -1) {
			if r, err := state.LoadRecipeState(root, cand); err == nil && r.OwnerSession == "" &&
				state.TakeOwnerRequest(root, cand, "create", ownerRequestMaxAge) {
				id = cand
			}
		}
	}
	if id == "" {
		return
	}
	recipe, err := state.LoadRecipeState(root, id)
	if err != nil || recipe.OwnerSession == input.SessionID {
		return
	}
	if !claim && recipe.OwnerSession != "" {
		return
	}
	prev := recipe.OwnerSession
	recipe.OwnerSession = input.SessionID
	if err := state.SaveRecipeState(root, recipe); err != nil {
		fmt.Fprintf(os.Stderr, "[bts] warning: record recipe owner: %v\n", err)
		return
	}
	if err := state.AppendChangelog(root, id, &state.ChangelogEntry{
		Action: "claim",
		Result: fmt.Sprintf("owner session %s -> %s", shortSession(prev), shortSession(input.SessionID)),
	}); err != nil {
		fmt.Fprintf(os.Stderr, "[bts] warning: append changelog: %v\n", err)
	}
}

// isObserver reports whether sessionID is a session other than the
// recipe's owner. A recipe with no owner, or a payload with no session id,
// has no observers: behaviour stays as it was before ownership existed.
func isObserver(recipe *state.RecipeState, sessionID string) bool {
	return recipe != nil && recipe.OwnerSession != "" && sessionID != "" && sessionID != recipe.OwnerSession
}

func shortSession(id string) string {
	if id == "" {
		return "none"
	}
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// observerEditNotice warns a session that does not own the active recipe
// when it writes a file the recipe's tasks.json or wireframe File
// Structure names, and returns "" for anything else. In the AGEN-43
// handoff a stood-down session could still edit and test r-003's code,
// and only its own discipline kept the two sessions from editing the same
// files (trial problem #56). It warns rather than refuses: the observer
// may be the user's own session fixing something on purpose, and a
// refusal would make it claim the recipe just to type.
func observerEditNotice(root string, recipe *state.RecipeState, input *HookInput) string {
	if input.ToolName != "Write" && input.ToolName != "Edit" {
		return ""
	}
	path, _ := input.ToolInput["file_path"].(string)
	rel := projectRelPath(root, input.CWD, path)
	if rel == "" || !recipeFiles(root, recipe.ID)[rel] {
		return ""
	}
	return fmt.Sprintf("[bts] This session does not own recipe %s; session %s works it, and %s is one of its files. "+
		"Leave the recipe's files to that session, or run `bts recipe claim %s` first.",
		recipe.ID, shortSession(recipe.OwnerSession), rel, recipe.ID)
}

// recipeFiles is the set of files the recipe's tasks.json and wireframe
// File Structure rows name, relative to the project root, slash-separated.
// Both are read: tasks.json may not exist yet, and its tasks need not
// cover every row.
func recipeFiles(root, recipeID string) map[string]bool {
	files := map[string]bool{}
	add := func(f string) {
		if f = filepath.ToSlash(filepath.Clean(strings.TrimSpace(f))); f != "" && f != "." {
			files[f] = true
		}
	}
	if ts, err := state.LoadTaskState(root, recipeID); err == nil {
		for _, t := range ts.Tasks {
			add(t.File)
		}
	}
	if data, err := os.ReadFile(filepath.Join(state.RecipeDir(root, recipeID), "wireframe.md")); err == nil {
		for _, k := range engine.ParseWireframeFileTable(string(data)) {
			add(k.Path)
		}
	}
	return files
}

// projectRelPath returns path, absolute or relative to cwd, relative to the
// project root and slash-separated, or "" when it lies outside the project.
func projectRelPath(root, cwd, path string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	rel, err := filepath.Rel(root, filepath.Clean(path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(rel)
}

// observerNotice is the one line another session sees about the recipe.
func observerNotice(recipe *state.RecipeState) string {
	return fmt.Sprintf("[bts] Recipe %s (phase %s) is worked by session %s; this session is not bound to it and gets no recipe gates. "+
		"To take it over, run `bts recipe claim %s`.", recipe.ID, recipe.Phase, shortSession(recipe.OwnerSession), recipe.ID)
}
