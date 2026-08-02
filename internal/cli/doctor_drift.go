package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Config-drift checks. settings.yaml and .mcp.json are user-owned
// (preserved by `bts update`), so template-default changes never reach
// existing projects — doctor is where that drift becomes visible.

// activeReviewerSecurityRe matches an UNCOMMENTED reviewer_security
// override inside the agents block (two-space indent per template).
var activeReviewerSecurityRe = regexp.MustCompile(`(?m)^\s+reviewer_security\s*:\s*\S`)

// checkConfigDrift inspects user-owned config files for stale defaults.
// All findings are warnings — the user may have set them intentionally.
func checkConfigDrift(root string) []doctorIssue {
	var issues []doctorIssue

	settingsPath := filepath.Join(root, ".bts", "config", "settings.yaml")
	if data, err := os.ReadFile(settingsPath); err == nil {
		if activeReviewerSecurityRe.Match(data) {
			issues = append(issues, doctorIssue{
				level:   "warning",
				section: "config",
				message: "settings.yaml has an active `reviewer_security:` override — the default moved to the session model in v0.9.x (security review is recall-bound; sonnet misses semantic flaws the rebuttal can't rescue)",
				fix:     "comment the line out to adopt the session-model default, or keep it if the override is intentional",
			})
		}
	}

	mcpPath := filepath.Join(root, ".mcp.json")
	if data, err := os.ReadFile(mcpPath); err == nil {
		content := string(data)
		if strings.Contains(content, "context7") && !strings.Contains(content, "CONTEXT7_API_KEY") {
			issues = append(issues, doctorIssue{
				level:   "warning",
				section: "config",
				message: ".mcp.json runs Context7 without the CONTEXT7_API_KEY passthrough — anonymous free tier only (IP rate limits)",
				fix:     "copy the ${CONTEXT7_API_KEY:+--api-key ...} args from the current template .mcp.json; export the key in your shell profile to raise limits",
			})
		}
	}

	return issues
}

// checkTestResultsProvenance warns when test-results.json was written
// by hand instead of `bts test run` — the DONE gates trust its status
// field, and only recorded_by=="bts" means the status came from the
// actual exit code.
func checkTestResultsProvenance(root, recipeID string) []doctorIssue {
	tr, err := state.LoadTestResults(root, recipeID)
	if err != nil {
		return nil // no test results yet — nothing to check
	}
	if tr.RecordedBy == "bts" {
		return nil
	}
	return []doctorIssue{{
		level:   "warning",
		section: "tests",
		message: fmt.Sprintf("test-results.json is hand-recorded (status=%s without recorded_by:\"bts\") — the DONE gates are trusting a transcription, not an exit code", tr.Status),
		fix:     fmt.Sprintf("re-run via `bts test run %s --cmd \"<test command>\"` so status is machine-derived", recipeID),
	}}
}

// checkDirtyVerifiedDocs surfaces rule-3 violations (doc modified after
// its last verification) in doctor output, same detection as the stop
// hook's DONE gate.
func checkDirtyVerifiedDocs(root, recipeID string) []doctorIssue {
	dirty, err := state.DirtyVerifiedDocs(root, recipeID)
	if err != nil || len(dirty) == 0 {
		return nil
	}
	return []doctorIssue{{
		level:   "warning",
		section: "verification",
		message: fmt.Sprintf("%s modified after last verification (rule 3)", strings.Join(dirty, ", ")),
		fix:     "run /bts-verify on the modified doc(s), then `bts recipe log " + recipeID + " --from-verification ... --doc <path>`",
	}}
}

// checkUnenforceableRule3 reports documents whose rule-3 gate is
// currently inert.
//
// A document is enforceable when its last full pass recorded a content
// hash, or when its local snapshot is still on disk. Neither holds for a
// round logged before hashes existed and then carried into a fresh clone
// or worktree — .bts/local/ is gitignored, so nothing came across. The
// gate does not fire, and silence there reads exactly like "verified and
// unchanged". This check is what makes the difference visible; one full
// pass re-arms it.
func checkUnenforceableRule3(root, recipeID string) []doctorIssue {
	entries, err := state.ReadVerifyLog(root, recipeID)
	if err != nil || len(entries) == 0 {
		return nil
	}
	hashed := state.LastFullPassHashes(entries)

	seen := map[string]bool{}
	var gaps []string
	for i := range entries {
		doc := entries[i].Doc
		if doc == "" || !entries[i].FullPass || seen[doc] {
			continue
		}
		seen[doc] = true
		if _, ok := hashed[doc]; ok {
			continue
		}
		if _, serr := os.Stat(state.VerifySnapshotPath(root, recipeID, doc)); serr == nil {
			continue
		}
		gaps = append(gaps, doc)
	}
	if len(gaps) == 0 {
		return nil
	}
	sort.Strings(gaps)
	return []doctorIssue{{
		level:   "warning",
		section: "verification",
		message: fmt.Sprintf(
			"rule 3 is unenforceable for %s: the round that verified it recorded no content hash and its local snapshot is absent, so an edit since then would pass every gate unnoticed",
			strings.Join(gaps, ", ")),
		fix: "run one full pass to re-arm it: /bts-verify, then `bts recipe log " + recipeID + " --from-verification ... --doc <path> --scope full`",
	}}
}
