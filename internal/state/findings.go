package state

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Findings ledger — cross-round identity for verification findings.
//
// verification.md is overwritten every cycle and its findings are
// numbered positionally (#1, #2, #3), so "#4" in round 5 and "#4" in
// round 6 are unrelated. Nothing carried forward between rounds: a
// fresh verifier re-derived the whole document from scratch each time,
// re-raising points an earlier round had already settled, and
// `bts-verification-protocol.md`'s stagnation detector ("if the SAME
// finding IDs persist across max_iterations, do not retry") had no
// finding IDs to detect with.
//
// findings.jsonl is an append-only event log keyed by a content-derived
// ID. Folding it yields, per finding: how many rounds it has been open,
// when it was fixed, and whether a previously fixed finding came back.
//
// Identity is a hash of (document, normalised title). Two roundings of
// the same defect match only if the verifier phrases them consistently;
// a genuinely reworded finding gets a new ID and is treated as new.
// That is the conservative direction — a missed match costs one extra
// carry-forward line in the next prompt, whereas a false match would
// suppress a real finding.

// Finding statuses.
const (
	FindingOpen = "open" // reported by the latest round
	// FindingUnreported is an open finding that stopped being reported.
	// It is NOT a closure: absence is what a fix looks like, but it is
	// also what a verifier restating the same defect in different words
	// looks like, and what a verifier told to skip deliberately-open
	// items looks like.
	//
	// Identity is sha256(doc + normalised title), so a rephrased finding
	// hashes to a new ID while its predecessor goes unmatched. When
	// absence closed a finding outright, both of those produced the same
	// record as a real fix: one measured round reported "68 new, 27
	// fixed" where every one of the 27 was a restatement still present
	// in the document under a new ID, and the operator had to write the
	// correction into the changelog by hand. Across that recipe at least
	// 40 of 458 closures were false, and the two the operator caught are
	// only the ones anybody looked for.
	//
	// So absence demotes rather than closes. Promotion to fixed needs a
	// second consecutive silent round, and never happens while the same
	// anchor is still producing new findings — the signature of a
	// restatement rather than a repair.
	FindingUnreported = "unreported"
	FindingFixed      = "fixed"     // absent, and confirmed absent
	FindingDeferred   = "deferred"  // minor [deferred] — runtime watch-item, not an IMPROVE target
	FindingDismissed  = "dismissed" // adjudicated as not-a-defect; must not be re-raised
)

// NotClosed reports whether a status still owes the loop something —
// either a fix or an adjudication. Deferred and dismissed are settled;
// unreported is not.
func NotClosed(status string) bool {
	return status == FindingOpen || status == FindingUnreported
}

// FindingEvent is one append-only observation about one finding.
type FindingEvent struct {
	ID        string `json:"id"`
	Doc       string `json:"doc"`
	Iteration int    `json:"iteration"`
	Severity  string `json:"severity"`
	Title     string `json:"title"`
	Anchor    string `json:"anchor,omitempty"`
	Status    string `json:"status"`
	Reason    string `json:"reason,omitempty"` // why dismissed, when applicable
	// Dimensions is the measurement class of the round that produced this
	// event — which instruments read the document. See roundCovers: a
	// round can only be silent ABOUT a finding an instrument it ran could
	// have found. Empty means "written before this field existed".
	Dimensions []string `json:"dimensions,omitempty"`
	Timestamp  string   `json:"timestamp"`
}

// FindingState is the folded current state of one finding.
type FindingState struct {
	FindingEvent
	FirstIteration int `json:"first_iteration"`
	LastIteration  int `json:"last_iteration"`
	OpenRounds     int `json:"open_rounds"` // rounds reported open
	Reopened       int `json:"reopened"`    // times it went fixed → open again
}

// ReportedFinding is one entry of the <bts-findings> block's findings array.
type ReportedFinding struct {
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Anchor   string `json:"anchor,omitempty"`
}

// findingsPath returns the ledger path for a recipe.
func findingsPath(root, recipeID string) string {
	return filepath.Join(RecipeDir(root, recipeID), "findings.jsonl")
}

