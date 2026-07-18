package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
