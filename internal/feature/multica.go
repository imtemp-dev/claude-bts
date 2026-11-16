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

// MulticaItem is one item's sub-issue, and what the manager decided and saw
// about its branch.
type MulticaItem struct {
	Issue      string       `json:"issue"`
	Key        string       `json:"key,omitempty"`
	Stage      int          `json:"stage"`
	At         string       `json:"at"`
	Base       string       `json:"base,omitempty"`          // the branch the item started from
	BaseItem   string       `json:"base_item,omitempty"`     // the unmerged item it is stacked on
	Merged     *MergeRecord `json:"merged,omitempty"`        // its pull request merged
	RestackFor string       `json:"restack_for,omitempty"`   // the merged item a restack was asked for
	Setup      []string     `json:"setup_pending,omitempty"` // sub-issue setup steps still to do, in order
}

// MergeRecord is a merged pull request.
type MergeRecord struct {
	SHA  string `json:"sha"`
	At   string `json:"at"`
	Into string `json:"into,omitempty"` // the branch it merged into; "" (recorded before bts kept it) is the base
}

// Steps that finish a new sub-issue, recorded as pending when it is created
// and done in order, so a dispatch that fails between them never creates
// the issue again or registers a wakeup twice.
const (
	setupMetadata      = "metadata"
	setupFailureWakeup = "failure_wakeup"
	setupReviewWakeup  = "review_wakeup"
)

// reachedBase reports whether item id's work is on the feature's base
// branch: its pull request merged into the base, or into the branch of the
// item it was stacked on, once that item's work reached the base.
func reachedBase(f *Feature, id string) bool {
	if f.Multica == nil {
		return false
	}
	for seen := map[string]bool{}; id != "" && !seen[id]; {
		seen[id] = true
		mi := f.Multica.Items[id]
		if mi == nil || mi.Merged == nil {
			return false
		}
		if mi.Merged.Into == "" || mi.Merged.Into == f.BaseBranch {
			return true
		}
		if mi.BaseItem == "" || mi.Merged.Into != mi.Base {
			return false
		}
		id = mi.BaseItem
	}
	return false
}

// MulticaConfig names the agents a store's features use, and the branch
// prefix for item branches, written by `bts multica setup` into
// <store>/multica.json.
type MulticaConfig struct {
	Manager      string `json:"manager_agent"`
	Worker       string `json:"worker_agent"`
	BranchPrefix string `json:"branch_prefix,omitempty"`
}

// MaxStack is how many unmerged items deep a stack may go.
const MaxStack = 2

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

func workerBrief(f *Feature, p PlanItem, stage int, base, baseItem string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "bts feature **%s**, item **%s**: %s (%s", f.ID, p.ID, p.Title, p.Type)
	if p.Express {
		b.WriteString(", express")
	}
	fmt.Fprintf(&b, "), stage %d.\n\n", stage)
	if base == "" {
		b.WriteString("Waiting in backlog: the manager starts it, and sets its base branch here, when its dependencies allow.\n")
		return b.String()
	}
	b.WriteString("Work it with the bts-feature-worker skill.  The steps that start it:\n\n")
	repo := f.Repo.URL
	if repo == "" {
		repo = "<the repository>"
	}
	fmt.Fprintf(&b, "1. `multica repo checkout %s --ref %s`", repo, base)
	if baseItem != "" {
		fmt.Fprintf(&b, " (stacked on %s, which has not merged yet)", baseItem)
	}
	b.WriteString("\n")
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

// Action is one thing Dispatch did, or, with Note, one thing it is waiting
// for.
type Action struct {
	Item  string
	Issue string
	What  string
	Note  bool
}

