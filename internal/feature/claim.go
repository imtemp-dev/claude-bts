package feature

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/imtemp-dev/claude-bts/pkg/version"
)

// gitCmd builds a git command in dir that never takes the index lock, since
// it can run inside a hook beside the session's own git commands.
func gitCmd(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	return cmd
}

// git runs git in dir and returns its trimmed output, or "" on failure.
func git(dir string, args ...string) string {
	out, err := gitCmd(dir, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func headSHA(dir string) string { return git(dir, "rev-parse", "HEAD") }
func branch(dir string) string  { return git(dir, "branch", "--show-current") }

// dirty reports uncommitted changes git does not ignore, leaving out bts's
// own files: in a project that tracks .bts/, recipe.json changes at every
// completion.
func dirty(dir string) bool {
	return git(dir, "status", "--porcelain", "--", ".", ":(exclude).bts", ":(exclude).claude", ":(exclude).verify") != ""
}

// isAncestor reports whether sha is in HEAD's history; ok is false when git
// cannot say (no repository, unknown commit).
func isAncestor(dir, sha string) (yes, ok bool) {
	if sha == "" {
		return false, false
	}
	err := gitCmd(dir, "merge-base", "--is-ancestor", sha, "HEAD").Run()
	if err == nil {
		return true, true
	}
	if ee, isExit := err.(*exec.ExitError); isExit && ee.ExitCode() == 1 {
		return false, true
	}
	return false, false
}

// ClaimRequest asks to start an item in a worktree.
type ClaimRequest struct {
	FeatureID string
	ItemID    string
	Root      string // the worktree's bts root
	Type      string // "" for the item's own type; "fix" opens a fix-forward on a done item
}

// Claimed is a claim taken in the store, before its recipe exists.
type Claimed struct {
	Dir          string
	Feature      *Feature
	Item         PlanItem
	Kind         string
	RecipeType   string
	Express      bool
	Attempt      int
	AttemptDir   string
	Confirmation int
	Amends       string // the done attempt's recipe, for a fix-forward
	restoreFrom  string // its snapshot, restored into the worktree if absent
	amendsN      int    // the done attempt's number
}

// BeginClaim checks that the item may start in this worktree and takes the
// next attempt directory with os.Mkdir, so of two concurrent claims exactly
// one wins.  The caller creates the recipe, then calls Write, or Abort.
func BeginClaim(req ClaimRequest) (*Claimed, error) {
	dir, err := Find(req.FeatureID)
	if err != nil {
		return nil, err
	}
	f, err := Load(dir)
	if err != nil {
		return nil, err
	}
	if f.Closed != nil {
		return nil, fmt.Errorf("feature %s is %s", f.ID, f.Closed.Kind)
	}
	n, plan, err := Confirmed(dir, f)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, fmt.Errorf("feature %s is not confirmed yet: the user confirms the intent and plan first (bts feature confirm)", f.ID)
	}
	var pi *PlanItem
	for i := range plan {
		if plan[i].ID == req.ItemID {
			pi = &plan[i]
		}
	}
	if pi == nil {
		return nil, fmt.Errorf("item %s is not in confirmation %d of %s; the changed plan needs the user's confirmation first", req.ItemID, n, f.ID)
	}
	if f.Item(req.ItemID) == nil {
		return nil, fmt.Errorf("item %s was removed from the plan of %s; the removal awaits the user's confirmation, so it is not started", req.ItemID, f.ID)
	}
	// The attempt number is fixed before the state is read.  Two claims
	// racing then either aim at the same number, and os.Mkdir lets one in,
	// or the later one reads the earlier one's directory as holding the
	// item.  Taken after the read, each could aim at its own number.
	next := maxAttempt(dir, req.ItemID) + 1
	s, err := Derive(dir, f)
	if err != nil {
		return nil, err
	}
	is := s.item(req.ItemID)
	if is == nil {
		return nil, fmt.Errorf("item %s has no status in %s", req.ItemID, f.ID)
	}
	switch is.State {
	case StateSkipped, StateDeferred:
		return nil, fmt.Errorf("item %s is %s", is.ID, is.State)
	}
	if is.latest != nil && is.latest.Open() {
		holder := fmt.Sprintf("attempt a%d", is.latest.N)
		if c := is.latest.Claim; c != nil && c.RecipeID != "" {
			holder += fmt.Sprintf(" (recipe %s in %s", c.RecipeID, c.Worktree)
			if c.MulticaIssue != "" {
				holder += ", Multica issue " + c.MulticaIssue
			}
			holder += ")"
		}
		return nil, fmt.Errorf("item %s is held by %s; it must finish, or the manager releases it (bts feature item release)", is.ID, holder)
	}
	if unmet := s.unmetDeps(pi.DependsOn); len(unmet) > 0 {
		return nil, fmt.Errorf("item %s waits for %s to be done", is.ID, strings.Join(unmet, ", "))
	}
	if active, _ := state.GetActiveRecipe(req.Root); active != nil {
		return nil, fmt.Errorf("this worktree already works recipe %s (phase %s); one item per worktree", active.ID, active.Phase)
	}
	// A recipe at finalize is between its spec and its implementation,
	// unless it is a spec-only type, which ends there.
	if fin, _ := state.GetFinalizedRecipe(req.Root); fin != nil && terminalPhase(fin.Type) != "finalize" {
		return nil, fmt.Errorf("this worktree already works recipe %s (finalized, not yet implemented); one item per worktree", fin.ID)
	}

	c := &Claimed{Dir: dir, Feature: f, Item: *pi, Kind: "build", RecipeType: pi.Type, Express: pi.Express, Confirmation: n}
	if is.done != nil {
		if req.Type != "fix" {
			return nil, fmt.Errorf("item %s is done (attempt a%d); a change to it is a fix-forward: --type fix", is.ID, is.done.N)
		}
		c.Kind, c.RecipeType, c.Express = "fix-forward", "fix", false
		c.Amends, c.amendsN = is.done.Claim.RecipeID, is.done.N
		c.restoreFrom = filepath.Join(currentSnapshot(is.done.Dir), "recipe")
	} else if req.Type != "" && req.Type != pi.Type {
		return nil, fmt.Errorf("item %s is a %s item; --type %s is only for a fix-forward on a done item", is.ID, pi.Type, req.Type)
	}

	base := filepath.Join(dir, ItemsDir, pi.ID)
	if err := os.MkdirAll(base, 0755); err != nil {
		return nil, err
	}
	for k := next; ; k++ {
		ad := filepath.Join(base, fmt.Sprintf("a%d", k))
		if err := os.Mkdir(ad, 0755); err != nil {
			if !os.IsExist(err) {
				return nil, err
			}
			// A directory left by a claim that died before claim.json is
			// skipped; any other means another claim got there first.
			if fi, serr := os.Stat(ad); serr == nil && !fileExists(filepath.Join(ad, ClaimFile)) && time.Since(fi.ModTime()) >= claimGrace {
				continue
			}
			return nil, fmt.Errorf("item %s was just claimed by another session (attempt a%d)", pi.ID, k)
		}
		c.Attempt, c.AttemptDir = k, ad
		return c, nil
	}
}

// Abort gives the attempt directory back after a failed create.
func (c *Claimed) Abort() { _ = os.RemoveAll(c.AttemptDir) }

// RestoreAmended copies the done attempt's recipe into the worktree when the
// worktree does not have it, so `--amends` can find it: the worktree that
// built it may be gone.
func (c *Claimed) RestoreAmended(root string) error {
	if c.Amends == "" {
		return nil
	}
	dst := state.RecipeDir(root, c.Amends)
	if _, err := os.Stat(dst); err == nil {
		// Recipe ids repeat across worktrees: the one here must be the
		// done attempt's, not another item's recipe with the same id.
		r, err := state.LoadRecipeState(root, c.Amends)
		if err != nil || r.Feature != c.Feature.ID || r.FeatureItem != c.Item.ID || r.FeatureAttempt != c.amendsN {
			other := "an unbound recipe"
			if err == nil && r.Feature != "" {
				other = fmt.Sprintf("item %s attempt a%d of %s", r.FeatureItem, r.FeatureAttempt, r.Feature)
			}
			return fmt.Errorf("this worktree's %s is %s, not item %s attempt a%d; open the fix-forward in another worktree", c.Amends, other, c.Item.ID, c.amendsN)
		}
		return nil
	}
	if err := copyDir(c.restoreFrom, dst); err != nil {
		return fmt.Errorf("restore %s from the feature store: %w", c.Amends, err)
	}
	return nil
}

// Write records the claim, once the recipe id is known.
func (c *Claimed) Write(root, recipeID, multicaIssue string) error {
	cl := &Claim{
		Schema: Schema, Feature: c.Feature.ID, FeatureDir: c.Dir, Item: c.Item.ID, Attempt: c.Attempt,
		Kind: c.Kind, Amends: c.Amends, RecipeID: recipeID, RecipeType: c.RecipeType, Confirmation: c.Confirmation,
		Worktree: root, StartSHA: headSHA(root), Branch: branch(root), MulticaIssue: multicaIssue,
		BtsVersion: version.GetVersion(), ClaimedAt: now(),
	}
	return state.WriteJSON(filepath.Join(c.AttemptDir, ClaimFile), cl)
}

// Bind sets the recipe's link to this attempt.
func (c *Claimed) Bind(r *state.RecipeState) {
	r.Feature, r.FeatureItem, r.FeatureAttempt, r.FeatureDir = c.Feature.ID, c.Item.ID, c.Attempt, c.Dir
}
