package feature

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/multica"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// MulticaLink is where a published feature lives on Multica.  Only the
// manager writes it, inside feature.json.
type MulticaLink struct {
	Parent    string                  `json:"parent_issue"`
	ParentKey string                  `json:"parent_key,omitempty"`
	Manager   string                  `json:"manager_agent"`
	Worker    string                  `json:"worker_agent"`
	Items     map[string]*MulticaItem `json:"items,omitempty"`
}

// MulticaItem is one item's sub-issue.
type MulticaItem struct {
	Issue string `json:"issue"`
	Key   string `json:"key,omitempty"`
	Stage int    `json:"stage"`
	At    string `json:"at"`
}

// MulticaConfig names the agents a store's features use, written by
// `bts multica setup` into <store>/multica.json.
type MulticaConfig struct {
	Manager string `json:"manager_agent"`
	Worker  string `json:"worker_agent"`
}

const multicaConfigFile = "multica.json"

// LoadMulticaConfig reads the store's agent configuration.
func LoadMulticaConfig() (*MulticaConfig, error) {
	store, err := StoreRoot()
	if err != nil {
		return nil, err
	}
	var c MulticaConfig
	if err := state.ReadJSON(filepath.Join(store, multicaConfigFile), &c); err != nil {
		return nil, fmt.Errorf("no Multica agents configured for the feature store: run bts multica setup")
	}
	return &c, nil
}

// SaveMulticaConfig writes the store's agent configuration.
func SaveMulticaConfig(c *MulticaConfig) error {
	store, err := StoreRoot()
	if err != nil {
		return err
	}
	return state.WriteJSON(filepath.Join(store, multicaConfigFile), c)
}

// Multica operations hold the feature lock while they talk to the server.
const (
	multicaLockWait = 90 * time.Second
	multicaBudget   = 60 * time.Second
)

// Wakeup instructions the dispatcher registers for the manager.
const (
	failureInstruction = "A run on this item failed or was cancelled. Follow the bts-feature-manager skill: read bts feature status, look at the run (multica issue runs, run-messages), then rerun, release and dispatch again, or ask the user on the parent issue."
	reviewInstruction  = "An item reached in_review. Follow the bts-feature-manager skill: reconcile the feature, check the item's results against the contracts, then run bts feature dispatch."
)

func parentBrief(f *Feature, dir string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "bts feature **%s**: %s\n\n", f.ID, f.Title)
	b.WriteString("Managed by the bts-feature-manager skill.  Every run starts from the store, not from this conversation:\n\n")
	fmt.Fprintf(&b, "- `bts feature status %s` — the plan and each item's state\n", f.ID)
	fmt.Fprintf(&b, "- `bts feature reconcile %s` — what changed since the last look\n", f.ID)
	fmt.Fprintf(&b, "- `bts feature dispatch %s` — start ready items and keep the sub-issues in step\n\n", f.ID)
	fmt.Fprintf(&b, "Store: `%s`.  Repository: %s, base `%s`.\n", dir, f.Repo.URL, f.BaseBranch)
	if f.Ticket != "" {
		fmt.Fprintf(&b, "Ticket: %s.\n", f.Ticket)
	}
	return b.String()
}

func workerBrief(f *Feature, p PlanItem, stage int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "bts feature **%s**, item **%s**: %s (%s", f.ID, p.ID, p.Title, p.Type)
	if p.Express {
		b.WriteString(", express")
	}
	fmt.Fprintf(&b, "), stage %d.\n\n", stage)
	b.WriteString("Work it with the bts-feature-worker skill.  The steps that start it:\n\n")
	repo := f.Repo.URL
	if repo == "" {
		repo = "<the repository>"
	}
	fmt.Fprintf(&b, "1. `multica repo checkout %s --ref %s`\n", repo, f.BaseBranch)
	b.WriteString("2. From this task's workdir: `bts init --multica`\n")
	fmt.Fprintf(&b, "3. In the repository: `bts recipe create --feature %s --item %s`, then `bts feature context %s %s`\n", f.ID, p.ID, f.ID, p.ID)
	if len(p.DependsOn) > 0 {
		fmt.Fprintf(&b, "\nDepends on %s, whose results `bts feature context` serves.\n", strings.Join(p.DependsOn, ", "))
	}
	return b.String()
}

