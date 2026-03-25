package metrics

import (
	"math"
	"testing"
)

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 0.0001
}

func TestLookupPricing_ExactPrefix(t *testing.T) {
	p, ok := LookupPricing("claude-opus-4-6")
	if !ok {
		t.Fatal("expected match for claude-opus-4-6")
	}
	if p.InputPerMTok != 5.0 {
		t.Errorf("InputPerMTok: got %f, want 5.0", p.InputPerMTok)
	}
	if p.OutputPerMTok != 25.0 {
		t.Errorf("OutputPerMTok: got %f, want 25.0", p.OutputPerMTok)
	}
}

func TestLookupPricing_SonnetMatch(t *testing.T) {
	p, ok := LookupPricing("claude-sonnet-4-6")
	if !ok {
		t.Fatal("expected match for claude-sonnet-4-6")
	}
	if p.InputPerMTok != 3.0 {
		t.Errorf("InputPerMTok: got %f, want 3.0", p.InputPerMTok)
	}
}

func TestLookupPricing_HaikuMatch(t *testing.T) {
	p, ok := LookupPricing("claude-haiku-4-5-20251001")
	if !ok {
		t.Fatal("expected match for claude-haiku-4-5-20251001")
	}
	if p.InputPerMTok != 1.0 {
		t.Errorf("InputPerMTok: got %f, want 1.0", p.InputPerMTok)
	}
}

func TestLookupPricing_CaseInsensitive(t *testing.T) {
	_, ok := LookupPricing("Claude-Opus-4-6")
	if !ok {
		t.Fatal("expected case-insensitive match")
	}
}

func TestLookupPricing_UnknownModel(t *testing.T) {
	_, ok := LookupPricing("gpt-4o")
	if ok {
		t.Fatal("expected no match for gpt-4o")
	}
}

func TestLookupPricing_EmptyString(t *testing.T) {
	_, ok := LookupPricing("")
	if ok {
		t.Fatal("expected no match for empty string")
	}
}

func TestCalculateCost_Opus(t *testing.T) {
	tokens := TokenSnapshot{
		InputTokens:         100_000,
		OutputTokens:         10_000,
		CacheReadTokens:     200_000,
		CacheCreationTokens:  50_000,
	}
	cost := CalculateCost(tokens, "claude-opus-4-6")

	// Input: 100K * $5/MTok = $0.50
	if !almostEqual(cost.Input, 0.50) {
		t.Errorf("Input: got %f, want 0.50", cost.Input)
	}
	// Output: 10K * $25/MTok = $0.25
	if !almostEqual(cost.Output, 0.25) {
		t.Errorf("Output: got %f, want 0.25", cost.Output)
	}
	// CacheRead: 200K * $0.50/MTok = $0.10
	if !almostEqual(cost.CacheRead, 0.10) {
		t.Errorf("CacheRead: got %f, want 0.10", cost.CacheRead)
	}
	// CacheWrite: 50K * $6.25/MTok = $0.3125
	if !almostEqual(cost.CacheWrite, 0.3125) {
		t.Errorf("CacheWrite: got %f, want 0.3125", cost.CacheWrite)
	}
	// Total = 0.50 + 0.25 + 0.10 + 0.3125 = 1.1625
	if !almostEqual(cost.Total, 1.1625) {
		t.Errorf("Total: got %f, want 1.1625", cost.Total)
	}
}

func TestCalculateCost_UnknownModel(t *testing.T) {
	tokens := TokenSnapshot{InputTokens: 100_000, OutputTokens: 10_000}
	cost := CalculateCost(tokens, "unknown-model")
	if cost.Total != 0 {
		t.Errorf("Total: got %f, want 0 for unknown model", cost.Total)
	}
}

func TestCalculateCost_ZeroTokens(t *testing.T) {
	cost := CalculateCost(TokenSnapshot{}, "claude-opus-4-6")
	if cost.Total != 0 {
		t.Errorf("Total: got %f, want 0 for zero tokens", cost.Total)
	}
}

func TestFormatCost(t *testing.T) {
	tests := []struct {
		cost float64
		want string
	}{
		{0, "$0.00"},
		{0.005, "<$0.01"},
		{0.01, "$0.01"},
		{1.234, "$1.23"},
		{12.50, "$12.50"},
		{100.999, "$101.00"},
		{-1.0, "$0.00"},
	}
	for _, tt := range tests {
		got := FormatCost(tt.cost)
		if got != tt.want {
			t.Errorf("FormatCost(%f): got %q, want %q", tt.cost, got, tt.want)
		}
	}
}

func TestAddCost(t *testing.T) {
	a := CostBreakdown{Input: 1.0, Output: 2.0, CacheRead: 0.5, CacheWrite: 0.3, Total: 3.8}
	b := CostBreakdown{Input: 0.5, Output: 1.0, CacheRead: 0.1, CacheWrite: 0.1, Total: 1.7}
	sum := AddCost(a, b)
	if !almostEqual(sum.Total, 5.5) {
		t.Errorf("Total: got %f, want 5.5", sum.Total)
	}
	if !almostEqual(sum.Input, 1.5) {
		t.Errorf("Input: got %f, want 1.5", sum.Input)
	}
}
