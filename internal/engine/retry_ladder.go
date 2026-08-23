package engine

import (
	"regexp"
	"strings"
)

// ErrorClass is the coarse classification used to route retry decisions.
// "syntactic" errors are the ones that respond to in-place edits
// (type mismatch, missing import, unterminated string). "semantic"
// errors cluster around behavior and require strategy changes. "unknown"
// falls into tier 3+ escalation paths because we cannot tell whether
// a different strategy would help.
type ErrorClass string

const (
	ErrorSyntactic ErrorClass = "syntactic"
	ErrorSemantic  ErrorClass = "semantic"
	ErrorUnknown   ErrorClass = "unknown"
)

// Patterns that most commonly mark a purely-syntactic error across the
// languages jig recipes target. Intentionally conservative — any miss
// falls back to semantic, which is the safer default (harder
// escalation kicks in sooner).
var (
	syntacticSignalsRe = regexp.MustCompile(
		`(?i)(` +
			// Go / TS / JS
			`\bsyntax error\b|\bunexpected token\b|\bunexpected identifier\b|\bunterminated\b|` +
			`\bcannot find name\b|\bcannot find module\b|\bno such file\b|` +
			`\bTS\d{4}\b|\bts\d{4}\b|` +
			// Python
			`\bSyntaxError\b|\bImportError\b|\bNameError\b|\bIndentationError\b|\bModuleNotFoundError\b|` +
			// Swift — these contain quotes; can't rely on \b at tail
			`cannot find type|cannot find '[^']+' in scope|` +
			`expected '[^']+'|\buse of undeclared\b|` +
			// Rust
			`\bunresolved import\b|\bunresolved name\b|` +
			`\bcannot find (type|macro|function|value)\b` +
			`)`,
	)
	semanticSignalsRe = regexp.MustCompile(
		`(?i)(\bassertion|assertionerror|expected .+ got |expected value|` +
			`comparison always true|infinite loop|deadlock|race detected|` +
			`panic: runtime error|segmentation fault|null pointer|` +
			`divide by zero|out of bounds|` +
			`FAIL\s+\w+|` +
			// Go-style `--- FAIL: TestFoo` or `test_foo failed` variants.
			`test\S*\s+failed\b|` +
			// Pytest/Jest failure summaries.
			`\d+\s+failed,?\s+\d+\s+passed` +
			`)`,
	)
)

// ClassifyBuildError maps a raw compiler/test error string to one of
// the three classes. `language` is a hint (`"go"`, `"ts"`, `"python"`,
// etc.) — not all classifiers use it today but future rules may.
func ClassifyBuildError(lastError string, _ string) ErrorClass {
	if lastError == "" {
		return ErrorUnknown
	}
	if syntacticSignalsRe.MatchString(lastError) {
		return ErrorSyntactic
	}
	if semanticSignalsRe.MatchString(lastError) {
		return ErrorSemantic
	}
	return ErrorUnknown
}

// RetryAction is the symbolic next step the loop should take. The
// jig-implement skill translates each to a concrete procedure.
type RetryAction string

const (
	ActionRetryInplace     RetryAction = "retry_inplace"
	ActionStrategySwitch   RetryAction = "strategy_switch"
	ActionSpecEscalate     RetryAction = "spec_escalate"
	ActionDomainEscalate   RetryAction = "domain_escalate"
	ActionArchitectEscal   RetryAction = "architect_escalate"
	ActionBlock            RetryAction = "block"
)

// RetryDecision is what NextRetryDecision returns for the CLI and skill
// to act on. Rationale is a short human-readable explanation suitable
// for the implement loop's log.
type RetryDecision struct {
	NextTier  int
	Action    RetryAction
	Rationale string
}

// Ladder tier meanings:
//   1 — syntactic retries (in-place fixes)
//   2 — semantic retries  (strategy switches)
//   3 — spec escalation   (re-read final.md, verify the block)
//   4 — domain escalation (re-verify invariant ownership)
//   5 — architect escalation (rewrite the anchor / re-decompose task)
//   6 — give up (blocked)
//
// `attemptsPerTier` caps how many attempts live in each tier before
// progressing. These mirror the settings.retry_ladder keys so operators
// can tune without code changes.
type LadderConfig struct {
	SyntacticMax      int
	SemanticMax       int
	SpecEscalate      bool
	DomainEscalate    bool
	ArchitectEscalate bool
}

// DefaultLadder matches the default settings.yaml values shipped with
// the template.
func DefaultLadder() LadderConfig {
	return LadderConfig{
		SyntacticMax:      3,
		SemanticMax:       2,
		SpecEscalate:      true,
		DomainEscalate:    true,
		ArchitectEscalate: true,
	}
}

