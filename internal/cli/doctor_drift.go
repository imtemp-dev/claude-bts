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

// checkUnrecordedRevisions reports rounds that recorded no doc_hash.
//
// checkUnenforceableRule3 above asks whether the LAST FULL PASS left a
// hash, which is the right question for whether the gate can fire at
// all. It is the wrong question for whether the gate has been firing.
// A measured recipe recorded hashes for iterations 1-27 and none for
// 28-34: the last full pass still had one, so nothing reported a
// problem, while rule 3 sat inert for the final seven rounds — 21% of
// the run — because stampContentHashes fails to stderr and both gates
// fall back rather than manufacture a verdict.
//
// Falling back is right. Falling back silently is not.
func checkUnrecordedRevisions(root, recipeID string) []doctorIssue {
	entries, err := state.ReadVerifyLog(root, recipeID)
	if err != nil {
		return nil
	}
	// Only the recent tail matters: an old gap is history, a current one
	// means the next completion check has nothing to stand on.
	const window = 5
	start := len(entries) - window
	if start < 0 {
		start = 0
	}
	examined := len(entries) - start
	missing := 0
	var last int
	for i := start; i < len(entries); i++ {
		if entries[i].Doc == "" {
			continue // unscoped legacy round; nothing to hash against
		}
		if entries[i].DocHash == "" {
			missing++
			last = entries[i].Iteration
		}
	}
	if missing == 0 {
		return nil
	}
	return []doctorIssue{{
		level:   "warning",
		section: "verification",
		message: fmt.Sprintf(
			"%d of the last %d verify rounds recorded no doc_hash (most recently iteration %d), so rule 3 was not enforced on them and completion has no revision to confirm against",
			missing, examined, last),
		fix: "run `bts recipe log` from the project root with a --doc path that resolves from there — a relative --doc resolved against another directory hashes nothing and only warns on stderr",
	}}
}

// checkActiveOverrides surfaces every hard gate this recipe is currently
// bypassing.
//
// An override is a legitimate operator decision, so this is not an error
// — but it must never be invisible. A measured recipe finalized past two
// hard gates with seven majors open, and `bts doctor` reported a healthy
// recipe, because the decisions lived only as prose in changelog.jsonl.
func checkActiveOverrides(root, recipeID string) []doctorIssue {
	records, err := state.ReadOverrides(root, recipeID)
	if err != nil || len(records) == 0 {
		return nil
	}
	var issues []doctorIssue
	for _, r := range state.LiveOverrides(records) {
		what := r.Gate
		if r.Doc != "" {
			what += " on " + r.Doc
		}
		if n := len(r.Findings); n > 0 {
			what += fmt.Sprintf(", excusing %d finding(s)", n)
		}
		pin := "not pinned to a revision — it keeps applying as the document changes"
		if r.DocHash != "" {
			pin = "pinned to the revision it was granted on"
		}
		issues = append(issues, doctorIssue{
			level:   "warning",
			section: "verification",
			message: fmt.Sprintf("gate override in force: %s (%s) — %s",
				what, pin, state.TruncateRunes(strings.ReplaceAll(r.Reason, "\n", " "), 120)),
			fix: "keep it if the judgement still holds, or `bts recipe override revoke " + recipeID +
				" --gate " + r.Gate + " --reason \"...\"` to put the gate back",
		})
	}
	return issues
}

