package multica

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed skills/*.md
var skillFS embed.FS

// SkillDef is a skill bts ships to a Multica workspace.  Multica writes an
// agent's skills into each run's .claude/skills/, so a run needs no bts
// project to have them.
type SkillDef struct {
	Name        string
	Description string
	file        string
}

// Skills are the workspace skills `bts multica setup` installs.
var Skills = []SkillDef{
	{"bts-feature-manager", "Manage a bts feature on Multica: plan it with the user in Chat, publish it, dispatch its items as sub-issues, and act on their results.", "skills/bts-feature-manager.md"},
	{"bts-feature-worker", "Work one item of a bts feature on its Multica sub-issue: claim it, read its context, run its recipe, finish in review.", "skills/bts-feature-worker.md"},
}

// Content returns the skill's body.
func (s SkillDef) Content() string {
	b, _ := skillFS.ReadFile(s.file)
	return string(b)
}

// Agent is the part of an agent bts reads.
type Agent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func decodeList[T any](out []byte, key string) ([]T, error) {
	var list []T
	if err := json.Unmarshal(out, &list); err == nil {
		return list, nil
	}
	var wrapped map[string]json.RawMessage
	if err := json.Unmarshal(out, &wrapped); err != nil {
		return nil, fmt.Errorf("unexpected multica output: %s", firstLine(out))
	}
	if err := json.Unmarshal(wrapped[key], &list); err != nil {
		return nil, fmt.Errorf("unexpected multica output: %s", firstLine(out))
	}
	return list, nil
}

// ResolveAgent finds an agent by id or name.
func ResolveAgent(ctx context.Context, nameOrID string) (*Agent, error) {
	out, err := runner(ctx, "", "agent", "list", "--output", "json")
	if err != nil {
		return nil, err
	}
	agents, err := decodeList[Agent](out, "agents")
	if err != nil {
		return nil, err
	}
	for _, a := range agents {
		if a.ID == nameOrID || strings.EqualFold(a.Name, nameOrID) {
			return &a, nil
		}
	}
	return nil, fmt.Errorf("no agent %q in this Multica workspace", nameOrID)
}

type skillRow struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// UpsertSkill creates the workspace skill, or updates the one with its name.
func UpsertSkill(ctx context.Context, s SkillDef) (id string, created bool, err error) {
	out, err := runner(ctx, "", "skill", "list", "--output", "json")
	if err != nil {
		return "", false, err
	}
	rows, err := decodeList[skillRow](out, "skills")
	if err != nil {
		return "", false, err
	}
	for _, r := range rows {
		if r.Name == s.Name {
			if _, err := runner(ctx, s.Content(), "skill", "update", r.ID, "--description", s.Description, "--content-stdin"); err != nil {
				return "", false, err
			}
			return r.ID, false, nil
		}
	}
	out, err = runner(ctx, s.Content(), "skill", "create", "--name", s.Name, "--description", s.Description, "--content-stdin", "--output", "json")
	if err != nil {
		return "", false, err
	}
	var row skillRow
	var wrapped struct {
		Skill skillRow `json:"skill"`
	}
	if json.Unmarshal(out, &wrapped) == nil && wrapped.Skill.ID != "" {
		row = wrapped.Skill
	} else if err := json.Unmarshal(out, &row); err != nil || row.ID == "" {
		return "", false, fmt.Errorf("unexpected multica output: %s", firstLine(out))
	}
	return row.ID, true, nil
}

// AttachSkill gives an agent a skill, keeping its others.
func AttachSkill(ctx context.Context, agentID, skillID string) error {
	return runCLI(ctx, "agent", "skills", "add", agentID, "--skill-ids", skillID)
}
