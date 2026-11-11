package feature

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/multica"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Attempt files a worker writes beside claim.json.  The snapshot is frozen
// at completion; these record what happens to the item's branch afterwards.
const (
	PRFile      = "pr.json"
	RestackFile = "restack.json"
)

// PRRecord is the item's draft pull request, as its worker opened it.
type PRRecord struct {
	Schema    int    `json:"schema"`
	URL       string `json:"url"`
	Number    int    `json:"number,omitempty"`
	Branch    string `json:"branch"`
	Base      string `json:"base"`
	HeadSHA   string `json:"head_sha"`
	OpenedAt  string `json:"opened_at"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// RestackRecord is one rebase of the item's branch onto the base branch
// after the item it was stacked on merged.
type RestackRecord struct {
	Schema  int    `json:"schema"`
	FromRef string `json:"from_base"`
	ToRef   string `json:"to_base"`
	OldFork string `json:"old_fork"`
	NewFork string `json:"new_fork"`
	HeadSHA string `json:"head_sha"`
	At      string `json:"at"`
}

// ghRun runs the GitHub CLI in dir.  Tests replace it.  It runs inside bts,
// not as a typed command, so the user's permission asks on gh pr commands
// do not apply; bts runs only the pull-request operations its commands own.
var ghRun = func(dir, stdin string, args ...string) ([]byte, error) {
	cmd := exec.Command("gh", args...)
	cmd.Dir = dir
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("gh %s: %v: %s", strings.Join(args[:min(2, len(args))], " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// SetGHRunner replaces the GitHub CLI runner and returns its restore.
func SetGHRunner(f func(dir, stdin string, args ...string) ([]byte, error)) (restore func()) {
	prev := ghRun
	ghRun = f
	return func() { ghRun = prev }
}

// gitRun runs git in dir, returning stdout and git's message on failure.
func gitRun(dir string, args ...string) (string, error) {
	cmd := gitCmd(dir, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args[:min(2, len(args))], " "), strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

// readPR returns an attempt's pull request record, or nil.
func readPR(ad string) *PRRecord {
	var p PRRecord
	if err := state.ReadJSON(filepath.Join(ad, PRFile), &p); err != nil || p.URL == "" {
		return nil
	}
	return &p
}

// latestRestack returns the attempt's last restack, or nil.
func latestRestack(ad string) *RestackRecord {
	var list []RestackRecord
	if err := state.ReadJSON(filepath.Join(ad, RestackFile), &list); err != nil || len(list) == 0 {
		return nil
	}
	return &list[len(list)-1]
}

// itemPR returns the newest pull request any attempt of the item opened.
func itemPR(dir string, it *Item) (*PRRecord, *Attempt) {
	atts := Attempts(dir, it)
	for i := len(atts) - 1; i >= 0; i-- {
		if p := readPR(atts[i].Dir); p != nil {
			return p, atts[i]
		}
	}
	return nil, nil
}

// boundRecipe is the worktree's recipe for the item: its newest attempt.
func boundRecipe(root, featureID, item string) (*state.RecipeState, error) {
	recipes, err := state.ListRecipes(root)
	if err != nil {
		return nil, err
	}
	want := featurePrefix(featureID)
	var best *state.RecipeState
	for _, r := range recipes {
		if r.FeatureItem == item && featurePrefix(r.Feature) == want && (best == nil || r.FeatureAttempt > best.FeatureAttempt) {
			best = r
		}
	}
	if best == nil {
		return nil, fmt.Errorf("this worktree has no recipe for item %s of %s", item, featureID)
	}
	return best, nil
}

func featurePrefix(id string) string {
	var k int
	if _, err := fmt.Sscanf(strings.TrimSpace(id), "f-%d", &k); err != nil {
		return id
	}
	return fmt.Sprintf("f-%03d", k)
}

// BranchName is the branch an item's pull request uses:
// <prefix><ticket>-<issue key>-<slug>.  The Multica issue key in it lets
// Multica link the pull request to the item's sub-issue.
func BranchName(prefix, ticket, issueKey, title string) string {
	if ticket == "" {
		ticket = "NA"
	}
	parts := []string{ticket}
	if issueKey != "" {
		parts = append(parts, issueKey)
	}
	parts = append(parts, slugOr(title, "item"))
	return prefix + strings.Join(parts, "-")
}

// prContext gathers what OpenDraftPR and Restack work from.
type prContext struct {
	root  string
	r     *state.RecipeState
	dir   string // feature directory
	f     *Feature
	ad    string // attempt directory
	claim *Claim
	plan  PlanItem
}

func loadPRContext(root, featureID, item string) (*prContext, error) {
	r, err := boundRecipe(root, featureID, item)
	if err != nil {
		return nil, err
	}
	ad, ok := attemptDir(r)
	if !ok {
		return nil, fmt.Errorf("attempt a%d of item %s is not in the feature store", r.FeatureAttempt, item)
	}
	dir := filepath.Dir(filepath.Dir(filepath.Dir(ad)))
	f, err := Load(dir)
	if err != nil {
		return nil, err
	}
	var c Claim
	if err := state.ReadJSON(filepath.Join(ad, ClaimFile), &c); err != nil {
		return nil, err
	}
	pc := &prContext{root: root, r: r, dir: dir, f: f, ad: ad, claim: &c}
	if snap, err := ReadConfirmed(dir, f); err == nil && snap != nil {
		for _, p := range snap.Plan {
			if p.ID == item {
				pc.plan = p
			}
		}
	}
	if pc.plan.ID == "" {
		pc.plan = PlanItem{ID: item, Title: item}
	}
	return pc, nil
}

// baseFor is the branch the item's pull request targets: the base dispatch
// chose (an unmerged dependency's branch when stacked), else the feature's.
func (pc *prContext) baseFor() string {
	if pc.f.Multica != nil {
		if mi := pc.f.Multica.Items[pc.plan.ID]; mi != nil && mi.Base != "" {
			return mi.Base
		}
	}
	return pc.f.BaseBranch
}

func prBody(pc *prContext, issueKey string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Item %s of bts feature %s: %s.\n\n", pc.plan.ID, pc.f.ID, pc.plan.Title)
	if text := firstParagraph(filepath.Join(state.RecipeDir(pc.root, pc.r.ID), "final.md")); text != "" {
		b.WriteString(text + "\n\n")
	} else if text := firstParagraph(filepath.Join(state.RecipeDir(pc.root, pc.r.ID), "fix-spec.md")); text != "" {
		b.WriteString(text + "\n\n")
	}
	b.WriteString("Draft opened by bts when the item's recipe completed; not ready for review.")
	if issueKey != "" {
		fmt.Fprintf(&b, "  Tracked as %s.", issueKey)
	}
	b.WriteString("\n")
	return b.String()
}

// firstParagraph is the first prose paragraph of a Markdown document,
// skipping headings and status lines.
func firstParagraph(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var para []string
	for _, line := range strings.Split(string(data), "\n") {
		l := strings.TrimSpace(line)
		switch {
		case l == "":
			if len(para) > 0 {
				return strings.Join(para, " ")
			}
		case strings.HasPrefix(l, "#"), strings.HasPrefix(l, "Status:"), strings.HasPrefix(l, "<!--"), strings.HasPrefix(l, "|"):
			continue
		default:
			para = append(para, l)
		}
	}
	return strings.Join(para, " ")
}

// OpenDraftPR pushes the item's branch and opens its draft pull request, or
// pushes new commits to the one already open.  It renames a branch Multica
// named (agent/…) to BranchName first.  It never marks a pull request ready,
// merges, or pushes any branch but the item's own.
func OpenDraftPR(root, featureID, item string) (*PRRecord, bool, error) {
	pc, err := loadPRContext(root, featureID, item)
	if err != nil {
		return nil, false, err
	}
	if pc.r.Phase != terminalPhase(pc.r.Type) {
		return nil, false, fmt.Errorf("%s is in phase %s: the pull request opens once the recipe completes", pc.r.ID, pc.r.Phase)
	}
	base := pc.baseFor()
	cur := branch(root)
	for _, protected := range []string{base, pc.f.BaseBranch, "main", "master"} {
		if cur == protected {
			return nil, false, fmt.Errorf("the worktree is on %s; an item's work goes on its own branch", cur)
		}
	}
	if cur == "" {
		return nil, false, fmt.Errorf("the worktree is not on a branch")
	}
	if dirty(root) {
		return nil, false, fmt.Errorf("uncommitted changes: commit the item's work before its pull request")
	}
	head := headSHA(root)
	fork := pc.claim.StartSHA
	if rs := latestRestack(pc.ad); rs != nil {
		fork = rs.NewFork
	}
	if head == fork {
		return nil, false, fmt.Errorf("no commits since the item started (%s)", short(fork))
	}

	// A fix-forward pushes to the pull request an earlier attempt opened.
	existing := readPR(pc.ad)
	if existing == nil {
		if it := pc.f.Item(item); it != nil {
			if p, _ := itemPR(pc.dir, it); p != nil {
				existing = p
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), multicaBudget)
	defer cancel()
	issueKey := ""
	if pc.claim.MulticaIssue != "" {
		if is, err := multica.GetIssue(ctx, pc.claim.MulticaIssue); err == nil {
			issueKey = is.Identifier
		}
	}

	if existing != nil {
		if _, err := gitRun(root, "push", "origin", "HEAD:refs/heads/"+existing.Branch); err != nil {
			return nil, false, fmt.Errorf("push to %s (the item's open pull request): %w", existing.Branch, err)
		}
		rec := *existing
		rec.HeadSHA, rec.UpdatedAt = head, now()
		if err := state.WriteJSON(filepath.Join(pc.ad, PRFile), &rec); err != nil {
			return nil, false, err
		}
		return &rec, false, nil
	}

	prefix := ""
	if cfg, err := LoadMulticaConfig(); err == nil {
		prefix = cfg.BranchPrefix
	}
	if strings.HasPrefix(cur, "agent/") || (prefix != "" && !strings.HasPrefix(cur, prefix)) {
		want := BranchName(prefix, pc.f.Ticket, issueKey, pc.plan.Title)
		if _, err := gitRun(root, "branch", "-m", want); err != nil {
			return nil, false, err
		}
		cur = want
	}
	if _, err := gitRun(root, "push", "-u", "origin", cur); err != nil {
		return nil, false, err
	}

	rec := &PRRecord{Schema: Schema, Branch: cur, Base: base, HeadSHA: head, OpenedAt: now()}
	if out, err := ghRun(root, "", "pr", "view", cur, "--json", "url,number,state"); err == nil {
		var v struct {
			URL    string `json:"url"`
			Number int    `json:"number"`
			State  string `json:"state"`
		}
		if json.Unmarshal(out, &v) == nil && v.URL != "" && v.State == "OPEN" {
			rec.URL, rec.Number = v.URL, v.Number
		}
	}
	created := false
	if rec.URL == "" {
		ticket := pc.f.Ticket
		if ticket == "" {
			ticket = "NA"
		}
		title := fmt.Sprintf("[%s] %s", ticket, pc.plan.Title)
		out, err := ghRun(root, prBody(pc, issueKey), "pr", "create", "--draft", "--base", base, "--head", cur, "--title", title, "--body-file", "-")
		if err != nil {
			return nil, false, err
		}
		rec.URL = lastLine(string(out))
		if out, err := ghRun(root, "", "pr", "view", rec.URL, "--json", "number"); err == nil {
			var v struct {
				Number int `json:"number"`
			}
			if json.Unmarshal(out, &v) == nil {
				rec.Number = v.Number
			}
		}
		created = true
	}
	if err := state.WriteJSON(filepath.Join(pc.ad, PRFile), rec); err != nil {
		return nil, false, err
	}
	return rec, created, nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// Restack moves the item's branch onto the feature's base branch after the
// item it was stacked on merged: `git rebase --onto origin/<base> <fork>`,
// where fork is the commit the item started from (or its last restack's).
// A conflict aborts the rebase and is reported.  On success it pushes the
// item's own branch with --force-with-lease and points its pull request at
// the base branch.
func Restack(root, featureID, item string) (*RestackRecord, error) {
	pc, err := loadPRContext(root, featureID, item)
	if err != nil {
		return nil, err
	}
	pr := readPR(pc.ad)
	if pr == nil {
		return nil, fmt.Errorf("item %s has no pull request to restack", item)
	}
	if pc.f.Multica == nil || pc.f.Multica.Items[item] == nil || pc.f.Multica.Items[item].BaseItem == "" {
		return nil, fmt.Errorf("item %s was not stacked on another item", item)
	}
	under := pc.f.Multica.Items[pc.f.Multica.Items[item].BaseItem]
	if under == nil || under.Merged == nil {
		return nil, fmt.Errorf("item %s is stacked on %s, which has not merged", item, pc.f.Multica.Items[item].BaseItem)
	}
	if dirty(root) {
		return nil, fmt.Errorf("uncommitted changes: restack needs a clean worktree")
	}
	fork := pc.claim.StartSHA
	if rs := latestRestack(pc.ad); rs != nil {
		fork = rs.NewFork
	}
	target := pc.f.BaseBranch
	if _, err := gitRun(root, "fetch", "origin", target); err != nil {
		return nil, err
	}
	onto := "origin/" + target
	if _, err := gitRun(root, "rebase", "--onto", onto, fork, pr.Branch); err != nil {
		_, _ = gitRun(root, "rebase", "--abort")
		return nil, fmt.Errorf("rebase onto %s stopped on a conflict, and was aborted; resolve it by hand: %w", onto, err)
	}
	if _, err := gitRun(root, "push", "--force-with-lease", "origin", pr.Branch); err != nil {
		return nil, err
	}
	if _, err := ghRun(root, "", "pr", "edit", pr.URL, "--base", target); err != nil {
		return nil, fmt.Errorf("rebased and pushed, but retargeting the pull request failed: %w", err)
	}
	newFork, _ := gitRun(root, "merge-base", onto, "HEAD")
	rec := RestackRecord{Schema: Schema, FromRef: pr.Base, ToRef: target, OldFork: fork, NewFork: newFork, HeadSHA: headSHA(root), At: now()}
	var list []RestackRecord
	_ = state.ReadJSON(filepath.Join(pc.ad, RestackFile), &list)
	list = append(list, rec)
	if err := state.WriteJSON(filepath.Join(pc.ad, RestackFile), list); err != nil {
		return nil, err
	}
	pr.Base, pr.HeadSHA, pr.UpdatedAt = target, rec.HeadSHA, now()
	if err := state.WriteJSON(filepath.Join(pc.ad, PRFile), pr); err != nil {
		return nil, err
	}
	return &rec, nil
}

// PRState is a pull request's state on GitHub.
type PRState struct {
	State       string `json:"state"` // OPEN, CLOSED, MERGED
	MergedAt    string `json:"mergedAt"`
	MergeCommit struct {
		OID string `json:"oid"`
	} `json:"mergeCommit"`
}

func prState(url string) (*PRState, error) {
	out, err := ghRun("", "", "pr", "view", url, "--json", "state,mergedAt,mergeCommit")
	if err != nil {
		return nil, err
	}
	var s PRState
	if err := json.Unmarshal(out, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// sortedKeys returns a map's keys in order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
