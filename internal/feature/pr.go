package feature

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

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

// ghTimeout bounds one GitHub CLI call: a gh waiting on an auth prompt or a
// dead network must not hold a feature command, or its lock, for good.
var ghTimeout = 30 * time.Second

// ghRun runs the GitHub CLI in dir.  Tests replace it.  It runs inside bts,
// not as a typed command, so the user's permission asks on gh pr commands
// do not apply; bts runs only the pull-request operations its commands own.
var ghRun = runGH

func runGH(dir, stdin string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ghTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = dir
	cmd.WaitDelay = time.Second
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	name := strings.Join(args[:min(2, len(args))], " ")
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("gh %s: no answer within %s", name, ghTimeout)
	}
	if err != nil {
		return out, fmt.Errorf("gh %s: %v: %s", name, err, strings.TrimSpace(stderr.String()))
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

// itemRestack returns the newest restack of the item's pull request, from
// whichever attempt recorded it, or nil.
func itemRestack(atts []*Attempt, pr *PRRecord) *RestackRecord {
	for i := len(atts) - 1; i >= 0; i-- {
		if p := readPR(atts[i].Dir); p == nil || p.URL != pr.URL {
			continue
		}
		if rs := latestRestack(atts[i].Dir); rs != nil {
			return rs
		}
	}
	return nil
}

// itemFork is where the item's commits on its pull request's branch begin:
// the newest restack's new fork, else the start of the attempt that opened
// the pull request.  A fix-forward attempt starts on top of the earlier
// attempts' commits and pushes to the same pull request, so its own start
// would leave theirs out of a rebase.
func itemFork(atts []*Attempt, pr *PRRecord) string {
	if rs := itemRestack(atts, pr); rs != nil {
		return rs.NewFork
	}
	for _, a := range atts {
		if p := readPR(a.Dir); p != nil && p.URL == pr.URL && a.Claim != nil {
			return a.Claim.StartSHA
		}
	}
	return ""
}

// boundRecipe is the worktree's recipe for the item: its newest attempt.
func boundRecipe(root, featureID, item string) (*state.RecipeState, error) {
	recipes, err := state.ListRecipes(root)
	if err != nil {
		return nil, err
	}
	var best *state.RecipeState
	for _, r := range recipes {
		if r.FeatureItem == item && SameID(r.Feature, featureID) && (best == nil || r.FeatureAttempt > best.FeatureAttempt) {
			best = r
		}
	}
	if best == nil {
		return nil, fmt.Errorf("this worktree has no recipe for item %s of %s", item, featureID)
	}
	return best, nil
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

// prBody is the draft pull request's description: what the item delivers,
// its invariants with the tests that hold them, the checks bts recorded,
// and which recipe documents the text cites, since those live in .bts/
// and a reviewer of the pull request cannot open them (trial problem
// #158).  It names no local path: other people read it.
func prBody(pc *prContext, issueKey string) string {
	rdir := state.RecipeDir(pc.root, pc.r.ID)
	var b strings.Builder
	fmt.Fprintf(&b, "Item %s of bts feature %s: %s.\n\n", pc.plan.ID, pc.f.ID, pc.plan.Title)
	if text := firstParagraph(filepath.Join(rdir, "final.md")); text != "" {
		b.WriteString(text + "\n\n")
		if table := sectionTable(filepath.Join(rdir, "final.md"), "invariants"); table != "" {
			b.WriteString("Invariants and the tests that hold them:\n\n" + table + "\n\n")
		}
	} else if text := firstParagraph(filepath.Join(rdir, "fix-spec.md")); text != "" {
		b.WriteString(text + "\n\n")
	}
	if checks := recordedChecks(pc.root, pc.r.ID); len(checks) > 0 {
		b.WriteString("Checks bts recorded:\n\n" + strings.Join(checks, "\n") + "\n\n")
	}
	if cited := citedRecords(b.String(), rdir); len(cited) > 0 {
		verb := "is a bts record"
		if len(cited) > 1 {
			verb = "are bts records"
		}
		fmt.Fprintf(&b, "%s %s of the item's recipe, kept outside the repository (in the worker's worktree and the bts feature store); not part of this pull request.\n\n",
			joinNames(cited), verb)
	}
	b.WriteString("Draft opened by bts when the item's recipe completed; not ready for review.")
	if issueKey != "" {
		fmt.Fprintf(&b, "  Tracked as %s.", issueKey)
	}
	b.WriteString("\n")
	return b.String()
}

// sectionTable is the first table in the section of a Markdown document
// whose heading contains word (any case), or "".
func sectionTable(path, word string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	in := false
	var rows []string
	for _, line := range strings.Split(string(data), "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "#") {
			if len(rows) > 0 {
				break
			}
			in = strings.Contains(strings.ToLower(l), strings.ToLower(word))
			continue
		}
		if !in {
			continue
		}
		if strings.HasPrefix(l, "|") {
			rows = append(rows, l)
		} else if len(rows) > 0 {
			break
		}
	}
	return strings.Join(rows, "\n")
}

// recordedChecks summarises what bts recorded about the recipe's tests,
// its interrogate run and its deviation report, one line each.
func recordedChecks(root, recipeID string) []string {
	var out []string
	if tr, err := state.LoadTestResults(root, recipeID); err == nil && tr != nil && strings.TrimSpace(tr.Command) != "" {
		line := fmt.Sprintf("- Tests: `%s`, %s", tr.Command, tr.Status)
		if tr.Total > 0 {
			line = fmt.Sprintf("- Tests: `%s`, %d passed", tr.Command, tr.Passed)
			if tr.Failed > 0 {
				line += fmt.Sprintf(", %d failed", tr.Failed)
			}
		}
		out = append(out, line)
	}
	if run, err := state.LoadInterrogateRun(root, recipeID); err == nil && run != nil && run.VerifiedAt != "" {
		var fixed, waived, open int
		for _, f := range run.Findings {
			switch f.Status {
			case state.InterrogateFixed:
				fixed++
			case state.InterrogateWaived:
				waived++
			case state.InterrogateReproduced:
				open++
			}
		}
		line := "- Interrogate: nothing reproduced"
		if n := fixed + waived + open; n > 0 {
			line = fmt.Sprintf("- Interrogate: %d reproduced, %d fixed", n, fixed)
			if waived > 0 {
				line += fmt.Sprintf(", %d left unfixed with the user's agreement", waived)
			}
			if open > 0 {
				line += fmt.Sprintf(", %d open", open)
			}
		}
		out = append(out, line)
	}
	if dev, ok := deviationCounts(filepath.Join(state.RecipeDir(root, recipeID), "deviation.md")); ok {
		out = append(out, fmt.Sprintf("- Deviation report: %s, %d not implemented", plural(dev["Deviations"], "deviation"), dev["Not Implemented"]))
	}
	return out
}

// deviationCounts reads the "- Name: N" lines of deviation.md's Summary.
func deviationCounts(path string) (map[string]int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	counts := map[string]int{}
	in := false
	for _, line := range strings.Split(string(data), "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "#") {
			if in {
				break
			}
			in = strings.EqualFold(strings.TrimSpace(strings.TrimLeft(l, "#")), "summary")
			continue
		}
		i := strings.LastIndex(l, ":")
		if !in || !strings.HasPrefix(l, "- ") || i < 3 {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(l[i+1:]), "%d", &n); err == nil {
			counts[strings.TrimSpace(l[2:i])] = n
		}
	}
	_, ok := counts["Deviations"]
	return counts, ok
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

var backticked = regexp.MustCompile("`([^`\n]+)`")

// citedRecords are the recipe's own files (its documents, probes, records)
// a text names in backticks, in order: a reviewer of the pull request
// cannot open them.
func citedRecords(text, recipeDir string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range backticked.FindAllStringSubmatch(text, -1) {
		name := strings.TrimSpace(m[1])
		if seen[name] || strings.ContainsAny(name, " \t") || strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
			continue
		}
		if fi, err := os.Stat(filepath.Join(recipeDir, filepath.FromSlash(name))); err == nil && fi.Mode().IsRegular() {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// joinNames is "`a`", "`a` and `b`" or "`a`, `b` and `c`".
func joinNames(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = "`" + n + "`"
	}
	if len(q) == 1 {
		return q[0]
	}
	return strings.Join(q[:len(q)-1], ", ") + " and " + q[len(q)-1]
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
// item it was stacked on reached it: `git rebase --onto origin/<base>
// <fork>` on the worktree's HEAD, which must be on the pull request's branch
// and hold its head.  fork is where the item's commits begin, whichever
// attempt made them (itemFork).  A conflict aborts the rebase and is
// reported.  On success it pushes the branch, leased on the head bts last
// pushed, records the restack, then points the pull request at the base
// branch; a retry after a failed retarget only retargets.
func Restack(root, featureID, item string) (*RestackRecord, error) {
	pc, err := loadPRContext(root, featureID, item)
	if err != nil {
		return nil, err
	}
	it := pc.f.Item(item)
	if it == nil {
		return nil, fmt.Errorf("no item %s in %s", item, pc.f.ID)
	}
	pr, _ := itemPR(pc.dir, it)
	if pr == nil {
		return nil, fmt.Errorf("item %s has no pull request to restack", item)
	}
	if pc.f.Multica == nil || pc.f.Multica.Items[item] == nil || pc.f.Multica.Items[item].BaseItem == "" {
		return nil, fmt.Errorf("item %s was not stacked on another item", item)
	}
	underID := pc.f.Multica.Items[item].BaseItem
	if !reachedBase(pc.f, underID) {
		if under := pc.f.Multica.Items[underID]; under != nil && under.Merged != nil {
			return nil, fmt.Errorf("item %s is stacked on %s, which merged into %s, not %s", item, underID, under.Merged.Into, pc.f.BaseBranch)
		}
		return nil, fmt.Errorf("item %s is stacked on %s, which has not merged", item, underID)
	}
	target := pc.f.BaseBranch
	atts := Attempts(pc.dir, it)
	if last := itemRestack(atts, pr); last != nil && last.ToRef == target && last.HeadSHA == pr.HeadSHA && pr.Base != target {
		return retarget(pc, pr, last)
	}
	if dirty(root) {
		return nil, fmt.Errorf("uncommitted changes: restack needs a clean worktree")
	}
	if cur := branch(root); cur != pr.Branch {
		if cur == "" {
			cur = "a detached HEAD"
		}
		return nil, fmt.Errorf("the worktree is on %s; restack runs on the pull request's branch %s (git switch %s)", cur, pr.Branch, pr.Branch)
	}
	if yes, _ := isAncestor(root, pr.HeadSHA); !yes {
		return nil, fmt.Errorf("the worktree's %s does not contain the pull request's head %s: bring it up to date (git pull --ff-only origin %s), then restack again", pr.Branch, short(pr.HeadSHA), pr.Branch)
	}
	fork := itemFork(atts, pr)
	if fork == "" {
		return nil, fmt.Errorf("no record of where item %s's commits begin", item)
	}
	if _, err := gitRun(root, "fetch", "origin", target); err != nil {
		return nil, err
	}
	onto := "origin/" + target
	before := headSHA(root)
	if _, err := gitRun(root, "rebase", "--onto", onto, fork); err != nil {
		_, _ = gitRun(root, "rebase", "--abort")
		return nil, fmt.Errorf("rebase onto %s stopped on a conflict, and was aborted; resolve it by hand: %w", onto, err)
	}
	head := headSHA(root)
	if _, err := gitRun(root, "push", "--force-with-lease="+pr.Branch+":"+pr.HeadSHA, "origin", "HEAD:refs/heads/"+pr.Branch); err != nil {
		_, _ = gitRun(root, "reset", "-q", "--hard", before)
		return nil, fmt.Errorf("rebased, but the push was refused (origin's %s is no longer %s?); the branch is back as it was: %w", pr.Branch, short(pr.HeadSHA), err)
	}
	newFork, _ := gitRun(root, "merge-base", onto, "HEAD")
	rec := RestackRecord{Schema: Schema, FromRef: pr.Base, ToRef: target, OldFork: fork, NewFork: newFork, HeadSHA: head, At: now()}
	var list []RestackRecord
	_ = state.ReadJSON(filepath.Join(pc.ad, RestackFile), &list)
	list = append(list, rec)
	if err := state.WriteJSON(filepath.Join(pc.ad, RestackFile), list); err != nil {
		return nil, err
	}
	pr.HeadSHA, pr.UpdatedAt = head, now()
	if err := state.WriteJSON(filepath.Join(pc.ad, PRFile), pr); err != nil {
		return nil, err
	}
	return retarget(pc, pr, &rec)
}

// retarget points the item's pull request at the restack's target branch
// and records it.
func retarget(pc *prContext, pr *PRRecord, rec *RestackRecord) (*RestackRecord, error) {
	if _, err := ghRun(pc.root, "", "pr", "edit", pr.URL, "--base", rec.ToRef); err != nil {
		return nil, fmt.Errorf("rebased and pushed, but retargeting the pull request to %s failed; run bts feature restack again to retarget it: %w", rec.ToRef, err)
	}
	pr.Base, pr.UpdatedAt = rec.ToRef, now()
	if err := state.WriteJSON(filepath.Join(pc.ad, PRFile), pr); err != nil {
		return nil, err
	}
	return rec, nil
}

// PRState is a pull request's state on GitHub.
type PRState struct {
	State       string `json:"state"` // OPEN, CLOSED, MERGED
	BaseRefName string `json:"baseRefName"`
	MergedAt    string `json:"mergedAt"`
	MergeCommit struct {
		OID string `json:"oid"`
	} `json:"mergeCommit"`
}

func prState(url string) (*PRState, error) {
	out, err := ghRun("", "", "pr", "view", url, "--json", "state,baseRefName,mergedAt,mergeCommit")
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
