package hook

import (
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// A recipe belongs to one Claude Code session (docs/research/
// session-ownership-design.md). The session id reaches bts only through
// hook input, so ownership is bound here, from the successful Bash call
// that ran `bts recipe create` or `bts recipe claim <id>`. A session id
// survives /compact and --resume; --fork-session starts a new one.

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
		stdout, _ := input.ToolResponse["stdout"].(string)
		for _, cand := range recipeIDRe.FindAllString(stdout, -1) {
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

// observerNotice is the one line another session sees about the recipe.
func observerNotice(recipe *state.RecipeState) string {
	return fmt.Sprintf("[bts] Recipe %s (phase %s) is worked by session %s; this session is not bound to it and gets no recipe gates. "+
		"To take it over, run `bts recipe claim %s`.", recipe.ID, recipe.Phase, shortSession(recipe.OwnerSession), recipe.ID)
}
