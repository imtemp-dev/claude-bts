package multica

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

// IssueSpec is what CreateIssue sends.
type IssueSpec struct {
	Title       string
	Description string
	Parent      string // parent issue id or key, for a sub-issue
	Stage       int    // ordered barrier group under the parent; 0 for none
	AssigneeID  string // agent or member UUID
	Status      string // e.g. backlog, todo
}

// Issue is the part of an issue bts reads.
type Issue struct {
	ID         string                 `json:"id"`
	Identifier string                 `json:"identifier"`
	Title      string                 `json:"title"`
	Status     string                 `json:"status"`
	Stage      *int                   `json:"stage"`
	AssigneeID string                 `json:"assignee_id"`
	Metadata   map[string]interface{} `json:"metadata"`
}

// Meta returns a metadata value as a string.
func (i *Issue) Meta(key string) string {
	if v, ok := i.Metadata[key]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

func decodeIssue(out []byte) (*Issue, error) {
	var wrapped struct {
		Issue *Issue `json:"issue"`
	}
	if err := json.Unmarshal(out, &wrapped); err == nil && wrapped.Issue != nil && wrapped.Issue.ID != "" {
		return wrapped.Issue, nil
	}
	var is Issue
	if err := json.Unmarshal(out, &is); err != nil || is.ID == "" {
		return nil, fmt.Errorf("unexpected multica output: %s", firstLine(out))
	}
	return &is, nil
}

// CreateIssue creates an issue and returns it.
func CreateIssue(ctx context.Context, s IssueSpec) (*Issue, error) {
	args := []string{"issue", "create", "--title", s.Title, "--description-stdin", "--output", "json"}
	if s.Parent != "" {
		args = append(args, "--parent", s.Parent)
	}
	if s.Stage > 0 {
		args = append(args, "--stage", strconv.Itoa(s.Stage))
	}
	if s.AssigneeID != "" {
		args = append(args, "--assignee-id", s.AssigneeID)
	}
	if s.Status != "" {
		args = append(args, "--status", s.Status)
	}
	out, err := runner(ctx, s.Description, args...)
	if err != nil {
		return nil, err
	}
	return decodeIssue(out)
}

// GetIssue reads one issue.
func GetIssue(ctx context.Context, id string) (*Issue, error) {
	out, err := runner(ctx, "", "issue", "get", id, "--output", "json")
	if err != nil {
		return nil, err
	}
	return decodeIssue(out)
}

// SetStatus moves an issue to a status.  Moving an assigned issue out of
// backlog starts its assignee's run.
func SetStatus(ctx context.Context, id, status string) error {
	return runCLI(ctx, "issue", "status", id, status)
}

// Comment adds a comment to an issue.
func Comment(ctx context.Context, id, content string) error {
	_, err := runner(ctx, content, "issue", "comment", "add", id, "--content-stdin")
	return err
}

// Rerun starts the issue's assignee again.
func Rerun(ctx context.Context, id string) error {
	return runCLI(ctx, "issue", "rerun", id)
}

// ActiveRuns counts the issue's queued or running runs.
func ActiveRuns(ctx context.Context, id string) (int, error) {
	out, err := runner(ctx, "", "issue", "runs", id, "--active", "--output", "json")
	if err != nil {
		return 0, err
	}
	var list []json.RawMessage
	if json.Unmarshal(out, &list) == nil {
		return len(list), nil
	}
	var wrapped struct {
		Runs []json.RawMessage `json:"runs"`
	}
	if err := json.Unmarshal(out, &wrapped); err != nil {
		return 0, fmt.Errorf("unexpected multica output: %s", firstLine(out))
	}
	return len(wrapped.Runs), nil
}

// Children lists a parent issue's sub-issues, across its stages.
func Children(ctx context.Context, parent string) ([]*Issue, error) {
	out, err := runner(ctx, "", "issue", "children", parent, "--output", "json")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Stages []struct {
			Issues []*Issue `json:"issues"`
		} `json:"stages"`
		Unstaged json.RawMessage `json:"unstaged"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("unexpected multica output: %s", firstLine(out))
	}
	var all []*Issue
	for _, s := range resp.Stages {
		all = append(all, s.Issues...)
	}
	var unstaged []*Issue
	if json.Unmarshal(resp.Unstaged, &unstaged) == nil {
		all = append(all, unstaged...)
	} else {
		var u struct {
			Issues []*Issue `json:"issues"`
		}
		if json.Unmarshal(resp.Unstaged, &u) == nil {
			all = append(all, u.Issues...)
		}
	}
	return all, nil
}

// Wakeup is a rule that starts an agent's run when something happens on an
// issue.
type Wakeup struct {
	Issue       string   // the issue the rule watches
	AgentID     string   // the agent to wake
	Events      []string // e.g. task.failed; or empty with a condition
	UntilIssue  string   // condition: this other issue reaches UntilState
	UntilState  string   // done, ended or in_review
	Mode        string   // once or continuous
	MaxFires    int
	ExpiresIn   string // e.g. 720h
	Instruction string
}

// CreateWakeup registers a wakeup rule.
func CreateWakeup(ctx context.Context, w Wakeup) error {
	args := []string{"issue", "wakeup", "create", w.Issue, "--agent-id", w.AgentID, "--instruction", w.Instruction}
	for _, e := range w.Events {
		args = append(args, "--event", e)
	}
	if w.UntilIssue != "" {
		args = append(args, "--until-issue", w.UntilIssue, "--until-issue-state", w.UntilState)
	}
	if w.Mode != "" {
		args = append(args, "--mode", w.Mode)
	}
	if w.MaxFires > 0 {
		args = append(args, "--max-fires", strconv.Itoa(w.MaxFires))
	}
	if w.ExpiresIn != "" {
		args = append(args, "--expires-in", w.ExpiresIn)
	}
	return runCLI(ctx, args...)
}