// chooseBase decides where a ready item starts: the feature's base branch
// when every dependency was skipped or reached it; the branch of its one
// unmerged dependency, stacked, when that dependency has a draft pull
// request and the stack stays within MaxStack; otherwise it waits, and wait
// says why.  A dependency that merged into some other branch waits too.
func chooseBase(dir string, f *Feature, p PlanItem, s *Status) (base, baseItem, wait string) {
	link := f.Multica
	var unmerged []string
	for _, d := range p.DependsOn {
		if ds := s.item(d); ds != nil && ds.State == StateSkipped {
			continue // met, as Next counts it
		}
		if reachedBase(f, d) {
			continue
		}
		if mi := link.Items[d]; mi != nil && mi.Merged != nil {
			return "", "", fmt.Sprintf("%s merged into %s, not %s; waits until that work reaches %s", d, mi.Merged.Into, f.BaseBranch, f.BaseBranch)
		}
		unmerged = append(unmerged, d)
	}
	switch len(unmerged) {
	case 0:
		return f.BaseBranch, "", ""
	case 1:
	default:
		return "", "", fmt.Sprintf("%s have not merged; an item stacks on one unmerged item at most", strings.Join(unmerged, ", "))
	}
	d := unmerged[0]
	it := f.Item(d)
	if it == nil {
		return "", "", d + " is not in the plan"
	}
	pr, _ := itemPR(dir, it)
	if pr == nil {
		return "", "", d + " has no draft pull request yet"
	}
	if depth := stackDepth(f, d) + 1; depth > MaxStack {
		return "", "", fmt.Sprintf("stacking on %s would make %d unmerged levels (at most %d); waits for a merge", d, depth, MaxStack)
	}
	return pr.Branch, d, ""
}

// stackDepth is how many items not yet on the base branch an item and those
// under it make.
func stackDepth(f *Feature, id string) int {
	depth := 0
	for seen := map[string]bool{}; id != "" && !seen[id]; {
		seen[id] = true
		mi := f.Multica.Items[id]
		if mi == nil || reachedBase(f, id) {
			break
		}
		depth++
		id = mi.BaseItem
	}
	return depth
}

// finishSetup does a sub-issue's pending setup steps in order, dropping each
// one done.  It reports whether it did any.
func finishSetup(ctx context.Context, f *Feature, link *MulticaLink, mi *MulticaItem, item string) (bool, error) {
	did := false
	for len(mi.Setup) > 0 {
		var err error
		switch mi.Setup[0] {
		case setupMetadata:
			if err = multica.SetMetadata(ctx, mi.Issue, "bts.feature", f.ID); err == nil {
				err = multica.SetMetadata(ctx, mi.Issue, "bts.item", item)
			}
		case setupFailureWakeup:
			if err = multica.CreateWakeup(ctx, multica.Wakeup{
				Issue: mi.Issue, AgentID: link.Manager, Events: []string{"task.failed", "task.cancelled"},
				Mode: "continuous", MaxFires: 20, ExpiresIn: "720h", Instruction: failureInstruction,
			}); err != nil {
				err = fmt.Errorf("register the failure wakeup for %s: %w", item, err)
			}
		case setupReviewWakeup:
			if err = multica.CreateWakeup(ctx, multica.Wakeup{
				Issue: link.Parent, AgentID: link.Manager, UntilIssue: mi.Issue, UntilState: "in_review",
				Instruction: reviewInstruction,
			}); err != nil {
				err = fmt.Errorf("register the review wakeup for %s: %w", item, err)
			}
		}
		if err != nil {
			return did, err
		}
		mi.Setup = mi.Setup[1:]
		did = true
	}
	mi.Setup = nil
	return did, nil
}

// prRead is one pull request state, read before dispatch takes the lock.
type prRead struct {
	URL   string
	State *PRState
	Err   error
}

// readPRStates asks GitHub for the state of every recorded item's pull
// request that has not merged.  It runs before dispatch takes the feature
// lock, so a slow gh holds no other feature command.
func readPRStates(dir string) map[string]prRead {
	f, err := Load(dir)
	if err != nil || f.Multica == nil {
		return nil
	}
	out := map[string]prRead{}
	for _, id := range sortedKeys(f.Multica.Items) {
		it := f.Item(id)
		if it == nil || f.Multica.Items[id].Merged != nil {
			continue
		}
		pr, _ := itemPR(dir, it)
		if pr == nil {
			continue
		}
		st, err := prState(pr.URL)
		out[id] = prRead{URL: pr.URL, State: st, Err: err}
	}
	return out
}

var closedStatuses = map[string]bool{"done": true, "cancelled": true}

