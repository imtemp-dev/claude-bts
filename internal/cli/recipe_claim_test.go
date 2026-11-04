package cli

import (
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

func TestCheckClaimable(t *testing.T) {
	root, id := newTestRunFixture(t)
	if _, err := checkClaimable(root, id); err != nil {
		t.Fatalf("an open recipe is claimable: %v", err)
	}
	if _, err := checkClaimable(root, "r-999-missing"); err == nil || !strings.Contains(err.Error(), "no recipe") {
		t.Fatalf("a missing recipe must be refused, got %v", err)
	}
	r, _ := state.LoadRecipeState(root, id)
	r.Phase = "complete"
	_ = state.SaveRecipeState(root, r)
	if _, err := checkClaimable(root, id); err == nil || !strings.Contains(err.Error(), "nothing to claim") {
		t.Fatalf("a completed recipe must be refused, got %v", err)
	}
}