// NormalizeFindingTitle reduces a finding title to its identity form:
// lowercase, with every run of non-letter/non-digit characters collapsed
// to a single space, then truncated. Markdown emphasis, backticks,
// trailing periods and dash style therefore do not change the ID, so a
// finding restated with different formatting still matches.
//
// Classification is Unicode-aware rather than ASCII-only: specs in this
// project are frequently Korean, and CJK titles must normalise on the
// same rules. Note that this makes Unicode punctuation (em dash, curly
// quotes, ideographic comma) a separator, which is the intent — only
// letters and digits carry identity.
func NormalizeFindingTitle(s string) string {
	var b strings.Builder
	space := true // suppresses leading and repeated separators
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
			continue
		}
		if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	out := strings.TrimSpace(b.String())
	// Truncate on a rune boundary — a byte slice could split a
	// multi-byte character and make the ID depend on encoding luck.
	if runes := []rune(out); len(runes) > 160 {
		out = string(runes[:160])
	}
	return out
}

// FindingID derives the stable ID for a finding on a document.
// Same document + same normalised title → same ID, across rounds and
// across machines.
func FindingID(docBase, title string) string {
	sum := sha256.Sum256([]byte(filepath.Base(docBase) + "|" + NormalizeFindingTitle(title)))
	return "F-" + hex.EncodeToString(sum[:])[:8]
}

// AppendFindingEvents appends events to the ledger, stamping timestamps.
func AppendFindingEvents(root, recipeID string, events []FindingEvent) error {
	if len(events) == 0 {
		return nil
	}
	path := findingsPath(root, recipeID)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range events {
		if events[i].Timestamp == "" {
			events[i].Timestamp = now
		}
		if err := AppendJSONL(path, events[i]); err != nil {
			return err
		}
	}
	return nil
}

