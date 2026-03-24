package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/jlim/claude-forge/internal/metrics"
	"github.com/jlim/claude-forge/internal/state"
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

	agentFile := filepath.Join(state.StatePath(root), "active-agent.json")
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

	_ = metrics.Append(root, &metrics.MetricsEvent{
		Kind:      metrics.KindSubagentStart,
		SessionID: input.SessionID,
		AgentID:   input.AgentID,
	})

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

	agentFile := filepath.Join(state.StatePath(root), "active-agent.json")
	_ = os.Remove(agentFile)

	_ = metrics.Append(root, &metrics.MetricsEvent{
		Kind:      metrics.KindSubagentStop,
		SessionID: input.SessionID,
		AgentID:   input.AgentID,
	})

	return &HookOutput{}, nil
}
