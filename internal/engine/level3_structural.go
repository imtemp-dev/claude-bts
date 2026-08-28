package engine

import (
	"regexp"
	"strings"
)

// Structural predicates for the level criteria.
//
// Each answers a yes/no question about the document's shape, and each
// saturates: a document that satisfies one cannot satisfy it harder by
// growing. See level3Criteria in consistency_checker.go for why that
// property is the point.

var (
	// A path-like token: a known source extension, or two or more
	// slash-separated segments. Backticks and table pipes are stripped by
	// the caller before matching.
	pathTokenRe = regexp.MustCompile(
		`(?i)[\w.@/-]*\w\.(?:ts|tsx|js|jsx|go|py|rs|java|kt|kts|swift|rb|c|h|cc|cpp|cs|sql|ya?ml|json|sh|proto|graphql)\b` +
			`|\b[\w.-]+(?:/[\w.-]+){1,}`)

	// Invariant identifiers. domain.md and the blueprint both use INV-NNN;
	// CheckInvariantOwnership reads the same shape. Group 1 is the ID as
	// written (for reporting), group 2 the digits without leading zeros
	// (for matching, so INV-007 and INV-7 are one invariant).
	invariantIDRe = regexp.MustCompile(`(?i)\b(INV-0*(\d+))\b`)

	// A line that names an owner: it carries a path.
	ownerTokenRe = pathTokenRe

	// A falsifier has to be a NAMED thing that can go red — a test file, a
	// spec, a probe, a command. The word alone is not enough: on one
	// measured draft a single cross-reference line listed all nine
	// invariants and contained the word "테스트", which marked seven of
	// them covered by an index entry that falsifies nothing.
	//
	// So a line names a falsifier when it carries BOTH a falsifier word
	// and an artifact that could be run or read — a path, or a backticked
	// identifier.
	//
	// The camelCase alternative is case-SENSITIVE on purpose: `CoverTests`
	// has no word boundary before "Tests", and a case-insensitive
	// `\w*tests?\b` would swallow "latest".
	falsifierWordRe = regexp.MustCompile(
		`(?i)\btests?\b|\bspec\b|assert|expect|probe|falsif|` +
			`테스트|반증|프로브|관측` +
			`|(?-i:[a-z]Tests?\b)`)

	// A heading that names something crossing a boundary.
	//
	// The word boundaries wrap only the ASCII alternatives. RE2's `\b` is
	// defined against ASCII `\w`, so a Korean word never has one adjacent
	// to it and `\b계약\b` cannot match at all — a Korean-language spec
	// could not satisfy this criterion at any length. English keeps its
	// boundaries so "api" does not match inside "rapid".
	boundaryHeadingRe = regexp.MustCompile(
		`(?im)^#{1,6}[^\n]*(?:\b(?:contract|wire|schema|payload|dto|endpoint|` +
			`migration|api|interface)\b|계약|스키마|와이어|마이그레이션|경계|인터페이스)`)

	// Table rows and fenced lines both count as a declared shape: the
	// question is whether the shape is pinned somewhere, not how.
	tableRowRe = regexp.MustCompile(`(?m)^\s*\|.*\|\s*$`)
	fenceRe    = regexp.MustCompile("(?m)^\\s*```")

	// An ordered step: `S-1`, `Step 2`, `3.` at the start of a line or
	// heading.
	orderedStepRe = regexp.MustCompile(`(?im)^\s*#{0,6}\s*(?:S-\d+|step\s+\d+|\d+[.)])\s+\S`)

	// Something that undoes a step.
	rollbackRe = regexp.MustCompile(
		`(?i)rollback|roll back|revert|undo|down migration|되돌리|롤백|복구`)

	// The line that says what would settle an uncertainty before anyone
	// writes code. `Why-deferred:` is the existing convention
	// (bts-verification-protocol.md § Severity Classification);
	// `Opens-with:` names the command that settles it.
	//
	// This is a different question from uncertaintyResolRe, which asks
	// whether an uncertainty was settled DURING implementation. The
	// section itself and its entries are matched with uncertaintySectionRe
	// and uncertaintyHeadingRe from uncertainty_checker.go, so the shape
	// the stop hook and /bts-implement parse is the shape scored here.
	deferralMarkerRe = regexp.MustCompile(`(?im)^\s*[*_>-]*\s*(?:why-deferred|opens-with)\s*:\s*\S`)
)