// NextRetryDecision implements the ladder transition function. The
// caller passes the task's current tier and retry_count, plus the
// classified error, and receives the action to take.
//
// Rules (keep in sync with jig-implement SKILL.md §Step 3 retry block):
//   - Tier 1 (syntactic): retry in-place while errors stay syntactic
//     and retry_count < syntactic_max. First non-syntactic error or
//     exceeding the cap bumps us to tier 2.
//   - Tier 2 (semantic): strategy_switch. After semantic_max attempts
//     in this tier, move to tier 3.
//   - Tier 3 (spec_escalate): one shot, always moves to tier 4 on
//     exhaustion.
//   - Tier 4 (domain_escalate): one shot, moves to tier 5.
//   - Tier 5 (architect_escalate): one shot, moves to tier 6 (block).
//   - Disabling an escalation in LadderConfig skips that tier and
//     moves straight to the next.
//
// attemptsInTier counts attempts within the CURRENT tier only. Callers
// persist it as Task.AttemptsInTier and MUST reset it to 0 whenever
// NextTier differs from the task's current tier (see Task and
// jig-implement SKILL.md §Step 3). Task.RetryCount stays the TOTAL
// attempt budget used by the hard cap and is never reset.
func NextRetryDecision(currentTier, attemptsInTier int, errClass ErrorClass, cfg LadderConfig) RetryDecision {
	if cfg.SyntacticMax <= 0 {
		cfg.SyntacticMax = 3
	}
	if cfg.SemanticMax <= 0 {
		cfg.SemanticMax = 2
	}
	if currentTier < 1 {
		currentTier = 1
	}

	switch currentTier {
	case 1:
		if errClass == ErrorSyntactic && attemptsInTier < cfg.SyntacticMax {
			return RetryDecision{
				NextTier:  1,
				Action:    ActionRetryInplace,
				Rationale: "syntactic error within tier 1 budget — retry in place",
			}
		}
		// Non-syntactic, or budget exhausted → advance.
		return advanceFrom(2, cfg)
	case 2:
		if attemptsInTier < cfg.SemanticMax {
			return RetryDecision{
				NextTier:  2,
				Action:    ActionStrategySwitch,
				Rationale: "semantic failure within tier 2 budget — try a different approach",
			}
		}
		return advanceFrom(3, cfg)
	case 3:
		return advanceFrom(4, cfg)
	case 4:
		return advanceFrom(5, cfg)
	case 5:
		return advanceFrom(6, cfg)
	default:
		return RetryDecision{
			NextTier:  6,
			Action:    ActionBlock,
			Rationale: "retry ladder exhausted — mark blocked",
		}
	}
}

// advanceFrom skips disabled escalation tiers. For example, if
// SpecEscalate is false and the next tier is 3, we jump to tier 4.
func advanceFrom(next int, cfg LadderConfig) RetryDecision {
	for {
		switch next {
		case 2:
			return RetryDecision{
				NextTier:  2,
				Action:    ActionStrategySwitch,
				Rationale: "lower tier exhausted or error is non-syntactic — try a different approach",
			}
		case 3:
			if !cfg.SpecEscalate {
				next = 4
				continue
			}
			return RetryDecision{
				NextTier:  3,
				Action:    ActionSpecEscalate,
				Rationale: "two prior tiers insufficient — re-read final.md block and re-verify",
			}
		case 4:
			if !cfg.DomainEscalate {
				next = 5
				continue
			}
			return RetryDecision{
				NextTier:  4,
				Action:    ActionDomainEscalate,
				Rationale: "spec re-read did not help — re-verify domain.md invariant ownership",
			}
		case 5:
			if !cfg.ArchitectEscalate {
				next = 6
				continue
			}
			return RetryDecision{
				NextTier:  5,
				Action:    ActionArchitectEscal,
				Rationale: "domain check did not resolve — re-enter /jig-architect to re-decompose",
			}
		default:
			return RetryDecision{
				NextTier:  6,
				Action:    ActionBlock,
				Rationale: "retry ladder exhausted — mark blocked",
			}
		}
	}
}

// ShortErrorSignature returns a 60-character fingerprint of the error
// suitable for escalation_notes. Reduces log noise while still making
// the transition auditable.
func ShortErrorSignature(errText string) string {
	errText = strings.TrimSpace(errText)
	if len(errText) > 60 {
		return errText[:57] + "..."
	}
	return errText
}
