package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Settings is a partial mapping of .bts/config/settings.yaml — only the
// fields the Go engine reads. Fields not declared here are ignored when
// the YAML is parsed, so adding skill-only keys in settings.yaml does
// not require a code change.
type Settings struct {
	Implement ImplementSettings `yaml:"implement"`
	Defend    DefendSettings    `yaml:"defend"`
	// Simulate is the legacy block (simulation was removed); only its
	// finding_batch is still read, into Defend when defend sets none.
	Simulate legacySimulateSettings `yaml:"simulate"`
	Verify   VerifySettings         `yaml:"verify"`
	// Statusline registers bts's own statusLine in the project's
	// .claude/settings.local.json.  Off by default (jongyoon, 11-06): a
	// project entry overrides the statusline a person configures in their
	// own Claude Code settings, and Claude Code customises its statusline
	// itself now.
	Statusline bool `yaml:"statusline"`
	// Review picks where the user reviews a recipe's documents.
	Review ReviewSettings `yaml:"review"`
	// Interrogate bounds the prove stage's adversary rounds.
	Interrogate InterrogateSettings `yaml:"interrogate"`
}

// InterrogateSettings: `max_rescope_rounds` is how many scoped rounds a
// large change after a verified interrogate run requires, at most, before
// interrogate_clean lets a further large change through with a notice
// (trial problem #140).  1 or more; the default is 4: on the Reports PR 3
// recipe the fourth and fifth scoped rounds still reproduced 12 real
// defects, while the rounds a comment edit or a rebase forced no longer
// count.
type InterrogateSettings struct {
	MaxRescopeRounds int `yaml:"max_rescope_rounds"`
}

// ReviewSettings: `surface` is "artifact" (a review page with pinned
// notes, /bts-review-page) or "ask" (AskUserQuestion).  `bts review page`
// refuses when it is "ask".
type ReviewSettings struct {
	Surface string `yaml:"surface"`
}

// ReviewSurfaceArtifact and ReviewSurfaceAsk are the values of review.surface.
const (
	ReviewSurfaceArtifact = "artifact"
	ReviewSurfaceAsk      = "ask"
)

// ImplementSettings controls the implementation loop's retry limits and
// mid-run review cadence. These were previously hard-coded in three
// places (settings.yaml prose, bts-implement SKILL.md, protocol table);
// this struct is now the single source.
type ImplementSettings struct {
	MaxBuildRetries   int                 `yaml:"max_build_retries"`
	MaxTestIterations int                 `yaml:"max_test_iterations"`
	MidrunReviewEvery int                 `yaml:"midrun_review_every"`
	RetryLadder       RetryLadderSettings `yaml:"retry_ladder"`
	// RequireEditGo makes `bts recipe log --phase` refuse the first phase
	// that writes product code (a build recipe's skeleton, any other
	// type's first implement phase) until the recipe holds a resolved
	// decision `code-edit-go`. Off by default. On true-shark the user's
	// "wait for approval before editing" held only because the agent
	// chose to ask (trial problem #67).
	RequireEditGo bool `yaml:"require_edit_go"`
	// FormatCmd is the project's own formatter, run at the project root
	// before every recorded command (build check, falsifier runs, test run,
	// drive) and before bts fingerprints the code for it.  A build that
	// formats its sources itself (CSharpier.MsBuild in Carepatron-App)
	// otherwise rewrites the files each command builds, so the fingerprint
	// one command recorded goes stale on the next (trial problem #69).
	// Empty: nothing runs.
	FormatCmd string `yaml:"format_cmd"`
	// SetupCmd makes a fresh copy of the project runnable (a dependency
	// install such as "cd agent_server && uv sync -q --extra dev"): bts runs
	// it in every copy it makes (adversary copies, interrogate reruns,
	// mutations, pins) before the command under test, since a copy holds
	// what git lists and no ignored virtualenv or node_modules (trial
	// problems #141, #156).  A --setup flag wins over it; with neither, a
	// recipe bound to a feature takes the feature's setup.  Empty: nothing
	// runs.
	SetupCmd string `yaml:"setup_cmd"`
}

// RetryLadderSettings mirrors engine.LadderConfig with yaml tags.
// Keeping them separate avoids leaking yaml struct tags into the pure-
// Go ladder API.
type RetryLadderSettings struct {
	SyntacticMax      int  `yaml:"syntactic_max"`
	SemanticMax       int  `yaml:"semantic_max"`
	SpecEscalate      bool `yaml:"spec_escalate"`
	DomainEscalate    bool `yaml:"domain_escalate"`
	ArchitectEscalate bool `yaml:"architect_escalate"`
}

// LadderConfig projects RetryLadderSettings onto the engine's internal
// config type. Used by the CLI when invoking NextRetryDecision.
func (r RetryLadderSettings) LadderConfig() LadderConfig {
	return LadderConfig(r)
}

