package engine

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Settings is a partial mapping of .bts/config/settings.yaml — only the
// fields the Go engine reads. Fields not declared here are ignored when
// the YAML is parsed, so adding skill-only keys in settings.yaml does
// not require a code change.
type Settings struct {
	Implement ImplementSettings `yaml:"implement"`
	Simulate  SimulateSettings  `yaml:"simulate"`
	Verify    VerifySettings    `yaml:"verify"`
}

// ImplementSettings controls the implementation loop's retry limits and
// mid-run review cadence. These were previously hard-coded in three
// places (settings.yaml prose, bts-implement SKILL.md, protocol table);
// this struct is now the single source.
type ImplementSettings struct {
	MaxBuildRetries   int                 `yaml:"max_build_retries"`
	MaxTestIterations int                 `yaml:"max_test_iterations"`
	MidrunReviewEvery int                 `yaml:"midrun_review_every"`
	RetryLadder       RetryLadderSettings `yaml:"retry_ladder"`
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

// SimulateSettings captures the simulation checker thresholds.
type SimulateSettings struct {
	MinScenarios       int     `yaml:"min_scenarios"`
	CrossBoundaryRatio float64 `yaml:"cross_boundary_ratio"`
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
	// EvidenceTTLDays is how long a cached framework-claim lookup stays
	// usable. 0 disables expiry for successful lookups; "unavailable"
	// results always expire after an hour regardless.
	EvidenceTTLDays int `yaml:"evidence_ttl_days"`
	// MaxSectionLines is the H2 section length at which `bts verify`
	// starts reporting span. 0 disables. See section_span_checker.go.
	MaxSectionLines int `yaml:"max_section_lines"`
	// SectionSpanSeverity classifies those reports. "info" (default)
	// records without blocking; "major" makes an oversize section a
	// completion blocker.
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
		Simulate: SimulateSettings{
			MinScenarios:       5,
			CrossBoundaryRatio: DefaultCrossBoundaryRatio,
		},
		Verify: VerifySettings{
			MaxIterations:       3,
			EvidenceTTLDays:     30,
			ConfirmPasses:       2,
			MaxSectionLines:     300,
			SectionSpanSeverity: SeverityInfo,
		},
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
	if s.Simulate.MinScenarios <= 0 {
		s.Simulate.MinScenarios = def.Simulate.MinScenarios
	}
	if s.Simulate.CrossBoundaryRatio <= 0 {
		s.Simulate.CrossBoundaryRatio = def.Simulate.CrossBoundaryRatio
	}
	if s.Verify.MaxIterations <= 0 {
		s.Verify.MaxIterations = def.Verify.MaxIterations
	}
	if s.Verify.ConfirmPasses <= 0 {
		s.Verify.ConfirmPasses = def.Verify.ConfirmPasses
	}
	// 0 is meaningful (disable the check), so only a negative value
	// falls back to the default.
	if s.Verify.MaxSectionLines < 0 {
		s.Verify.MaxSectionLines = def.Verify.MaxSectionLines
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
