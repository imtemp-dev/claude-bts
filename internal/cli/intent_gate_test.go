package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #157 (AGEN-215): a headless run moved a blueprint recipe
// to scoping with its intent unconfirmed; nothing in Go checked.
func TestPhaseChange_LeavingDiscoveryNeedsAConfirmedIntent(t *testing.T) {
	for _, typ := range []string{"blueprint", "build"} {
		t.Run(typ, func(t *testing.T) {
			root, id := newTestRunFixture(t)
			r, _ := state.LoadRecipeState(root, id)
			r.Type, r.Phase = typ, "discovery"
			if err := state.SaveRecipeState(root, r); err != nil {
				t.Fatal(err)
			}
			intent := filepath.Join(state.RecipeDir(root, id), "intent.md")
			// A Status line a session wrote by hand is not a confirmation.
			if err := os.WriteFile(intent, []byte("# Intent\n\n**Status:** CONFIRMED\n\nPurpose: greet.\n"), 0644); err != nil {
				t.Fatal(err)
			}

			for _, phase := range []string{"scoping", "research", "domain-model"} {
				err := checkPhasePreConditions(root, r, phase, true)
				if err == nil || !strings.Contains(err.Error(), "is not confirmed") || !strings.Contains(err.Error(), "bts recipe decision hold "+id) {
					t.Fatalf("discovery → %s with no confirmation recorded, even with --force: want a refusal naming the way out, got %v", phase, err)
				}
			}
			for _, phase := range []string{"discovery", "cancelled", "complete", "finalize"} {
				if err := checkPhasePreConditions(root, r, phase, true); err != nil {
					t.Errorf("discovery → %s must stay open: %v", phase, err)
				}
			}

			if _, err := executeRecipeConfirm(root, id, "intent", "yes, that is what I want"); err != nil {
				t.Fatal(err)
			}
			if err := checkPhasePreConditions(root, r, "scoping", false); err != nil {
				t.Fatalf("confirmed: discovery → scoping must pass, got %v", err)
			}
		})
	}
}

// Other recipe types, and a recipe already past discovery, are not held.
func TestPhaseChange_IntentGateScope(t *testing.T) {
	root, id := newTestRunFixture(t)
	r, _ := state.LoadRecipeState(root, id)
	r.Type, r.Phase = "fix", "discovery"
	if err := checkPhasePreConditions(root, r, "research", false); err != nil {
		t.Errorf("a fix recipe has no intent step: %v", err)
	}
	r.Type, r.Phase = "blueprint", "scoping"
	if err := checkPhasePreConditions(root, r, "research", false); err != nil {
		t.Errorf("a recipe already past discovery is not held: %v", err)
	}
}