// minNamedUnits is the point past which naming more files says nothing
// further about whether the reader knows what this spec touches.
const minNamedUnits = 3

// hasNamedUnits reports whether the document names at least a few
// distinct files or paths.
func hasNamedUnits(content string) bool {
	seen := make(map[string]bool, minNamedUnits)
	for _, m := range pathTokenRe.FindAllString(content, -1) {
		seen[strings.ToLower(m)] = true
		if len(seen) >= minNamedUnits {
			return true
		}
	}
	return false
}

// invariantsCarry reports whether the document declares at least one
// invariant AND every distinct invariant it declares appears on some
// line that also matches want.
//
// "On some line" is deliberate: both the owner table and the falsifier
// table put the invariant and its answer in the same row, which is the
// shape that makes the pairing checkable at all. An invariant listed in
// one place and answered in another prose paragraph is exactly the
// arrangement that lets an owner go missing without anyone noticing.
func invariantsCarry(content string, want func(string) bool) bool {
	declared := make(map[string]bool)
	satisfied := make(map[string]bool)
	for _, line := range strings.Split(content, "\n") {
		ids := invariantIDRe.FindAllStringSubmatch(line, -1)
		if len(ids) == 0 {
			continue
		}
		lineHasWant := want(line)
		for _, id := range ids {
			declared[id[2]] = true
			if lineHasWant {
				satisfied[id[2]] = true
			}
		}
	}
	if len(declared) == 0 {
		return false
	}
	return len(satisfied) == len(declared)
}

// lineNamesOwner reports whether a line names the file that keeps an
// invariant.
func lineNamesOwner(line string) bool {
	return ownerTokenRe.MatchString(line)
}

// lineNamesFalsifier reports whether a line names something that could
// go red for an invariant — a falsifier word plus the artifact it refers
// to. Either half alone is an index entry, not a falsifier.
func lineNamesFalsifier(line string) bool {
	if !falsifierWordRe.MatchString(line) {
		return false
	}
	return pathTokenRe.MatchString(line) || backtickedIdentRe.MatchString(line)
}

// minPinnedShapeLines is how much pinned shape — table rows, fenced
// lines — a boundary section has to carry before it is a declaration
// rather than a mention.
const minPinnedShapeLines = 3

// hasBoundaryContract reports whether the document names a boundary and
// pins a shape for it. What crosses a boundary is the expensive thing to
// get wrong: both sides get rebuilt, and once shipped a migration is
// involved.
func hasBoundaryContract(content string) bool {
	if !boundaryHeadingRe.MatchString(content) {
		return false
	}
	pinned := len(tableRowRe.FindAllString(content, -1))
	if pinned < minPinnedShapeLines {
		pinned += len(fenceRe.FindAllString(content, -1))
	}
	return pinned >= minPinnedShapeLines
}

// hasIrreversibleOrder reports whether the document sequences its steps
// and says what undoes them. A wrong order here is not a code fix: it is
// a production incident with no rollback, which is why it belongs in a
// blueprint and a function signature does not.
func hasIrreversibleOrder(content string) bool {
	return len(orderedStepRe.FindAllString(content, -1)) >= 2 &&
		rollbackRe.MatchString(content)
}

// hasDeclaredUncertainties reports whether the document has an
// uncertainties section and, for every uncertainty it lists, a line
// saying what would settle it.
//
// A document with the section and no entries passes. Declaring "nothing
// is open" is a claim the reader can act on; having no section at all is
// silence, and silence is what lets an unopened question read as a
// settled one.
func hasDeclaredUncertainties(content string) bool {
	if !uncertaintySectionRe.MatchString(content) {
		return false
	}
	entries := len(uncertaintyHeadingRe.FindAllString(content, -1))
	markers := len(deferralMarkerRe.FindAllString(content, -1))
	return markers >= entries
}