// Dispatch keeps the feature's sub-issues in step with the confirmed plan:
// it creates a sub-issue in backlog for each confirmed item that has none
// (with its stage, and wakeups for the manager on failure and on review),
// starts the ready ones, restarts a claimable one that has no run, cancels
// skipped or deferred ones, and moves the parent to in_review when every
// item is settled.  Running it again changes nothing that is already in
// step.  Pull request states are read before it takes the feature lock; a
// failure part way saves what it did on Multica before the failure.
func Dispatch(dir string) ([]Action, error) {
	ctx, cancel := context.WithTimeout(context.Background(), multicaBudget)
	defer cancel()
	prs := readPRStates(dir)
	var actions []Action
	_, err := MutateWait(dir, "dispatch", multicaLockWait, func(f *Feature) (string, error) {
		changed := false
		summary := func() string {
			var what []string
			for _, a := range actions {
				if !a.Note {
					what = append(what, strings.TrimSpace(a.Item+" "+a.What))
				}
			}
			return strings.Join(what, "; ")
		}
		// fail keeps what this dispatch already did with the error.
		fail := func(err error) (string, error) {
			if changed {
				return summary(), saveAnd(err)
			}
			return "", err
		}

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
		byID := map[string]*multica.Issue{}
		for _, c := range children {
			byID[c.ID] = c
			if c.Meta("bts.feature") == f.ID && c.Meta("bts.item") != "" {
				byItem[c.Meta("bts.item")] = c
			}
		}

		// Merges first, so a merge seen now frees what waited for it in
		// this same dispatch.
		for _, id := range sortedKeys(link.Items) {
			mi := link.Items[id]
			it := f.Item(id)
			if it == nil || mi.Merged != nil {
				continue
			}
			pr, _ := itemPR(dir, it)
			rd, ok := prs[id]
			if pr == nil || !ok || rd.URL != pr.URL {
				continue // opened since the read: the next dispatch reads it
			}
			if rd.Err != nil {
				actions = append(actions, Action{Item: id, Issue: mi.Key, What: "could not read its pull request's state: " + rd.Err.Error(), Note: true})
				continue
			}
			if rd.State.State != "MERGED" {
				continue
			}
			ps := rd.State
			mi.Merged = &MergeRecord{SHA: ps.MergeCommit.OID, At: ps.MergedAt, Into: ps.BaseRefName}
			changed = true
			what := "merged " + short(ps.MergeCommit.OID)
			if ps.BaseRefName != "" && ps.BaseRefName != f.BaseBranch {
				what += " into " + ps.BaseRefName
			}
			actions = append(actions, Action{Item: id, Issue: mi.Key, What: what})
		}

		for _, p := range snap.Plan {
			is := s.item(p.ID)
			if is == nil || is.Removed {
				continue
			}
			child := byItem[p.ID]
			if child == nil && link.Items[p.ID] != nil {
				child = byID[link.Items[p.ID].Issue] // recorded, its metadata not set yet
			}
			settledByDecision := is.State == StateSkipped || is.State == StateDeferred
			if child == nil && settledByDecision {
				continue
			}
			if child == nil {
				c, err := multica.CreateIssue(ctx, multica.IssueSpec{
					Title: fmt.Sprintf("%s: %s", p.ID, p.Title), Description: workerBrief(f, p, st[p.ID], "", ""),
					Parent: link.Parent, Stage: st[p.ID], AssigneeID: link.Worker, Status: "backlog",
				})
				if err != nil {
					return fail(fmt.Errorf("create the sub-issue for %s: %w", p.ID, err))
				}
				// Recorded at once with the setup still to do, so a failure
				// below never makes the next dispatch create it again.
				link.Items[p.ID] = &MulticaItem{Issue: c.ID, Key: c.Identifier, Stage: st[p.ID], At: now(),
					Setup: []string{setupMetadata, setupFailureWakeup, setupReviewWakeup}}
				changed = true
				c.Status = "backlog"
				child = c
				actions = append(actions, Action{Item: p.ID, Issue: c.Identifier, What: fmt.Sprintf("created in backlog, stage %d", st[p.ID])})
			} else if link.Items[p.ID] == nil {
				link.Items[p.ID] = &MulticaItem{Issue: child.ID, Key: child.Identifier, Stage: st[p.ID], At: now()}
				changed = true
			}
			mi := link.Items[p.ID]
			if did, err := finishSetup(ctx, f, link, mi, p.ID); did || err != nil {
				changed = changed || did
				if err != nil {
					return fail(err)
				}
			}

			switch {
			case settledByDecision:
				if !closedStatuses[child.Status] {
					if err := multica.SetStatus(ctx, child.ID, "cancelled"); err != nil {
						return fail(err)
					}
					d := f.Item(p.ID).Decision
					_ = multica.Comment(ctx, child.ID, fmt.Sprintf("Not worked: the user decided to %s it (%q).", d.Kind, d.Answer))
					actions = append(actions, Action{Item: p.ID, Issue: child.Identifier, What: "cancelled (" + d.Kind + ")"})
				}
			case isReady[p.ID] && child.Status == "backlog":
				base, baseItem, wait := chooseBase(dir, f, p, s)
				if wait != "" {
					actions = append(actions, Action{Item: p.ID, Issue: child.Identifier, What: "waits: " + wait, Note: true})
					continue
				}
				// Recorded before the calls: once the issue is todo, a later
				// dispatch never sets the base again.
				mi.Base, mi.BaseItem = base, baseItem
				changed = true
				if err := multica.UpdateDescription(ctx, child.ID, workerBrief(f, p, st[p.ID], base, baseItem)); err != nil {
					return fail(err)
				}
				if err := multica.SetStatus(ctx, child.ID, "todo"); err != nil {
					return fail(err)
				}
				what := "started on " + base
				if baseItem != "" {
					what += " (stacked on " + baseItem + ")"
				}
				actions = append(actions, Action{Item: p.ID, Issue: child.Identifier, What: what})
			case isReady[p.ID] && is.State == StateClaimable:
				if n, err := multica.ActiveRuns(ctx, child.ID); err == nil && n == 0 {
					if child.Status != "todo" {
						if err := multica.SetStatus(ctx, child.ID, "todo"); err != nil {
							return fail(err)
						}
					}
					if err := multica.Rerun(ctx, child.ID); err != nil {
						return fail(err)
					}
					actions = append(actions, Action{Item: p.ID, Issue: child.Identifier, What: "restarted"})
				}
			}
		}

		// An item stacked on one whose work reached the base branch is asked
		// to restack onto it.
		for _, id := range sortedKeys(link.Items) {
			mi := link.Items[id]
			if mi.BaseItem == "" || mi.Merged != nil || mi.RestackFor == mi.BaseItem {
				continue
			}
			under := link.Items[mi.BaseItem]
			it := f.Item(id)
			if under == nil || under.Merged == nil || it == nil {
				continue
			}
			if pr, _ := itemPR(dir, it); pr == nil || pr.Base == f.BaseBranch {
				continue
			}
			if !reachedBase(f, mi.BaseItem) {
				actions = append(actions, Action{Item: id, Issue: mi.Key, Note: true,
					What: fmt.Sprintf("restack waits: %s merged into %s, not %s", mi.BaseItem, under.Merged.Into, f.BaseBranch)})
				continue
			}
			msg := fmt.Sprintf("[@worker](mention://agent/%s) %s merged (%s). Restack this item onto %s: in the worktree, `bts feature restack %s %s`, then rerun the recipe's tests and report them.",
				link.Worker, mi.BaseItem, short(under.Merged.SHA), f.BaseBranch, f.ID, id)
			if err := multica.Comment(ctx, mi.Issue, msg); err != nil {
				return fail(err)
			}
			mi.RestackFor = mi.BaseItem
			changed = true
			actions = append(actions, Action{Item: id, Issue: mi.Key, What: "asked to restack onto " + f.BaseBranch})
		}

		// The parent goes to review once every item is settled and every
		// done item has its draft pull request.
		allPRs := true
		for _, is := range s.Items {
			if is.State == StateDone {
				if pr, _ := itemPR(dir, f.Item(is.ID)); pr == nil {
					allPRs = false
				}
			}
		}
		if s.State == FeatureReady && allPRs {
			if parent, err := multica.GetIssue(ctx, link.Parent); err == nil && parent.Status != "in_review" && !closedStatuses[parent.Status] {
				if err := multica.SetStatus(ctx, link.Parent, "in_review"); err != nil {
					return fail(err)
				}
				actions = append(actions, Action{Issue: link.ParentKey, What: "every item settled with its draft pull request: parent in review"})
			}
		}
		detail := summary()
		if !changed && detail == "" {
			return "", ErrNoChange
		}
		return detail, nil
	})
	return actions, err
}
