package feature

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/orca"
)

// A feature can run on Orca instead of Multica: the user's own Claude
// session, in an Orca terminal, coordinates it, and each item runs as an
// Orca orchestration worker (an interactive Claude Code session) in a
// child worktree of the coordinator's (docs/research/orca-orchestration-
// design.md, Delegate).  bts keeps deciding what is ready and where an
// item starts; Orca keeps the sessions, worktrees and mail.

// OrcaLink is where a feature runs on Orca: the orchestration Run that the
// coordinator's session holds, and one worker per item.
type OrcaLink struct {
	Run   string               `json:"run,omitempty"`
	Items map[string]*OrcaItem `json:"items,omitempty"`
}

// OrcaItem is the worker an item runs in, and what the dispatcher decided
// and saw about its branch.
type OrcaItem struct {
	Dispatch  string `json:"dispatch"`
	Task      string `json:"task,omitempty"`
	Workspace string `json:"workspace,omitempty"` // Orca's id of the worker's child worktree
	Starts    int    `json:"starts,omitempty"`    // workers started for the item, a restart included
	At        string `json:"at"`
	ItemBranch
}

// OrcaDispatchOptions says how DispatchOrca starts workers.
type OrcaDispatchOptions struct {
	Dir    string // the coordinator's project directory, where the orca CLI runs
	Agent  string // Orca agent id for workers; "" is claude
	Model  string // "" for the person's own settings
	DryRun bool   // decide and report; start nothing, send nothing, save nothing
}

//go:embed orca_worker.md
var orcaWorkerBrief string

var orcaBriefTmpl = template.Must(template.New("orca-worker").Parse(orcaWorkerBrief))

// OrcaBriefFile is the task each item's worker gets, written beside the
// item's attempts.
const OrcaBriefFile = "orca-brief.md"

