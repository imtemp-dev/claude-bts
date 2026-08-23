package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/imtemp-dev/jig/internal/metrics"
	"github.com/imtemp-dev/jig/internal/state"
)

type subagentStartHandler struct{}

func NewSubagentStartHandler() Handler {
	return &subagentStartHandler{}
}

func (h *subagentStartHandler) EventType() EventType {
	return EventSubagentStart
}

func (h *subagentStartHandler) Handle(input *HookInput) (*HookOutput, error) {
	root, err := state.FindRoot(input.CWD)
	if err != nil {
		return &HookOutput{}, nil
	}

	agentFile := filepath.Join(state.LocalPath(root), "active-agent.json")
	data := map[string]string{
		"agent_id":   input.AgentID,
		"started_at": time.Now().UTC().Format(time.RFC3339),
	}
	bytes, err := json.Marshal(data)
	if err != nil {
		return &HookOutput{}, nil
	}
	_ = os.MkdirAll(filepath.Dir(agentFile), 0755)
	_ = os.WriteFile(agentFile, bytes, 0644)

	_ = metrics.Append(root, subagentEvent(root, metrics.KindSubagentStart, input))

	return &HookOutput{}, nil
}

type subagentStopHandler struct{}

func NewSubagentStopHandler() Handler {
	return &subagentStopHandler{}
}

func (h *subagentStopHandler) EventType() EventType {
	return EventSubagentStop
}

func (h *subagentStopHandler) Handle(input *HookInput) (*HookOutput, error) {
	root, err := state.FindRoot(input.CWD)
	if err != nil {
		return &HookOutput{}, nil
	}

	agentFile := filepath.Join(state.LocalPath(root), "active-agent.json")
	_ = os.Remove(agentFile)

	_ = metrics.Append(root, subagentEvent(root, metrics.KindSubagentStop, input))

	return &HookOutput{}, nil
}

// subagentEvent builds a subagent metric carrying the active recipe, the
// way every other hook already does.
//
// Without RecipeID, metrics.Append writes the event to the global log
// only — and metrics.SubagentActivitySince reads the per-recipe log. The
// two never met, so VerifyLogEntry.AgentEvidence returned "none" on every
// round of every recipe, in every project, since the field was added. A
// measured recipe logged 7,632 subagent events globally and 0 under its
// recipe across 34 verify rounds, while genuinely self-verifying in two
// of them. The one mechanism meant to witness fork isolation could not
// fire, and its constant "none" was indistinguishable from a harness that
// emits no subagent events at all.
func subagentEvent(root string, kind metrics.EventKind, input *HookInput) *metrics.MetricsEvent {
	ev := &metrics.MetricsEvent{
		Kind:      kind,
		SessionID: input.SessionID,
		AgentID:   input.AgentID,
	}
	if recipe, _ := state.GetActiveRecipe(root); recipe != nil {
		ev.RecipeID = recipe.ID
		ev.Phase = recipe.Phase
	}
	return ev
}