// DefendSettings sizes /bts-defend. FindingBatch is how many findings
// one defender agent takes; 0 disables the cap. It was
// simulate.finding_batch until simulation was removed.
type DefendSettings struct {
	FindingBatch int `yaml:"finding_batch"`
}

// legacySimulateSettings reads an old settings.yaml's simulate block, of
// which only finding_batch still means anything.
type legacySimulateSettings struct {
	FindingBatch *int `yaml:"finding_batch"`
}

// VerifySettings captures verify loop controls read by Go code. Fields
// not referenced from Go stay undeclared here and pass through the
// parser untouched.
type VerifySettings struct {
	// MaxIterations is the convergence budget: consecutive verify rounds
	// allowed to make no progress on (critical, major, minor_resolvable)
	// before the loop must stop and ask the user. Enforced in
	// engine/convergence.go and `bts recipe log`.
	MaxIterations int `yaml:"max_iterations"`
	// MaxRounds is the total number of verify rounds allowed on one
	// document, regardless of what any of them measured. 0 disables.
	//
	// MaxIterations bounds rounds that make NO PROGRESS, which is a
	// judgement about the triple and therefore about which instruments
	// produced it. Every refinement of that judgement — per-document,
	// per-measurement-class, baseline staleness — closed a way for the
	// budget to be reset by something other than the document improving,
	// and each was found only after a recipe had already escaped through
	// it. One ran seventeen rounds against a budget of three.
	//
	// This cap makes no judgement at all. It counts. Nothing a round can
	// measure, declare or improve changes the count, so there is no hole
	// to find. It is the backstop, not the mechanism: a recipe that hits
	// it should move its open findings to Known Uncertainties and start
	// implementing, where a compiler and a test settle in seconds what
	// the eighteenth round was going to argue about.
	MaxRounds int `yaml:"max_rounds"`
	// EvidenceTTLDays is how long a cached framework-claim lookup stays
	// usable. 0 disables expiry for successful lookups; "unavailable"
	// results always expire after an hour regardless.
	EvidenceTTLDays int `yaml:"evidence_ttl_days"`
	// MaxSectionLines is the H2 section length at which `bts verify`
	// starts reporting span. 0 disables. See section_span_checker.go.
	MaxSectionLines int `yaml:"max_section_lines"`
	// MaxDocumentLines is the whole-document length at which `bts verify`
	// reports span. 0 disables.
	//
	// The per-section limit alone does not bound a document: seven
	// sections at the limit is 2,100 lines. Span is the one input to the
	// loop's total cost that is knowable before the loop starts —
	// section length and finding count correlate at r=+0.95 — so the
	// document as a whole needs its own bound, and a blueprint that
	// carries only what code cannot cheaply falsify does not approach it.
	MaxDocumentLines int `yaml:"max_document_lines"`
	// SectionSpanSeverity classifies both span reports. "major" (default)
	// or "critical" makes `bts verify` exit non-zero on an oversize
	// document or section, which blocks an express build's DONE
	// (build_verify_clean); "minor_resolvable" or "info" reports without
	// blocking. A span report never enters findings.jsonl or
	// verify-log.jsonl, so it blocks no other completion gate.
	//
	// It defaulted to info while the check was new. Measured afterwards:
	// 15 of 26 recipes in one project carried an oversize section, the
	// report fired on every one of them, and nothing followed. A finding
	// nobody has to act on is a finding nobody acts on.
	SectionSpanSeverity string `yaml:"section_span_severity"`
	// ConfirmPasses is how many consecutive clean full passes over an
	// unchanged revision the completion gate requires. 1 restores the
	// pre-v0.14 single-round rule.
	//
	// A verify round is a sample, not a measurement: measured rounds on
	// byte-identical documents have disagreed by two criticals and nine
	// majors, and rounds following no edit at all still averaged 8.9
	// previously-unseen findings. Replication is what turns a clean
	// number into evidence — see engine/completion_evidence.go.
	ConfirmPasses int `yaml:"confirm_passes"`
}

// DefaultSettings returns the built-in defaults matching the comments
// in the settings.yaml template. Callers on projects without an
// explicit settings.yaml receive this struct.
func DefaultSettings() *Settings {
	return &Settings{
		Implement: ImplementSettings{
			// 8 = full ladder walk: 3 syntactic + 2 semantic + 1 spec +
			// 1 domain + 1 architect escalation. A cap of 5 would make
			// tiers 4-5 unreachable (the pre-v0.6.1 bug).
			MaxBuildRetries:   8,
			MaxTestIterations: 5,
			MidrunReviewEvery: 5,
			RetryLadder: RetryLadderSettings{
				SyntacticMax:      3,
				SemanticMax:       2,
				SpecEscalate:      true,
				DomainEscalate:    true,
				ArchitectEscalate: true,
			},
		},
		Defend: DefendSettings{FindingBatch: 6},

		Verify: VerifySettings{
			MaxIterations:       3,
			MaxRounds:           6,
			EvidenceTTLDays:     30,
			ConfirmPasses:       2,
			MaxSectionLines:     300,
			MaxDocumentLines:    400,
			SectionSpanSeverity: SeverityMajor,
		},
		Review:      ReviewSettings{Surface: ReviewSurfaceArtifact},
		Interrogate: InterrogateSettings{MaxRescopeRounds: 4},
	}
}