// writeOrcaBrief renders an item's worker task into the store and returns
// its path.
func writeOrcaBrief(dir string, f *Feature, p PlanItem, base, baseItem string) (string, error) {
	var b bytes.Buffer
	if err := orcaBriefTmpl.Execute(&b, map[string]any{
		"Feature": f.ID, "Item": p.ID, "Title": p.Title, "Type": p.Type, "Express": p.Express,
		"Base": base, "BaseItem": baseItem,
	}); err != nil {
		return "", err
	}
	idir := filepath.Join(dir, ItemsDir, p.ID)
	if err := os.MkdirAll(idir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(idir, OrcaBriefFile)
	return path, os.WriteFile(path, b.Bytes(), 0644)
}

// orcaWorktreeName names an item's child worktree: <feature>-<item>.
func orcaWorktreeName(f *Feature, item string) string {
	return slugOr(f.ID+"-"+item, item)
}

// orcaBudget bounds a dispatch: each worker start makes a worktree and
// waits for the agent to take its task.
const orcaBudget = 10 * time.Minute

// DispatchOrca keeps a feature's Orca workers in step with the confirmed
// plan: it records merged pull requests, starts a worker for each ready
// item on the base chooseBase picks (a restarted one for an item whose
// attempt was released), asks a stacked item's worker to restack once the
// item under it reached the base, and says when every item is settled with
// its draft pull request.  Running it again changes nothing already in
// step.  A feature dispatched to Multica is refused.
func DispatchOrca(dir string, o OrcaDispatchOptions) ([]Action, error) {
	if o.Agent == "" {
		o.Agent = "claude"
	}
	ctx, cancel := context.WithTimeout(context.Background(), orcaBudget)
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
		fail := func(err error) (string, error) {
			if changed {
				return summary(), saveAnd(err)
			}
			return "", err
		}

		if f.Multica != nil && len(f.Multica.Items) > 0 {
			return "", fmt.Errorf("feature %s runs on Multica (%d items dispatched there); a feature runs on one backend", f.ID, len(f.Multica.Items))
		}
		if f.Closed != nil {
			return "", fmt.Errorf("feature %s is %s", f.ID, f.Closed.Kind)
		}
		snap, err := ReadConfirmed(dir, f)
		if err != nil {
			return "", err
		}
		if snap == nil {
			return "", fmt.Errorf("feature %s is not confirmed", f.ID)
		}
		if f.Orca == nil {
			f.Orca = &OrcaLink{}
		}
		if f.Orca.Items == nil {
			f.Orca.Items = map[string]*OrcaItem{}
		}
		link := f.Orca
		ready, s, err := Next(dir, f)
		if err != nil {
			return "", err
		}
		isReady := map[string]bool{}
		for _, r := range ready {
			isReady[r.ID] = true
		}

		// Merges first, so a merge seen now frees what waited for it.
		merged, did := recordMerges(dir, f, prs, func(string) string { return "" })
		actions = append(actions, merged...)
		changed = changed || did

		start := func(p PlanItem, base, baseItem, verb string) error {
			what := verb + " on " + base
			if baseItem != "" {
				what += " (stacked on " + baseItem + ")"
			}
			if o.DryRun {
				actions = append(actions, Action{Item: p.ID, What: "would be " + what})
				return nil
			}
			brief, err := writeOrcaBrief(dir, f, p, base, baseItem)
			if err != nil {
				return err
			}
			w, err := orca.WorkerStart(ctx, o.Dir, orca.WorkerSpec{
				SpecFile: brief, Title: fmt.Sprintf("%s %s: %s", f.ID, p.ID, p.Title),
				Name: orcaWorktreeName(f, p.ID), Base: base, Agent: o.Agent, Model: o.Model, Run: link.Run,
			})
			if err != nil {
				return fmt.Errorf("start the worker for %s: %w", p.ID, err)
			}
			if link.Run == "" {
				link.Run = w.Run
			}
			prev := link.Items[p.ID]
			oi := &OrcaItem{Dispatch: w.Dispatch, Task: w.Task, Workspace: w.Workspace, At: now(), Starts: 1,
				ItemBranch: ItemBranch{Base: base, BaseItem: baseItem}}
			if prev != nil {
				oi.Starts = prev.Starts + 1
				oi.Merged = prev.Merged
			}
			link.Items[p.ID] = oi
			changed = true
			what = verb + " worker " + w.Dispatch + " " + strings.TrimPrefix(what, verb+" ")
			if w.State != "" && w.State != "ready" {
				what += " (state " + w.State + ")"
			}
			actions = append(actions, Action{Item: p.ID, What: what})
			if w.Next != "" {
				actions = append(actions, Action{Item: p.ID, What: "Orca: " + w.Next, Note: true})
			}
			return nil
		}

		for _, p := range snap.Plan {
			is := s.item(p.ID)
			if is == nil || is.Removed {
				continue
			}
			oi := link.Items[p.ID]
			switch {
			case is.State == StateSkipped || is.State == StateDeferred:
				if oi != nil {
					actions = append(actions, Action{Item: p.ID, Note: true,
						What: fmt.Sprintf("%s by the user; its worker %s is left for the user to close", is.State, oi.Dispatch)})
				}
			case isReady[p.ID] && oi == nil:
				base, baseItem, wait := chooseBase(dir, f, p, s)
				if wait != "" {
					actions = append(actions, Action{Item: p.ID, What: "waits: " + wait, Note: true})
					continue
				}
				if err := start(p, base, baseItem, "started"); err != nil {
					return fail(err)
				}
			case isReady[p.ID] && is.State == StateClaimable:
				// Its last attempt was released or cancelled: a new worker
				// starts on the base the item was given.
				if err := start(p, oi.Base, oi.BaseItem, "restarted"); err != nil {
					return fail(err)
				}
			}
		}

		// A stacked item whose item underneath reached the base branch is
		// asked, through its worker, to restack onto it.
		for _, id := range sortedKeys(link.Items) {
			oi := link.Items[id]
			if oi.BaseItem == "" || oi.Merged != nil || oi.RestackFor == oi.BaseItem {
				continue
			}
			under := link.Items[oi.BaseItem]
			it := f.Item(id)
			if under == nil || under.Merged == nil || it == nil {
				continue
			}
			if pr, _ := itemPR(dir, it); pr == nil || pr.Base == f.BaseBranch {
				continue
			}
			if !reachedBase(f, oi.BaseItem) {
				actions = append(actions, Action{Item: id, Note: true,
					What: fmt.Sprintf("restack waits: %s merged into %s, not %s", oi.BaseItem, under.Merged.Into, f.BaseBranch)})
				continue
			}
			if o.DryRun {
				actions = append(actions, Action{Item: id, What: "would be asked to restack onto " + f.BaseBranch})
				continue
			}
			body := fmt.Sprintf("%s merged (%s). Restack this item onto %s: in this worktree, `bts feature restack %s %s`, then rerun the recipe's tests and report worker_done with them.",
				oi.BaseItem, short(under.Merged.SHA), f.BaseBranch, f.ID, id)
			if err := orca.Send(ctx, o.Dir, oi.Dispatch, "restack "+id, body); err != nil {
				return fail(fmt.Errorf("ask %s to restack: %w", id, err))
			}
			oi.RestackFor = oi.BaseItem
			changed = true
			actions = append(actions, Action{Item: id, What: "asked to restack onto " + f.BaseBranch})
		}

		// Every item settled, each done one with its draft pull request:
		// the feature is ready for the user's review.
		allPRs := true
		for _, is := range s.Items {
			if is.State != StateDone {
				continue
			}
			pr, _ := itemPR(dir, f.Item(is.ID))
			if pr == nil {
				allPRs = false
				actions = append(actions, Action{Item: is.ID, Note: true,
					What: fmt.Sprintf("done without a draft pull request: ask its worker to run bts feature pr %s %s", f.ID, is.ID)})
				continue
			}
			if rd, ok := prs[is.ID]; ok && rd.URL == pr.URL && rd.Err == nil && rd.State != nil && rd.State.State == "CLOSED" {
				allPRs = false
				actions = append(actions, Action{Item: is.ID, Note: true,
					What: fmt.Sprintf("its pull request %s was closed without merging: reopen it, or settle the item with the user", pr.URL)})
			}
		}
		if s.State == FeatureReady && allPRs {
			actions = append(actions, Action{Note: true, What: "every item is settled with its draft pull request: the feature is ready for the user's review"})
		}

		if o.DryRun {
			return "", ErrNoChange
		}
		detail := summary()
		if !changed && detail == "" {
			return "", ErrNoChange
		}
		return detail, nil
	})
	return actions, err
}

