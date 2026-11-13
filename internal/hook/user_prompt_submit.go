package hook

import (
	"fmt"

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
	recipe, err := state.GetActiveRecipe(root)
	if err != nil || recipe == nil || isObserver(recipe, input.SessionID) {
		return &HookOutput{}, nil
	}
	ctx := reviewReminder(root, recipe)
	if ctx == "" {
		return &HookOutput{}, nil
	}
	return &HookOutput{HookSpecificOutput: &HookSpecificOutput{
		HookEventName:     "UserPromptSubmit",
		AdditionalContext: ctx,
	}}, nil
}

// reviewReminder is the line added to the prompt while a review is under
// way on a page: the recipe is still in discovery or scoping, or the page
// holds open work.  "" when there is nothing to remind of.
func reviewReminder(root string, recipe *state.RecipeState) string {
	s, err := review.Load(root, recipe.ID)
	if err != nil || s.URL == "" {
		return ""
	}
	open, err := review.OpenWork(root, recipe.ID, "")
	if err != nil {
		return ""
	}
	if recipe.Phase != "discovery" && recipe.Phase != "scoping" && len(open) == 0 {
		return ""
	}
	msg := fmt.Sprintf("[bts] Recipe %s has a review page: %s. If the user sent notes, pressed Update plan "+
		"or confirmed there, read the page's rounds whose status is submitted (ArtifactData query) and follow "+
		"/bts-review-page § Round before answering.", recipe.ID, s.URL)
	if len(open) > 0 {
		msg += fmt.Sprintf(" %d item(s) are open on the page; `bts review status %s` lists them.", len(open), recipe.ID)
	}
	return msg
}