// ---------------------------------------------------------------------
// Level 1 and 2 predicates.
//
// These were keyword counters — "met" required two or more hits from a
// fixed vocabulary anywhere in the document. Two properties made that
// the engine of the problem this file exists to fix.
//
// First, the threshold is over an UNBOUNDED text, so any document long
// enough eventually clears it by accident. A 2,184-line draft passed
// tech_stack_specified because the words "node" and "postgresql" each
// occurred somewhere in it; the 90-line skeleton of the same design
// failed, and so did every honest short document.
//
// Second, the vocabulary named one stack — typescript, python, go,
// react, node, express, django, postgresql, redis. A Swift and Supabase
// project could not satisfy it at any length by describing itself
// accurately.
//
// And because level gates cascade (L1 >= 0.7 unlocks L2, both unlock
// L3), failing these made Level 3 unreachable. `bts verify` reported the
// unmet criteria, /bts-assess turned each into an IMPROVE instruction,
// and the only instruction that ever worked was "write more". Length was
// not merely rewarded here; it was the entry fee.
//
// Each predicate below reads structure and saturates. A document either
// has a dependency order or it does not, and stating it twice does not
// state it harder.

var (
	// A flow arrow as written in prose or a table. Distinct from
	// mermaid_graph.go's flowArrowRe, which parses mermaid edge syntax.
	proseArrowRe = regexp.MustCompile(`→|->|=>|⟶`)
	// A mermaid diagram that carries a flow.
	mermaidFlowRe = regexp.MustCompile(
		`(?im)^\s*(?:flowchart|graph|sequencediagram|statediagram)`)
	// A dependency relation: an explicit column, or prose naming one.
	dependencyRe = regexp.MustCompile(
		`(?i)depends\s*on|dependenc|imports?\s+from|\bcalls\b|의존|호출|선행|prerequisite`)
	// A backticked identifier — the way specs name a unit that is not a path.
	backtickedIdentRe = regexp.MustCompile("`([A-Za-z_][\\w.:<>()-]*)`")
	// Something going wrong.
	failureTermRe = regexp.MustCompile(
		`(?i)\berrors?\b|\bfail(?:ure|s|ed)?\b|exception|invalid|reject|` +
			`에러|오류|실패|거부`)
	// What is done about it.
	dispositionRe = regexp.MustCompile(
		`(?i)\b[45]\d{2}\b|fallback|retry|degrade|surface|swallow|propagate|` +
			`폴백|재시도|무시|전파|되돌|기본값`)
	// A recorded choice between alternatives.
	rationaleRe = regexp.MustCompile(
		`(?i)architect-decision|\bchosen\b|\bchose\b|\brationale\b|` +
			`\bbecause\b|\binstead of\b|\brather than\b|` +
			`대신|이유는|근거|선택한|채택`)
	alternativesRe = regexp.MustCompile(
		`(?i)alternativ|option\s*[ab12]|trade-?off|대안|선택지|절충`)
)

// minNamedComponents is the point at which a document has named enough
// parts to be describing a system rather than a single thing.
const minNamedComponents = 2

// namedUnits collects the distinct things a document names — file paths
// and backticked identifiers both count, because a spec names a database
// column or an RPC argument the same way it names a file.
func namedUnits(content string) map[string]bool {
	seen := make(map[string]bool)
	for _, m := range pathTokenRe.FindAllString(content, -1) {
		seen[strings.ToLower(m)] = true
	}
	for _, m := range backtickedIdentRe.FindAllStringSubmatch(content, -1) {
		seen[strings.ToLower(m[1])] = true
	}
	return seen
}

// hasNamedComponents reports whether the document names at least a
// couple of distinct parts.
func hasNamedComponents(content string) bool {
	return len(namedUnits(content)) >= minNamedComponents
}

// hasRelationships reports whether the document says how its parts are
// related — a dependency column, flow arrows, or an ordered sequence.
// Listing parts without relating them is a glossary, not an
// understanding.
func hasRelationships(content string) bool {
	return dependencyRe.MatchString(content) ||
		len(proseArrowRe.FindAllString(content, -1)) >= 2 ||
		len(orderedStepRe.FindAllString(content, -1)) >= 2
}

// knownStackTokens is a fallback for documents that name a technology
// without naming a file in it. It is deliberately not the primary
// signal: the extensions of the files a spec actually touches say what
// the stack is with more precision than any word list, and a word list
// can only ever be wrong about the stacks it forgot.
var knownStackTokens = regexp.MustCompile(
	`(?i)\btypescript\b|\bjavascript\b|\bpython\b|\bgolang\b|\bswift\b|` +
		`\bkotlin\b|\brust\b|\bjava\b|\bruby\b|\bc\+\+\b|\bnode(?:\.js)?\b|` +
		`\breact\b|\bvue\b|\bsvelte\b|\bswiftui\b|\buikit\b|\bjetpack\b|` +
		`\bexpress\b|\bnest(?:js)?\b|\bdjango\b|\bflask\b|\brails\b|\bspring\b|` +
		`\bpostgres(?:ql)?\b|\bmysql\b|\bsqlite\b|\bredis\b|\bmongo(?:db)?\b|` +
		`\bsupabase\b|\bfirebase\b|\bgraphql\b|\bgrpc\b`)