// checkLedgerAgreesWithVerifyLog compares the two counts of the same
// thing.
//
// verify-log.jsonl carries a round's severity totals; findings.jsonl
// carries the individual findings behind them. Nothing reconciled the
// two, and they drifted: at one recipe's finalization the folded ledger
// held 8 open majors and 22 resolvable minors while the last verify
// round recorded 7 and 18. Both numbers were quoted as authoritative in
// different places, and the gap is exactly the size of a false closure
// nobody noticed.
func checkLedgerAgreesWithVerifyLog(root, recipeID string) []doctorIssue {
	entries, err := state.ReadVerifyLog(root, recipeID)
	if err != nil || len(entries) == 0 {
		return nil
	}
	last := entries[len(entries)-1]
	if last.Doc == "" {
		return nil // legacy unscoped round; the ledger is not doc-scoped either
	}
	states, ferr := state.LoadFindings(root, recipeID, last.Doc)
	if ferr != nil || len(states) == 0 {
		return nil // no ledger for this document; nothing to reconcile
	}
	var critical, major, minorR int
	for _, st := range states {
		// `open` only. An `unreported` finding is one the round did not
		// report — that is what the status means — so counting it
		// against the round's totals made this check fire on the very
		// behaviour absence_is_not_closure exists to produce. It would
		// have warned on any recipe where a single finding went quiet
		// for one round.
		if st.Status != state.FindingOpen {
			continue
		}
		switch st.Severity {
		case "critical":
			critical++
		case "major":
			major++
		case "minor_resolvable":
			minorR++
		}
	}
	if critical == last.Critical && major == last.Major && minorR == last.EffectiveResolvable() {
		return nil
	}
	return []doctorIssue{{
		level:   "warning",
		section: "verification",
		message: fmt.Sprintf(
			"the findings ledger and verify-log disagree about %s: ledger has %d critical / %d major / %d minor_resolvable still owed, round %d recorded %d / %d / %d",
			last.Doc, critical, major, minorR,
			last.Iteration, last.Critical, last.Major, last.EffectiveResolvable()),
		fix: "the ledger is the itemised record and the log is the round total — reconcile with `bts recipe findings list " +
			recipeID + " --doc " + last.Doc + " --open`. A gap usually means a round was logged without its " +
			"<bts-findings> array, so the ledger never saw the findings the totals claim",
	}}
}

// checkDebateTreeDivergence reports debates that exist in both the
// project-level and recipe-level trees with different content.
//
// `bts debate log` writes rounds to .bts/specs/debates/{id}/, while
// /bts-debate writes its round markdown under the recipe and the
// manifest references it there. Both trees are legitimate; two copies of
// the SAME debate that disagree are not. A measured project carried both
// for two debates and all six round files differed, with the machine
// state (debate.json) in one tree and a prep round in the other — so
// "what was decided" depended on which copy the reader opened.
func checkDebateTreeDivergence(root, recipeID string) []doctorIssue {
	recipeTree := filepath.Join(state.RecipeDir(root, recipeID), "debates")
	projectTree := filepath.Join(state.SpecsPath(root), "debates")

	entries, err := os.ReadDir(recipeTree)
	if err != nil {
		return nil
	}
	var issues []doctorIssue
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		a := filepath.Join(recipeTree, e.Name())
		b := filepath.Join(projectTree, e.Name())
		if _, serr := os.Stat(b); serr != nil {
			continue // only one copy — nothing to disagree with
		}
		diverged := divergentFiles(a, b)
		if len(diverged) == 0 {
			continue
		}
		sort.Strings(diverged)
		issues = append(issues, doctorIssue{
			level:   "warning",
			section: "documents",
			message: fmt.Sprintf(
				"debate %q exists in both %s and %s, and %s differ — which copy records what was decided depends on which one is opened",
				e.Name(), recipeTree, projectTree, strings.Join(diverged, ", ")),
			fix: "keep one. The manifest references the recipe-level copy; `bts debate list` reads the project-level one. " +
				"Reconcile them, then delete the copy you are not keeping",
		})
	}
	return issues
}

// divergentFiles returns the names present in both directories whose
// contents differ.
func divergentFiles(a, b string) []string {
	entries, err := os.ReadDir(a)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		// state.ContentHash, not bytes.Equal: it normalises line endings,
		// and every other content comparison in bts goes through it. A
		// raw byte compare reports two copies as divergent when the only
		// difference is CRLF, which is noise on a checkout, not a
		// disagreement about what was decided.
		ha, oka, erra := state.FileContentHash(filepath.Join(a, e.Name()))
		hb, okb, errb := state.FileContentHash(filepath.Join(b, e.Name()))
		if erra != nil || errb != nil || !oka || !okb {
			continue
		}
		if ha != hb {
			out = append(out, e.Name())
		}
	}
	return out
}
