package hook

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/feature"
	"github.com/imtemp-dev/claude-bts/internal/review"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// The user-prompt-submit hook reminds the session of a review page that
// may hold a round it has not read.  The platform notifies a watching
// session 30 to 130 seconds after the user sends a round, and only while
// comment auto-replies are armed (pilot, docs/research/review-pages-design.md);
// a user who comes back to the terminal first should not have to say that
// they sent notes.  bts cannot read the page itself, so the hook reminds
// and the session reads.
type userPromptSubmitHandler struct{}

func NewUserPromptSubmitHandler() Handler { return &userPromptSubmitHandler{} }

func (h *userPromptSubmitHandler) EventType() EventType { return EventUserPromptSubmit }

func (h *userPromptSubmitHandler) Handle(input *HookInput) (*HookOutput, error) {
	if input.IsSubagent() {
		return &HookOutput{}, nil
	}
	root, err := input.ProjectRoot()
	if err != nil {
		return &HookOutput{}, nil
	}
	var lines []string
	recipe, err := state.GetActiveRecipe(root)
	if err == nil && recipe != nil && !isObserver(recipe, input.SessionID) {
		if ctx := reviewReminder(root, recipe); ctx != "" {
			lines = append(lines, ctx)
		}
	}
	// A coordinator settles a feature with no recipe of its own.
	lines = append(lines, featureReviewReminders(root)...)
	if ctx := localSurfaceReminder(root, input.Prompt); ctx != "" {
		lines = append(lines, ctx)
	}
	if len(lines) == 0 {
		return &HookOutput{}, nil
	}
	return &HookOutput{HookSpecificOutput: &HookSpecificOutput{
		HookEventName:     "UserPromptSubmit",
		AdditionalContext: strings.Join(lines, "\n"),
	}}, nil
}

// featureReviewReminders are the lines for the feature review pages this
// project published (`.bts/local/features/<id>/review/`) while the
// feature still waits for the user's confirmation or its page holds open
// work.
func featureReviewReminders(root string) []string {
	dirs, _ := filepath.Glob(filepath.Join(state.LocalPath(root), "features", "*", "review"))
	var out []string
	for _, d := range dirs {
		id := filepath.Base(filepath.Dir(d))
		fdir, err := feature.Find(id)
		if err != nil {
			continue
		}
		f, err := feature.Load(fdir)
		if err != nil || f.Closed != nil {
			continue
		}
		sub, err := feature.ReviewSubject(root, fdir)
		if err != nil {
			continue
		}
		s, err := review.Load(sub)
		if err != nil || s.URL == "" {
			continue
		}
		open, err := review.OpenWork(sub, "")
		if err != nil || (!feature.NeedsConfirmation(fdir, f) && len(open) == 0) {
			continue
		}
		msg := fmt.Sprintf("[bts] Feature %s has a review page: %s. If the user sent notes, pressed Update plan "+
			"or confirmed there, %s and follow /bts-review-page § Feature before answering.", f.ID, s.URL,
			readRounds(s, "--feature "+f.ID))
		if len(open) > 0 {
			msg += fmt.Sprintf(" %d item(s) are open on the page; `bts review status --feature %s` lists them.", len(open), f.ID)
		}
		out = append(out, msg)
	}
	return out
}

// reviewReminder is the line added to the prompt while a review is under
// way on a page: the recipe is still in discovery or scoping, or the page
// holds open work.  "" when there is nothing to remind of.
func reviewReminder(root string, recipe *state.RecipeState) string {
	sub := review.RecipeSubject(root, recipe.ID)
	s, err := review.Load(sub)
	if err != nil || s.URL == "" {
		return ""
	}
	open, err := review.OpenWork(sub, "")
	if err != nil {
		return ""
	}
	if recipe.Phase != "discovery" && recipe.Phase != "scoping" && len(open) == 0 {
		return ""
	}
	msg := fmt.Sprintf("[bts] Recipe %s has a review page: %s. If the user sent notes, pressed Update plan "+
		"or confirmed there, %s and follow /bts-review-page § Round before answering.", recipe.ID, s.URL,
		readRounds(s, recipe.ID))
	if len(open) > 0 {
		msg += fmt.Sprintf(" %d item(s) are open on the page; `bts review status %s` lists them.", len(open), recipe.ID)
	}
	return msg
}

// readRounds says how the session reads the rounds the user sent: a local
// page's data is on disk, a claude.ai page's is read with ArtifactData.
func readRounds(s *review.State, arg string) string {
	if s.Surface == review.SurfaceLocal {
		return fmt.Sprintf("run `bts review import %s`", arg)
	}
	return "read the page's rounds whose status is submitted (ArtifactData query)"
}
