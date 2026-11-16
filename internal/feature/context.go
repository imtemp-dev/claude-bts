package feature

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Served records what a context document was built from, so a later change
// to a contract or a dependency can be detected.
type Served struct {
	Feature      string      `json:"feature"`
	Item         string      `json:"item"`
	Confirmation int         `json:"confirmation"`
	Dependencies []ServedDep `json:"dependencies,omitempty"`
	Warnings     []string    `json:"warnings,omitempty"`
}

// ServedDep is one dependency's attempt as it was served.
type ServedDep struct {
	Item        string `json:"item"`
	Attempt     int    `json:"attempt"`
	RecipeID    string `json:"recipe_id"`
	ContentHash string `json:"content_hash"`
	HeadSHA     string `json:"head_sha,omitempty"`
	Direct      bool   `json:"direct"`
}

// depDocs are the documents of a done dependency served in full.
var depDocs = []string{"final.md", "fix-spec.md", "deviation.md", "review.md"}

// Context assembles what a child needs to work an item: the confirmed
// intent and contracts, the item, and each transitive dependency's latest
// done attempt from the store (direct ones in full), so it works after the
// dependency's worktree is gone.  worktree, when it is a git repository, is
// checked for each dependency's code.
func Context(featureID, itemID, worktree string) (string, *Served, error) {
	dir, err := Find(featureID)
	if err != nil {
		return "", nil, err
	}
	f, err := Load(dir)
	if err != nil {
		return "", nil, err
	}
	snap, err := ReadConfirmed(dir, f)
	if err != nil {
		return "", nil, err
	}
	if snap == nil {
		return "", nil, fmt.Errorf("feature %s is not confirmed yet", f.ID)
	}
	n, plan := snap.N, snap.Plan
	byID := map[string]PlanItem{}
	for _, p := range plan {
		byID[p.ID] = p
	}
	item, ok := byID[itemID]
	if !ok {
		return "", nil, fmt.Errorf("item %s is not in confirmation %d of %s", itemID, n, f.ID)
	}
	s, err := Derive(dir, f)
	if err != nil {
		return "", nil, err
	}
	served := &Served{Feature: f.ID, Item: itemID, Confirmation: n}
	if p := Pending(dir, f); len(p) > 0 {
		served.Warnings = append(served.Warnings, fmt.Sprintf("the %s changed after confirmation %d and is not confirmed; this context serves confirmation %d", strings.Join(p, ", "), n, n))
	}

	read := func(p string) string {
		b, _ := os.ReadFile(p)
		return strings.TrimSpace(string(b))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Feature %s: %s — context for %s\n\n", f.ID, f.Title, itemID)
	fmt.Fprintf(&b, "Served from confirmation %d of the feature store (%s).\n\n", n, dir)
	fmt.Fprintf(&b, "## Intent\n\n%s\n\n", demote(strings.TrimSpace(string(snap.Intent))))
	fmt.Fprintf(&b, "## Contracts\n\n%s\n\n", demote(strings.TrimSpace(string(snap.Contracts))))
	fmt.Fprintf(&b, "## This item\n\n- %s: %s (%s", item.ID, item.Title, item.Type)
	if item.Express {
		b.WriteString(", express")
	}
	b.WriteString(")\n")
	if len(item.DependsOn) > 0 {
		fmt.Fprintf(&b, "- Depends on: %s\n", strings.Join(item.DependsOn, ", "))
	}
	if f.Setup != "" {
		fmt.Fprintf(&b, "- Setup: `%s` makes a fresh checkout runnable; run it in the repository before the recipe's first build or test (bts runs it in every copy it makes)\n", f.Setup)
	}
	b.WriteString("\n")

	// Transitive dependencies, nearest first.
	direct := map[string]bool{}
	for _, d := range item.DependsOn {
		direct[d] = true
	}
	var order []string
	seen := map[string]bool{}
	queue := append([]string(nil), item.DependsOn...)
	for len(queue) > 0 {
		d := queue[0]
		queue = queue[1:]
		if seen[d] {
			continue
		}
		seen[d] = true
		order = append(order, d)
		queue = append(queue, byID[d].DependsOn...)
	}
	if len(order) > 0 {
		b.WriteString("## Earlier items\n\n")
	}
	for _, d := range order {
		ds := s.item(d)
		p := byID[d]
		if ds == nil || ds.done == nil {
			state := "not done"
			if ds != nil {
				state = ds.State
			}
			fmt.Fprintf(&b, "### %s: %s — %s\n\n", d, p.Title, state)
			continue
		}
		// Every done attempt, oldest first: the build's documents, then
		// each fix-forward's, so a dependent sees the patch and what it
		// patches.  The newest one's head is the code to build on.
		latest := ds.doneAll[len(ds.doneAll)-1]
		fmt.Fprintf(&b, "### %s: %s — %d done attempt(s), latest a%d", d, p.Title, len(ds.doneAll), latest.N)
		if latest.Manifest.HeadSHA != "" {
			fmt.Fprintf(&b, ", head %s", short(latest.Manifest.HeadSHA))
		}
		b.WriteString("\n\n")
		if worktree != "" && latest.Manifest.HeadSHA != "" {
			if yes, ok := isAncestor(worktree, latest.Manifest.HeadSHA); ok && !yes {
				served.Warnings = append(served.Warnings, fmt.Sprintf("%s's code (head %s) is not in this worktree's history; start from a base that contains it", d, short(latest.Manifest.HeadSHA)))
			}
		}
		for _, a := range ds.doneAll {
			served.Dependencies = append(served.Dependencies, ServedDep{Item: d, Attempt: a.N, RecipeID: a.Claim.RecipeID, ContentHash: a.Manifest.ContentHash, HeadSHA: a.Manifest.HeadSHA, Direct: direct[d]})
			if a.Manifest.Dirty {
				served.Warnings = append(served.Warnings, fmt.Sprintf("%s a%d finished with uncommitted changes; its head %s may not hold all of its code", d, a.N, short(a.Manifest.HeadSHA)))
			}
			rdir := filepath.Join(currentSnapshot(a.Dir), "recipe")
			if !direct[d] {
				fmt.Fprintf(&b, "Indirect dependency: a%d (%s, recipe %s) is in %s.\n\n", a.N, a.Claim.Kind, a.Claim.RecipeID, rdir)
				continue
			}
			fmt.Fprintf(&b, "#### a%d: %s, recipe %s, done via %s\n\n", a.N, a.Claim.Kind, a.Claim.RecipeID, a.Manifest.CompletedVia)
			for _, doc := range depDocs {
				if text := read(filepath.Join(rdir, doc)); text != "" {
					fmt.Fprintf(&b, "##### %s\n\n%s\n\n", doc, demote(demote(demote(text))))
				}
			}
		}
	}
	if len(served.Warnings) > 0 {
		b.WriteString("## Warnings\n\n")
		for _, w := range served.Warnings {
			fmt.Fprintf(&b, "- %s\n", w)
		}
	}
	return b.String(), served, nil
}

// demote pushes a document's headings one level down, so it nests under the
// context's own headings.
func demote(doc string) string {
	lines := strings.Split(doc, "\n")
	inFence := false
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			inFence = !inFence
		}
		if !inFence && strings.HasPrefix(l, "#") {
			lines[i] = "#" + l
		}
	}
	return strings.Join(lines, "\n")
}

func short(sha string) string {
	if len(sha) > 10 {
		return sha[:10]
	}
	return sha
}
