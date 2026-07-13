package engine

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Anchor regexps. These live at package scope so the compiled forms are
// reused across calls — draft.md and wireframe.md can be large in
// practice, and parsing cost compounds across every /verify cycle.
var (
	wireframePathIDRe = regexp.MustCompile(`<!--\s*path-id:\s*([A-Za-z0-9._\-]+)\s*-->`)
	draftPathRefRe    = regexp.MustCompile(`<!--\s*path:\s*wireframe\.md#([A-Za-z0-9._\-]+)\s*-->`)
)

// CheckWireframeAnchors enforces the wireframe ↔ draft path contract:
//
//   - Every `<!-- path-id: X -->` in wireframe.md MUST be referenced by
//     at least one `<!-- path: wireframe.md#X -->` in draft.md. Paths
//     declared but never specified in draft are CRITICAL — they are
//     execution paths without a behavior spec.
//
//   - Every draft anchor MUST resolve to a wireframe path-id. Orphan
//     draft anchors are MAJOR (the draft describes a path the wireframe
//     does not acknowledge; structure has drifted).
//
//   - Duplicate path-ids in wireframe.md are MAJOR (ambiguous target).
//
// Returns nil when either file is missing — callers rely on the
// precondition checker and text-level verifier to flag those cases.
func CheckWireframeAnchors(draftPath, wireframePath string) []Issue {
	wfData, err := os.ReadFile(wireframePath)
	if err != nil {
		return nil
	}
	drData, err := os.ReadFile(draftPath)
	if err != nil {
		return nil
	}

	wfIDs, wfDuplicates := extractAnchors(string(wfData), wireframePathIDRe)
	drIDs, _ := extractAnchors(string(drData), draftPathRefRe)

	var issues []Issue

	// Duplicate path-ids in wireframe.md
	for _, dup := range wfDuplicates {
		issues = append(issues, Issue{
			Category: "wireframe_anchor",
			Claim:    "duplicate_path_id: " + dup,
			Severity: "major",
			Detail:   "path-id '" + dup + "' appears more than once in wireframe.md. Each path-id must be unique; rename one path and update any draft references.",
		})
	}

	wfSet := toSet(wfIDs)
	drSet := toSet(drIDs)

	// Wireframe paths missing from draft — each is an unspecified
	// execution path.
	var unmapped []string
	for id := range wfSet {
		if !drSet[id] {
			unmapped = append(unmapped, id)
		}
	}
	sort.Strings(unmapped)
	for _, id := range unmapped {
		issues = append(issues, Issue{
			Category: "wireframe_anchor",
			Claim:    "unmapped_path: " + id,
			Severity: "critical",
			Detail:   "wireframe.md declares path-id '" + id + "' but draft.md has no `<!-- path: wireframe.md#" + id + " -->` section. Either specify the path in draft.md or remove the id from wireframe.md.",
		})
	}

	// Draft anchors with no matching wireframe id — drift.
	var orphan []string
	for id := range drSet {
		if !wfSet[id] {
			orphan = append(orphan, id)
		}
	}
	sort.Strings(orphan)
	for _, id := range orphan {
		issues = append(issues, Issue{
			Category: "wireframe_anchor",
			Claim:    "orphan_draft_anchor: " + id,
			Severity: "major",
			Detail:   "draft.md references wireframe.md#" + id + " but wireframe.md does not declare that path-id. Either add the path to wireframe.md or retarget the draft anchor.",
		})
	}

	return issues
}

// extractAnchors returns the ordered list of captured anchor ids (in
// document order) and the set of duplicates (first occurrence preserved).
// Keeping order makes issue output deterministic across runs.
func extractAnchors(content string, re *regexp.Regexp) ([]string, []string) {
	matches := re.FindAllStringSubmatch(content, -1)
	seen := map[string]int{}
	var ids []string
	var dups []string
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		id := strings.TrimSpace(m[1])
		if id == "" {
			continue
		}
		seen[id]++
		switch seen[id] {
		case 1:
			ids = append(ids, id)
		case 2:
			dups = append(dups, id)
		}
	}
	return ids, dups
}

func toSet(xs []string) map[string]bool {
	out := make(map[string]bool, len(xs))
	for _, x := range xs {
		out[x] = true
	}
	return out
}

// WireframeAnchorsForDraft is a convenience wrapper that resolves
// wireframe.md relative to the draft's directory, mirroring the
// convention used everywhere in the recipe tree.
func WireframeAnchorsForDraft(draftPath string) []Issue {
	dir := filepath.Dir(draftPath)
	wireframe := filepath.Join(dir, "wireframe.md")
	return CheckWireframeAnchors(draftPath, wireframe)
}