// minStackExtensions is how many distinct source-file kinds imply a
// stack has been pinned. One extension is a file; two is a stack with a
// boundary in it, which is the thing a reader needs to know.
const minStackExtensions = 2

// hasTechStack reports whether the document pins what it is built on.
// Preference goes to the extensions of the files it names, because those
// are evidence rather than assertion.
func hasTechStack(content string) bool {
	exts := make(map[string]bool, minStackExtensions)
	for _, m := range pathTokenRe.FindAllString(content, -1) {
		if i := strings.LastIndex(m, "."); i >= 0 && i < len(m)-1 {
			exts[strings.ToLower(m[i+1:])] = true
		}
	}
	if len(exts) >= minStackExtensions {
		return true
	}
	// The word list is a fallback for prose, so it runs on the prose. Left
	// on the raw text it fires on the extensions themselves — `\bswift\b`
	// matches inside `only/one.swift`, since a dot is a word boundary —
	// which would make one named file satisfy a criterion that asks for a
	// stack.
	return knownStackTokens.MatchString(pathTokenRe.ReplaceAllString(content, " "))
}

// hasDataFlow reports whether the document shows input moving to output
// — arrows or a mermaid flow. A sentence containing the word "input" is
// not a data flow.
func hasDataFlow(content string) bool {
	return len(proseArrowRe.FindAllString(content, -1)) >= 2 ||
		mermaidFlowRe.MatchString(content)
}

// hasErrorStrategy reports whether the document names a failure AND says
// what happens then. "Handle errors" names a failure and no disposition,
// which is the gap this asks about.
func hasErrorStrategy(content string) bool {
	return failureTermRe.MatchString(content) && dispositionRe.MatchString(content)
}

// hasInterfaces reports whether the document names something at a
// boundary. Level 3 asks the harder version of this question — whether
// the shape is pinned — in hasBoundaryContract.
func hasInterfaces(content string) bool {
	return boundaryHeadingRe.MatchString(content) ||
		len(tableRowRe.FindAllString(content, -1)) >= minPinnedShapeLines
}

// hasTechRationale reports whether a choice is recorded as a choice.
// The bts convention puts it in wireframe.md's `<!-- architect-decision -->`
// block, so either that marker or a stated reason satisfies this.
func hasTechRationale(content string) bool {
	return rationaleRe.MatchString(content) || alternativesRe.MatchString(content)
}

// ---------------------------------------------------------------------
// Delegation.
//
// A recipe is a chain of documents: domain.md holds the invariants,
// wireframe.md holds the decomposition, the flow and the recorded
// architect decision, scope.md holds the boundaries and the stack. The
// blueprint is the last link, not a copy of the chain.
//
// Scoring the blueprint as though it were alone made restating upstream
// the only way to score. On one measured recipe the draft grew to 4.7x
// the combined size of everything before it, and its "execution paths"
// section was a second copy of the wireframe's. Every copy is a second
// place the same claim can go stale — which is how a decision withdrawn
// in one section stayed standing in five others.
//
// So a criterion whose content has a canonical home upstream is
// satisfied by NAMING that home. A blueprint that says "the flow is in
// wireframe.md" has not lost the flow; it has located it, in one place,
// where a correction has to be made once.
//
// This applies only to the criteria that HAVE an upstream home. The
// Level 3 criteria do not delegate: what is always true and who keeps it
// true, what shape crosses a boundary, what cannot be undone, and what
// is still open are the blueprint's own job, and a pointer elsewhere
// would be the blueprint declining to do it.

var siblingDocRe = regexp.MustCompile(
	`(?i)\b(?:domain|wireframe|scope|intent|research)(?:/v\d+)?\.md\b`)

// delegates reports whether the document points at a sibling recipe
// document for this part of the answer.
func delegates(content string) bool {
	return siblingDocRe.MatchString(content)
}

// orDelegated wraps a predicate so that naming the upstream document
// satisfies it.
func orDelegated(predicate func(string) bool) func(string) bool {
	return func(content string) bool {
		return predicate(content) || delegates(content)
	}
}