// ReadFindingEvents returns every ledger event in file order.
// A missing ledger returns nil, nil. Malformed lines are skipped.
func ReadFindingEvents(root, recipeID string) ([]FindingEvent, error) {
	f, err := os.Open(findingsPath(root, recipeID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var out []FindingEvent
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e FindingEvent
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

// FoldFindings collapses the event log into per-ID current state,
// counting open rounds and reopenings along the way.
//
// Events are raw observations — they record what a verifier reported.
// The fold is where adjudication policy applies: a dismissal is sticky,
// so a later round raising the point again does NOT clear it. Without
// that, one re-raise would flip the finding back to open, drop it out of
// the carry-forward block's DISMISSED section, and the next verifier
// would never learn the point was settled — making a dismissed finding
// re-litigable forever, which is the opposite of what `dismiss` is for.
func FoldFindings(events []FindingEvent) map[string]*FindingState {
	states := make(map[string]*FindingState, len(events))
	dismissals := make(map[string]FindingEvent)
	for _, e := range events {
		st, ok := states[e.ID]
		if !ok {
			st = &FindingState{FindingEvent: e, FirstIteration: e.Iteration}
			states[e.ID] = st
		}
		if e.Status == FindingDismissed {
			dismissals[e.ID] = e
		}
		_, dismissed := dismissals[e.ID]

		// A reopen is a settled finding being raised again: either a
		// fixed one coming back (the last IMPROVE regressed) or a
		// dismissed one being re-litigated. `bts recipe log` reports
		// both, so this view must count both or the two disagree.
		if e.Status == FindingOpen && (st.Status == FindingFixed || dismissed) {
			st.Reopened++
		}
		if e.Status == FindingOpen {
			st.OpenRounds++
		}

		// Later events win for the mutable fields; keep the original
		// FirstIteration and the accumulated counters.
		first, rounds, reopened := st.FirstIteration, st.OpenRounds, st.Reopened
		st.FindingEvent = e
		st.FirstIteration, st.OpenRounds, st.Reopened = first, rounds, reopened
		st.LastIteration = e.Iteration

		if d, ok := dismissals[e.ID]; ok {
			st.Status = FindingDismissed
			st.Reason = d.Reason
		}
	}
	return states
}

// sortedStates returns folded states in a stable order: severity rank,
// then longest-open first, then ID.
func sortedStates(states map[string]*FindingState) []*FindingState {
	out := make([]*FindingState, 0, len(states))
	for _, st := range states {
		out = append(out, st)
	}
	rank := map[string]int{
		"critical": 0, "major": 1, "minor_resolvable": 2,
		"minor_deferred": 3, "info": 4,
	}
	sort.Slice(out, func(i, j int) bool {
		ri, rj := rank[out[i].Severity], rank[out[j].Severity]
		if ri != rj {
			return ri < rj
		}
		if out[i].OpenRounds != out[j].OpenRounds {
			return out[i].OpenRounds > out[j].OpenRounds
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// LoadFindings returns folded findings for a recipe, optionally narrowed
// to one document (docBase == "" means all documents).
func LoadFindings(root, recipeID, docBase string) ([]*FindingState, error) {
	events, err := ReadFindingEvents(root, recipeID)
	if err != nil {
		return nil, err
	}
	states := FoldFindings(events)
	if docBase != "" {
		want := filepath.Base(docBase)
		for id, st := range states {
			if st.Doc != want {
				delete(states, id)
			}
		}
	}
	return sortedStates(states), nil
}

// SyncResult reports what one round changed in the ledger.
// Reclassification is one finding whose severity changed between the
// round that raised it and this one.
//
// The completion gate counts (critical, major, minor_resolvable), and
// the convergence budget judges progress by watching that triple fall.
// A finding re-rated from major to minor moves the triple without an
// edit, so a round that repaired nothing reads as a round that made
// progress. Measured on one recipe: F-946af22c was raised `major` by a
// verify round and re-raised `minor_resolvable` by a simulate round two
// rounds later, over a document nobody had touched in between.
//
// Neither rating is presumed correct — instruments legitimately weigh
// the same defect differently. What is reported is the movement, so the
// count is not mistaken for the work.
type Reclassification struct {
	ID   string
	From string
	To   string
}

// DuplicateCandidate is two findings on one anchor that name the same
// thing in different words.
//
// A finding's identity is the hash of its normalised title, so the same
// defect written twice is two findings with two IDs, counted twice by
// every gate downstream. Restating is not rare — it is what a second
// instrument does when it reaches the same hole from another direction,
// and in a project whose specs mix Korean and English the two
// descriptions may not share a single word.
//
// What they do share is the vocabulary that survives translation:
// filenames, identifiers, and numbers. `verify.sql` is `verify.sql` in
// either language. So a pair is nominated when it sits on one anchor and
// both titles name the same artefact or quantity. On the measured recipe
// that flagged 2 pairs out of 36 open findings across 6 anchors carrying
// more than one — both genuine, and both rated differently by the two
// instruments that raised them.
//
// One shared token is the bar, and it over-nominates: on the measured
// recipe three pairs were raised and two were genuine, the third being
// two unrelated defects that both named `ArtworkView`. Requiring two
// shared tokens was tried and is worse — it drops both real pairs,
// because the other thing they share is a section number, and single
// characters carry no identity so they are never tokens at all.
//
// Over-nominating is the right side to err on. Telling two findings
// apart is a reading of what they mean, which no token test can do, and
// the cost of the two outcomes is not symmetric: a nomination the
// operator rejects costs one glance, while a duplicate nobody spots is
// counted twice by every gate for the rest of the recipe.
//
// It is a nomination, never a merge. Deciding that two findings are one
// is a reading of what they mean, and that is the operator's call.
type DuplicateCandidate struct {
	Anchor string
	A, B   string   // finding IDs, in reported order
	Shared []string // the tokens both titles carry, sorted
}

// invariantTokenRe matches the parts of a finding title that a
// translation leaves alone: dotted artefacts (verify.sql, main.go),
// camelCase, PascalCase and snake_case identifiers, and bare
// numbers. Ordered
// longest-form-first because Go alternation is leftmost-first.
var invariantTokenRe = regexp.MustCompile(
	`[A-Za-z_][\w.]*\.[A-Za-z]{1,10}` +
		`|[A-Za-z][a-z0-9]*[A-Z][A-Za-z0-9_]*` +
		`|[A-Za-z_]{3,}_\w+` +
		`|\d+`)

// invariantTokens extracts the language-invariant tokens of a title.
// Single characters are dropped: a lone digit is a section number or a
// count, shared by half the findings in any document.
func invariantTokens(title string) map[string]bool {
	out := map[string]bool{}
	for _, m := range invariantTokenRe.FindAllString(title, -1) {
		if len(m) > 1 {
			out[m] = true
		}
	}
	return out
}

type SyncResult struct {
	New        []string // first time seen
	Carried    []string // open in the previous round and still open
	Unreported []string // open before, silent this round — NOT a closure
	Fixed      []string // silent for a second round on a quiet anchor — closed
	Restated   []string // silent, but their anchor is still producing findings
	Reopened   []string // fixed before, open again — the edit regressed
	Stagnant   []string // open for >= stagnantAfter consecutive rounds
	Unjudged   []string // silent, but this round ran no instrument that could have found them
	// Reclassified is severity movement without an edit: the count fell
	// (or rose) because an instrument re-rated a finding, not because
	// anyone fixed it.
	Reclassified []Reclassification
	// Duplicates are pairs this round reported on one anchor that name
	// the same artefact — one defect being counted twice.
	Duplicates []DuplicateCandidate
}

// SyncFindings reconciles one verification round against the ledger.
//
// reported is the findings array from that round's <bts-findings> block.
//
// Absence does not close anything. A finding that was open and is not
// reported this round is demoted to `unreported`; it becomes `fixed`
// only after a second consecutive silent round, and only once its anchor
// has stopped producing findings entirely. See FindingUnreported for why
// — absence is equally the signature of a repair and of a verifier
// restating the same defect in different words.
//
// Dismissed findings stay dismissed unless the verifier raises them
// again, which is recorded as a reopen so the loop can see that an
// adjudicated point is being re-litigated. A finding returning from
// `unreported` is NOT a reopen: nothing ever claimed it was fixed.
// roundCovers reports whether this round could have found st — the
// precondition for reading its silence as anything at all.
//
// The convergence budget already refuses to compare rounds of different
// measurement classes (NoProgressStreak, VerifyLogEntry.StrengthClass).
// The ledger did not, and the two gates it feeds — absence_is_not_closure
// and verification_not_passed — were defeated by that asymmetry. On one
// measured recipe three consecutive rounds ran verify, then audit, then
// simulate against a BYTE-IDENTICAL document (one doc_hash across all
// three), and three findings the first round raised — one of them
// CRITICAL — closed as `fixed` without anyone touching a line. An audit
// has no reason to restate a logical inconsistency; its silence about
// one is not evidence, and the ledger read it as a repair.
//
// Two conditions, both from StrengthClass:
//
//   - Dimensions. The round must have run every instrument that produced
//     the finding. A verify+audit+simulate round may close a verify
//     finding; a verify-only round may not close an audit one.
//   - Scope. A delta round read part of the document, so its silence
//     about the rest is the scope and not the finding's absence.
//
// A round declaring no dimensions is a legacy round: it cannot say what
// it ran, so it keeps the old behaviour rather than stalling every
// ledger written before the field existed. Same for a finding raised by
// one.
func roundCovers(round *VerifyLogEntry, st *FindingState) bool {
	if len(round.Dimensions) == 0 {
		return true
	}
	if !round.FullPass {
		return false
	}
	for _, d := range st.Dimensions {
		if !slices.Contains(round.Dimensions, d) {
			return false
		}
	}
	return true
}

// nominateDuplicates pairs findings reported on one anchor whose titles
// share a language-invariant token. See DuplicateCandidate.
//
// Anchorless findings are skipped: without a shared location the token
// alone is far too weak — two findings can both mention `main.go` and
// have nothing to do with each other.
func nominateDuplicates(docBase string, reported []ReportedFinding) []DuplicateCandidate {
	type entry struct {
		id     string
		tokens map[string]bool
	}
	byAnchor := map[string][]entry{}
	seen := map[string]bool{}
	for _, rf := range reported {
		if rf.Anchor == "" || rf.Severity == "minor_deferred" {
			continue
		}
		id := FindingID(docBase, rf.Title)
		if seen[id] {
			continue
		}
		seen[id] = true
		byAnchor[rf.Anchor] = append(byAnchor[rf.Anchor],
			entry{id: id, tokens: invariantTokens(rf.Title)})
	}

	var out []DuplicateCandidate
	anchors := make([]string, 0, len(byAnchor))
	for a := range byAnchor {
		anchors = append(anchors, a)
	}
	sort.Strings(anchors)
	for _, anchor := range anchors {
		group := byAnchor[anchor]
		for i := 0; i < len(group); i++ {
			for j := i + 1; j < len(group); j++ {
				var shared []string
				for t := range group[i].tokens {
					if group[j].tokens[t] {
						shared = append(shared, t)
					}
				}
				if len(shared) == 0 {
					continue
				}
				sort.Strings(shared)
				out = append(out, DuplicateCandidate{
					Anchor: anchor, A: group[i].id, B: group[j].id,
					Shared: shared,
				})
			}
		}
	}
	return out
}

func SyncFindings(root, recipeID string, round *VerifyLogEntry, reported []ReportedFinding, stagnantAfter int) (*SyncResult, error) {
	docBase := filepath.Base(round.Doc)
	iteration := round.Iteration
	events, err := ReadFindingEvents(root, recipeID)
	if err != nil {
		return nil, err
	}
	prior := FoldFindings(events)

	res := &SyncResult{}
	seen := make(map[string]bool, len(reported))
	hotAnchor := make(map[string]bool)
	var toAppend []FindingEvent

	for _, rf := range reported {
		id := FindingID(docBase, rf.Title)
		if seen[id] {
			continue // duplicate title within one round
		}
		seen[id] = true

		status := FindingOpen
		if rf.Severity == "minor_deferred" {
			status = FindingDeferred
		}
		ev := FindingEvent{
			ID: id, Doc: docBase, Iteration: iteration,
			Severity: rf.Severity, Title: rf.Title, Anchor: rf.Anchor,
			Status: status, Dimensions: round.Dimensions,
		}
		toAppend = append(toAppend, ev)

		// An anchor is "hot" while it is still producing live findings of
		// any age. Restricting this to findings that are NEW this round
		// made the hold last exactly one round: a restatement is new when
		// the original goes silent, but by the next round it is merely
		// carried, the anchor reads as quiet, and the original closes as
		// `fixed` — the outcome this whole mechanism exists to prevent.
		//
		// Deferred items do not make an anchor hot. They are accepted
		// watch-items carried into implement by design, so an anchor
		// holding nothing else has gone quiet.
		if rf.Anchor != "" && rf.Severity != "minor_deferred" {
			hotAnchor[rf.Anchor] = true
		}

		if st, ok := prior[id]; ok && st.Severity != "" && st.Severity != rf.Severity {
			// Same finding, different weight. Report the movement so the
			// falling count is not read as work done.
			res.Reclassified = append(res.Reclassified,
				Reclassification{ID: id, From: st.Severity, To: rf.Severity})
		}

		switch st, ok := prior[id]; {
		case !ok:
			res.New = append(res.New, id)
		case st.Status == FindingFixed || st.Status == FindingDismissed:
			res.Reopened = append(res.Reopened, id)
		default:
			res.Carried = append(res.Carried, id)
			if stagnantAfter > 0 && st.OpenRounds+1 >= stagnantAfter {
				res.Stagnant = append(res.Stagnant, id)
			}
		}
	}

	// Nominate same-anchor pairs whose titles name the same artefact.
	// Built from what THIS round reported, because that is the set the
	// operator is about to act on, and because a pair only costs a double
	// count while both halves are live.
	res.Duplicates = nominateDuplicates(docBase, reported)

	// hotAnchor was filled as the reported findings were classified
	// above. A finding that vanished from an anchor which is still
	// producing findings is the shape a restatement makes, so it is not
	// promoted out of unreported while that stays true. It closes when
	// the anchor itself goes quiet, which is the honest signal that the
	// section was actually repaired rather than reworded.

	// Anything not reported this round is demoted, not closed. Deferred
	// items persist by design (runtime watch-items carried into
	// implement), and dismissed ones stay dismissed.
	for id, st := range prior {
		if st.Doc != docBase || seen[id] {
			continue
		}
		if !roundCovers(round, st) {
			// This round ran no instrument that could have raised it. Its
			// silence says nothing, so the finding keeps the state it had.
			if st.Status == FindingOpen || st.Status == FindingUnreported {
				res.Unjudged = append(res.Unjudged, id)
			}
			continue
		}
		switch {
		case st.Status == FindingOpen:
			// First silent round: record the absence, claim nothing.
			toAppend = append(toAppend, FindingEvent{
				ID: id, Doc: docBase, Iteration: iteration,
				Severity: st.Severity, Title: st.Title, Anchor: st.Anchor,
				Status: FindingUnreported,
			})
			res.Unreported = append(res.Unreported, id)
		case st.Status == FindingUnreported && !hotAnchor[st.Anchor]:
			// Second consecutive silent round on a quiet anchor: closed.
			toAppend = append(toAppend, FindingEvent{
				ID: id, Doc: docBase, Iteration: iteration,
				Severity: st.Severity, Title: st.Title, Anchor: st.Anchor,
				Status: FindingFixed,
			})
			res.Fixed = append(res.Fixed, id)
		case st.Status == FindingUnreported:
			// Still silent, but its anchor is still generating findings.
			// Hold it — this is the restatement signature.
			res.Restated = append(res.Restated, id)
		}
	}

	sort.Strings(res.New)
	sort.Strings(res.Carried)
	sort.Strings(res.Unreported)
	sort.Strings(res.Fixed)
	sort.Strings(res.Restated)
	sort.Strings(res.Unjudged)
	sort.Strings(res.Reopened)
	sort.Strings(res.Stagnant)

	if err := AppendFindingEvents(root, recipeID, toAppend); err != nil {
		return nil, err
	}
	return res, nil
}

// DismissFinding records an adjudication that a finding is not a defect,
// so later rounds can be told not to raise it again.
func DismissFinding(root, recipeID, id, reason string) error {
	events, err := ReadFindingEvents(root, recipeID)
	if err != nil {
		return err
	}
	st, ok := FoldFindings(events)[id]
	if !ok {
		return fmt.Errorf("unknown finding %s", id)
	}
	return AppendFindingEvents(root, recipeID, []FindingEvent{{
		ID: id, Doc: st.Doc, Iteration: st.LastIteration,
		Severity: st.Severity, Title: st.Title, Anchor: st.Anchor,
		Status: FindingDismissed, Reason: reason,
	}})
}

// CarryForwardBlock renders the adjudicated-findings context injected
// into the next round's verifier prompt. Returns "" when there is
// nothing to carry, so callers can omit the section entirely.
func CarryForwardBlock(states []*FindingState) string {
	var open, unreported, fixed, dismissed, deferred []*FindingState
	for _, st := range states {
		switch st.Status {
		case FindingOpen:
			open = append(open, st)
		case FindingUnreported:
			unreported = append(unreported, st)
		case FindingFixed:
			fixed = append(fixed, st)
		case FindingDismissed:
			dismissed = append(dismissed, st)
		case FindingDeferred:
			deferred = append(deferred, st)
		}
	}
	if len(states) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Adjudicated findings from previous rounds\n\n")
	b.WriteString("These were already raised on this document. Re-raise one ONLY if the\n")
	b.WriteString("text it refers to changed since — reuse its ID verbatim in your title\n")
	b.WriteString("so the ledger can track it. Do not re-derive settled points.\n\n")

	section := func(title, note string, list []*FindingState) {
		if len(list) == 0 {
			return
		}
		fmt.Fprintf(&b, "### %s (%d) — %s\n", title, len(list), note)
		for _, st := range list {
			fmt.Fprintf(&b, "- %s [%s] %s", st.ID, st.Severity, st.Title)
			if st.OpenRounds > 1 {
				fmt.Fprintf(&b, "  (open %d rounds)", st.OpenRounds)
			}
			if st.Reopened > 0 {
				fmt.Fprintf(&b, "  (reopened %dx)", st.Reopened)
			}
			if st.Reason != "" {
				fmt.Fprintf(&b, "  — dismissed: %s", st.Reason)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	section("STILL OPEN", "expect these unless the fix landed", open)
	section("UNCONFIRMED", "last round went silent on these without closing them — "+
		"say explicitly whether each is fixed, and reuse its exact recorded title if it is not", unreported)
	section("FIXED", "report again only if the fix was reverted or is wrong", fixed)
	section("DISMISSED", "adjudicated as not-a-defect; do NOT re-raise", dismissed)
	section("DEFERRED", "runtime watch-items; not IMPROVE targets", deferred)
	return b.String()
}