// LoadSettings reads .bts/config/settings.yaml under the given project
// root. Missing file → DefaultSettings (no error). Malformed YAML →
// error. Present file overrides fields individually; any field left
// zero in the YAML keeps the default.
func LoadSettings(root string) (*Settings, error) {
	path := filepath.Join(root, ".bts", "config", "settings.yaml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DefaultSettings(), nil
	}
	if err != nil {
		return nil, err
	}

	s := DefaultSettings()
	if err := yaml.Unmarshal(data, s); err != nil {
		return nil, err
	}
	// Restore defaults for zero-valued fields that the user left out.
	// yaml.Unmarshal of an absent key leaves the default intact, but
	// an explicit "max_build_retries: 0" would zero it — treat that as
	// an opt-in default instead of a silent "no retries".
	def := DefaultSettings()
	if s.Implement.MaxBuildRetries <= 0 {
		s.Implement.MaxBuildRetries = def.Implement.MaxBuildRetries
	}
	if s.Implement.MaxTestIterations <= 0 {
		s.Implement.MaxTestIterations = def.Implement.MaxTestIterations
	}
	if s.Implement.MidrunReviewEvery < 0 {
		s.Implement.MidrunReviewEvery = def.Implement.MidrunReviewEvery
	}
	// defend.finding_batch, or an old simulate.finding_batch when defend
	// sets none; a negative value falls back to the default.
	var explicit struct {
		Defend struct {
			FindingBatch *int `yaml:"finding_batch"`
		} `yaml:"defend"`
	}
	_ = yaml.Unmarshal(data, &explicit)
	if explicit.Defend.FindingBatch == nil && s.Simulate.FindingBatch != nil {
		s.Defend.FindingBatch = *s.Simulate.FindingBatch
	}
	if s.Defend.FindingBatch < 0 {
		s.Defend.FindingBatch = def.Defend.FindingBatch
	}
	if s.Verify.MaxIterations <= 0 {
		s.Verify.MaxIterations = def.Verify.MaxIterations
	}
	if s.Interrogate.MaxRescopeRounds <= 0 {
		s.Interrogate.MaxRescopeRounds = def.Interrogate.MaxRescopeRounds
	}
	switch strings.ToLower(strings.TrimSpace(s.Review.Surface)) {
	case ReviewSurfaceAsk:
		s.Review.Surface = ReviewSurfaceAsk
	default:
		s.Review.Surface = def.Review.Surface
	}
	if s.Verify.ConfirmPasses <= 0 {
		s.Verify.ConfirmPasses = def.Verify.ConfirmPasses
	}
	// 0 is meaningful (disable the check), so only a negative value
	// falls back to the default.
	if s.Verify.MaxSectionLines < 0 {
		s.Verify.MaxSectionLines = def.Verify.MaxSectionLines
	}
	if s.Verify.MaxDocumentLines < 0 {
		s.Verify.MaxDocumentLines = def.Verify.MaxDocumentLines
	}
	if s.Verify.MaxRounds < 0 {
		s.Verify.MaxRounds = def.Verify.MaxRounds
	}
	// An unrecognised severity was passed through verbatim and ended up
	// in the Issue as a classification nothing downstream knows how to
	// count, so a typo silently turned the check off rather than
	// changing how loudly it spoke.
	switch s.Verify.SectionSpanSeverity {
	case SeverityCritical, SeverityMajor, SeverityMinor, SeverityInfo:
	default:
		if s.Verify.SectionSpanSeverity != "" {
			fmt.Fprintf(os.Stderr,
				"[bts] warning: verify.section_span_severity=%q is not one of %s/%s/%s/%s — using %s\n",
				s.Verify.SectionSpanSeverity, SeverityCritical, SeverityMajor,
				SeverityMinor, SeverityInfo, def.Verify.SectionSpanSeverity)
		}
		s.Verify.SectionSpanSeverity = def.Verify.SectionSpanSeverity
	}
	// 0 is meaningful here (never expire), so only a negative value
	// falls back to the default.
	if s.Verify.EvidenceTTLDays < 0 {
		s.Verify.EvidenceTTLDays = def.Verify.EvidenceTTLDays
	}
	if s.Implement.RetryLadder.SyntacticMax <= 0 {
		s.Implement.RetryLadder.SyntacticMax = def.Implement.RetryLadder.SyntacticMax
	}
	if s.Implement.RetryLadder.SemanticMax <= 0 {
		s.Implement.RetryLadder.SemanticMax = def.Implement.RetryLadder.SemanticMax
	}
	return s, nil
}
