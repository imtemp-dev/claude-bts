package state

import "testing"

// Simulation is removed from bts: a round's instruments are verify and
// audit. Older rounds recorded "simulate"; it stays readable and is
// ignored everywhere a dimension set is compared (prove stage decision 6).
func TestDimensions_SimulateIsLegacy(t *testing.T) {
	dims, err := NormalizeDimensions([]string{"verify", "audit", "simulate"})
	if err != nil || len(dims) != 2 || dims[0] != "audit" || dims[1] != "verify" {
		t.Fatalf("simulate must be accepted and dropped, got %v %v", dims, err)
	}
	if _, err := NormalizeDimensions([]string{"guess"}); err == nil {
		t.Fatal("an unknown dimension is still refused")
	}
	now := &VerifyLogEntry{Dimensions: []string{"audit", "verify"}, FullPass: true}
	old := &VerifyLogEntry{Dimensions: []string{"audit", "simulate", "verify"}, FullPass: true}
	if !now.HasAllDimensions() {
		t.Fatal("verify+audit is every instrument now")
	}
	if now.StrengthClass() != old.StrengthClass() {
		t.Fatalf("an old three-instrument round compares with a new one: %q vs %q", now.StrengthClass(), old.StrengthClass())
	}
	if !roundCovers(now, &FindingState{FindingEvent: FindingEvent{Dimensions: []string{"simulate"}}}) {
		t.Fatal("a finding an old simulate pass raised must be closable by a full round")
	}
}