// Publish creates the feature's parent issue on Multica, assigned to the
// manager agent, and records it.  It is created in backlog and moved to todo
// only once recorded, so the manager's first run finds the link.  A feature
// already published returns its link.
func Publish(dir, manager, worker string) (*MulticaLink, bool, error) {
	f, err := Load(dir)
	if err != nil {
		return nil, false, err
	}
	if f.Multica != nil && f.Multica.Parent != "" {
		return f.Multica, true, nil
	}
	if n, _, err := Confirmed(dir, f); err != nil || n == 0 {
		if err == nil {
			err = fmt.Errorf("feature %s is not confirmed: the user confirms the plan before it is published", f.ID)
		}
		return nil, false, err
	}
	if manager == "" || worker == "" {
		return nil, false, fmt.Errorf("publish needs the manager and worker agents (bts multica setup)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), multicaBudget)
	defer cancel()
	var link *MulticaLink
	_, err = MutateWait(dir, "publish", multicaLockWait, func(f *Feature) (string, error) {
		if f.Multica != nil && f.Multica.Parent != "" {
			link = f.Multica
			return "", ErrNoChange
		}
		parent, err := multica.CreateIssue(ctx, multica.IssueSpec{
			Title: fmt.Sprintf("%s: %s", f.ID, f.Title), Description: parentBrief(f, dir),
			AssigneeID: manager, Status: "backlog",
		})
		if err != nil {
			return "", err
		}
		if err := multica.SetMetadata(ctx, parent.ID, "bts.feature", f.ID); err != nil {
			return "", err
		}
		link = &MulticaLink{Parent: parent.ID, ParentKey: parent.Identifier, Manager: manager, Worker: worker, Items: map[string]*MulticaItem{}}
		f.Multica = link
		return "parent " + parent.Identifier, nil
	})
	if err != nil {
		return nil, false, err
	}
	if err := multica.SetStatus(ctx, link.Parent, "todo"); err != nil {
		return link, false, fmt.Errorf("published as %s, but starting the manager failed: %w", link.ParentKey, err)
	}
	return link, false, nil
}

// Action is one thing Dispatch did.
type Action struct {
	Item  string
	Issue string
	What  string
}

var closedStatuses = map[string]bool{"done": true, "cancelled": true}

// Dispatch keeps the feature's sub-issues in step with the confirmed plan:
// it creates a sub-issue in backlog for each confirmed item that has none
// (with its stage, and wakeups for the manager on failure and on review),
// starts the ready ones, restarts a claimable one that has no run, cancels
// skipped or deferred ones, and moves the parent to in_review when every
// item is settled.  Running it again changes nothing that is already in
// step.
func Dispatch(dir string) ([]Action, error) {
	ctx, cancel := context.WithTimeout(context.Background(), multicaBudget)
	defer cancel()
	var actions []Action
	_, err := MutateWait(dir, "dispatch", multicaLockWait, func(f *Feature) (string, error) {
		link := f.Multica
		if link == nil || link.Parent == "" {
			return "", fmt.Errorf("feature %s is not published to Multica (bts feature publish)", f.ID)
		}
		if f.Closed != nil {
			return "", fmt.Errorf("feature %s is %s", f.ID, f.Closed.Kind)
		}
		if link.Items == nil {
			link.Items = map[string]*MulticaItem{}
		}
		snap, err := ReadConfirmed(dir, f)
		if err != nil {
			return "", err
		}
		if snap == nil {
			return "", fmt.Errorf("feature %s is not confirmed", f.ID)
		}
		st, err := stages(snap.Plan)
		if err != nil {
			return "", err
		}
		ready, s, err := Next(dir, f)
		if err != nil {
			return "", err
		}
		isReady := map[string]bool{}
		for _, r := range ready {
			isReady[r.ID] = true
		}

		children, err := multica.Children(ctx, link.Parent)
		if err != nil {
			return "", err
		}
		byItem := map[string]*multica.Issue{}
		for _, c := range children {
			if c.Meta("bts.feature") == f.ID && c.Meta("bts.item") != "" {
				byItem[c.Meta("bts.item")] = c
			}
		}

		changed := false
		for _, p := range snap.Plan {
			is := s.item(p.ID)
			if is == nil || is.Removed {
				continue
			}
			child := byItem[p.ID]
			settledByDecision := is.State == StateSkipped || is.State == StateDeferred
			if child == nil && settledByDecision {
				continue
			}
			if child == nil {
				c, err := multica.CreateIssue(ctx, multica.IssueSpec{
					Title: fmt.Sprintf("%s: %s", p.ID, p.Title), Description: workerBrief(f, p, st[p.ID]),
					Parent: link.Parent, Stage: st[p.ID], AssigneeID: link.Worker, Status: "backlog",
				})
				if err != nil {
					return "", fmt.Errorf("create the sub-issue for %s: %w", p.ID, err)
				}
				for k, v := range map[string]string{"bts.feature": f.ID, "bts.item": p.ID} {
					if err := multica.SetMetadata(ctx, c.ID, k, v); err != nil {
						return "", err
					}
				}
				if err := multica.CreateWakeup(ctx, multica.Wakeup{
					Issue: c.ID, AgentID: link.Manager, Events: []string{"task.failed", "task.cancelled"},
					Mode: "continuous", MaxFires: 20, ExpiresIn: "720h", Instruction: failureInstruction,
				}); err != nil {
					return "", fmt.Errorf("register the failure wakeup for %s: %w", p.ID, err)
				}
				if err := multica.CreateWakeup(ctx, multica.Wakeup{
					Issue: link.Parent, AgentID: link.Manager, UntilIssue: c.ID, UntilState: "in_review",
					Instruction: reviewInstruction,
				}); err != nil {
					return "", fmt.Errorf("register the review wakeup for %s: %w", p.ID, err)
				}
				link.Items[p.ID] = &MulticaItem{Issue: c.ID, Key: c.Identifier, Stage: st[p.ID], At: now()}
				changed = true
				c.Status = "backlog"
				child = c
				actions = append(actions, Action{p.ID, c.Identifier, fmt.Sprintf("created in backlog, stage %d", st[p.ID])})
			} else if link.Items[p.ID] == nil {
				link.Items[p.ID] = &MulticaItem{Issue: child.ID, Key: child.Identifier, Stage: st[p.ID], At: now()}
				changed = true
			}

			switch {
			case settledByDecision:
				if !closedStatuses[child.Status] {
					if err := multica.SetStatus(ctx, child.ID, "cancelled"); err != nil {
						return "", err
					}
					d := f.Item(p.ID).Decision
					_ = multica.Comment(ctx, child.ID, fmt.Sprintf("Not worked: the user decided to %s it (%q).", d.Kind, d.Answer))
					actions = append(actions, Action{p.ID, child.Identifier, "cancelled (" + d.Kind + ")"})
				}
			case isReady[p.ID] && child.Status == "backlog":
				if err := multica.SetStatus(ctx, child.ID, "todo"); err != nil {
					return "", err
				}
				actions = append(actions, Action{p.ID, child.Identifier, "started"})
			case isReady[p.ID] && is.State == StateClaimable:
				if n, err := multica.ActiveRuns(ctx, child.ID); err == nil && n == 0 {
					if child.Status != "todo" {
						if err := multica.SetStatus(ctx, child.ID, "todo"); err != nil {
							return "", err
						}
					}
					if err := multica.Rerun(ctx, child.ID); err != nil {
						return "", err
					}
					actions = append(actions, Action{p.ID, child.Identifier, "restarted"})
				}
			}
		}

		if s.State == FeatureReady {
			if parent, err := multica.GetIssue(ctx, link.Parent); err == nil && parent.Status != "in_review" && !closedStatuses[parent.Status] {
				if err := multica.SetStatus(ctx, link.Parent, "in_review"); err != nil {
					return "", err
				}
				actions = append(actions, Action{"", link.ParentKey, "every item settled: parent in review"})
			}
		}
		if !changed && len(actions) == 0 {
			return "", ErrNoChange
		}
		var what []string
		for _, a := range actions {
			what = append(what, strings.TrimSpace(a.Item+" "+a.What))
		}
		return strings.Join(what, "; "), nil
	})
	return actions, err
}
