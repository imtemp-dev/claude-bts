package engine

import (
	"testing"
)

func TestClassifyBuildError_Syntactic(t *testing.T) {
	cases := []string{
		"error TS2345: Argument of type 'number' is not assignable to parameter of type 'string'",
		"SyntaxError: unexpected token",
		"cannot find module '@foo/bar'",
		"ModuleNotFoundError: No module named 'foo'",
		"expected 'func'",
	}
	for _, c := range cases {
		if got := ClassifyBuildError(c, "go"); got != ErrorSyntactic {
			t.Errorf("ClassifyBuildError(%q) = %q, want syntactic", c, got)
		}
	}
}

func TestClassifyBuildError_Semantic(t *testing.T) {
	cases := []string{
		"assertion failed: expected 3 got 2",
		"FAIL TestAdd",
		"panic: runtime error: index out of range",
		"test_auth_refresh failed",
	}
	for _, c := range cases {
		if got := ClassifyBuildError(c, ""); got != ErrorSemantic {
			t.Errorf("ClassifyBuildError(%q) = %q, want semantic", c, got)
		}
	}
}

func TestClassifyBuildError_Empty(t *testing.T) {
	if got := ClassifyBuildError("", ""); got != ErrorUnknown {
		t.Errorf("empty error → want unknown, got %q", got)
	}
}

func TestNextRetryDecision_Tier1Retries(t *testing.T) {
	cfg := DefaultLadder()
	d := NextRetryDecision(1, 0, ErrorSyntactic, cfg)
	if d.Action != ActionRetryInplace || d.NextTier != 1 {
		t.Errorf("tier 1 syntactic should retry in place, got %+v", d)
	}
}

// Tier 1 exhaustion → tier 2 strategy switch.
func TestNextRetryDecision_Tier1Exhausted(t *testing.T) {
	cfg := DefaultLadder()
	d := NextRetryDecision(1, cfg.SyntacticMax, ErrorSyntactic, cfg)
	if d.Action != ActionStrategySwitch || d.NextTier != 2 {
		t.Errorf("tier 1 exhausted → tier 2 switch, got %+v", d)
	}
}

// Non-syntactic error at tier 1 jumps straight to semantic tier.
func TestNextRetryDecision_Tier1NonSyntactic(t *testing.T) {
	cfg := DefaultLadder()
	d := NextRetryDecision(1, 0, ErrorSemantic, cfg)
	if d.Action != ActionStrategySwitch || d.NextTier != 2 {
		t.Errorf("semantic at tier 1 → tier 2, got %+v", d)
	}
}

// Tier 2 budget exhaustion moves to spec escalation.
func TestNextRetryDecision_Tier2ToSpec(t *testing.T) {
	cfg := DefaultLadder()
	d := NextRetryDecision(2, cfg.SemanticMax, ErrorSemantic, cfg)
	if d.Action != ActionSpecEscalate || d.NextTier != 3 {
		t.Errorf("tier 2 exhausted → spec escalate, got %+v", d)
	}
}

// Disabling spec_escalate skips tier 3.
func TestNextRetryDecision_SkipsDisabledSpec(t *testing.T) {
	cfg := DefaultLadder()
	cfg.SpecEscalate = false
	d := NextRetryDecision(2, cfg.SemanticMax, ErrorSemantic, cfg)
	if d.Action != ActionDomainEscalate || d.NextTier != 4 {
		t.Errorf("disabled spec → jump to domain, got %+v", d)
	}
}

// Tier 5 (architect) exhaustion → block.
func TestNextRetryDecision_Tier5ToBlock(t *testing.T) {
	cfg := DefaultLadder()
	d := NextRetryDecision(5, 1, ErrorSemantic, cfg)
	if d.Action != ActionBlock || d.NextTier != 6 {
		t.Errorf("tier 5 → block, got %+v", d)
	}
}

// Disabling every escalation → tier 2 exhaustion drops to block.
func TestNextRetryDecision_AllEscalationsDisabled(t *testing.T) {
	cfg := LadderConfig{
		SyntacticMax:      3,
		SemanticMax:       2,
		SpecEscalate:      false,
		DomainEscalate:    false,
		ArchitectEscalate: false,
	}
	d := NextRetryDecision(2, cfg.SemanticMax, ErrorSemantic, cfg)
	if d.Action != ActionBlock {
		t.Errorf("no escalations enabled → block directly, got %+v", d)
	}
}

func TestShortErrorSignature_Truncates(t *testing.T) {
	long := "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz"
	if got := ShortErrorSignature(long); len(got) != 60 {
		t.Errorf("expected 60 chars, got %d", len(got))
	}
	if got := ShortErrorSignature("short"); got != "short" {
		t.Errorf("short string must pass through, got %q", got)
	}
}

// Full ladder walk with per-tier counters: proves every escalation tier
// (spec → domain → architect) is reachable within the default hard cap
// of 8 total attempts. This is a regression test for the pre-v0.6.1 bug
// where the CLI passed the TOTAL retry count as attemptsInTier and the
// cap was 5, making tiers 4-5 dead code.
func TestNextRetryDecision_FullLadderWalkWithinDefaultCap(t *testing.T) {
	cfg := DefaultLadder()
	const hardCap = 8 // engine.DefaultSettings().Implement.MaxBuildRetries

	tier := 1
	attemptsInTier := 0
	totalRetries := 0
	var visited []RetryAction

	for totalRetries < hardCap {
		// Every loop iteration = one build failure.
		totalRetries++
		attemptsInTier++

		// Tier 1 sees syntactic errors; later tiers see semantic ones.
		errClass := ErrorSemantic
		if tier == 1 {
			errClass = ErrorSyntactic
		}

		d := NextRetryDecision(tier, attemptsInTier, errClass, cfg)
		visited = append(visited, d.Action)
		if d.Action == ActionBlock {
			break
		}
		if d.NextTier != tier {
			tier = d.NextTier
			attemptsInTier = 0 // the reset bts-implement Step 3.4 mandates
		}
	}

	want := map[RetryAction]bool{
		ActionSpecEscalate:   false,
		ActionDomainEscalate: false,
		ActionArchitectEscal: false,
	}
	for _, a := range visited {
		if _, ok := want[a]; ok {
			want[a] = true
		}
	}
	for action, seen := range want {
		if !seen {
			t.Errorf("action %s never reached within hard cap %d; visited=%v", action, hardCap, visited)
		}
	}
}

// Without the per-tier reset (the old buggy caller behavior), the walk
// must skip tiers — documents WHY the reset is mandatory.
func TestNextRetryDecision_NoResetSkipsTiers(t *testing.T) {
	cfg := DefaultLadder()
	// Simulate the old caller: attemptsInTier == total retry_count.
	d := NextRetryDecision(2, 4, ErrorSemantic, cfg)
	if d.Action != ActionSpecEscalate {
		t.Fatalf("expected immediate spec escalation when counter is not reset, got %+v", d)
	}
	// 4 attempts "in tier 2" exceeds SemanticMax=2 instantly even though
	// tier 2 only ever ran once — the skipped-strategy-switch symptom.
}