// recordMerges records each dispatched item's pull request that the read
// before the lock saw merged, and reports what it recorded.  key names the
// item on the backend, for the action.
func recordMerges(dir string, f *Feature, prs map[string]prRead, key func(id string) string) ([]Action, bool) {
	var actions []Action
	changed := false
	for _, id := range branchIDs(f) {
		b := branchOf(f, id)
		it := f.Item(id)
		if it == nil || b.Merged != nil {
			continue
		}
		pr, _ := itemPR(dir, it)
		rd, ok := prs[id]
		if pr == nil || !ok || rd.URL != pr.URL {
			continue // opened since the read: the next dispatch reads it
		}
		if rd.Err != nil {
			actions = append(actions, Action{Item: id, Issue: key(id), What: "could not read its pull request's state: " + rd.Err.Error(), Note: true})
			continue
		}
		if rd.State.State != "MERGED" {
			continue
		}
		ps := rd.State
		b.Merged = &MergeRecord{SHA: ps.MergeCommit.OID, At: ps.MergedAt, Into: ps.BaseRefName}
		changed = true
		what := "merged " + short(ps.MergeCommit.OID)
		if ps.BaseRefName != "" && ps.BaseRefName != f.BaseBranch {
			what += " into " + ps.BaseRefName
		}
		actions = append(actions, Action{Item: id, Issue: key(id), What: what})
	}
	return actions, changed
}
