package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

func orcaOfferRoot(t *testing.T, settings string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".bts", "config"), 0755); err != nil {
		t.Fatal(err)
	}
	if settings != "" {
		if err := os.WriteFile(filepath.Join(root, ".bts", "config", "settings.yaml"), []byte(settings), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// The offer reaches only a session in Orca, says how orca.launch wants it
// asked, and is silent when the project turned it off.
func TestOrcaContinueOffer(t *testing.T) {
	r := &state.RecipeState{ID: "r-001-x", Type: "build", Phase: "finalize"}
	in := true
	prev := inOrca
	inOrca = func() bool { return in }
	t.Cleanup(func() { inOrca = prev })

	if got := orcaContinueOffer(orcaOfferRoot(t, ""), r); !strings.Contains(got, "`bts orca continue r-001-x`") ||
		!strings.Contains(got, "ask the user first") {
		t.Errorf("default (ask) offer: %q", got)
	}
	if got := orcaContinueOffer(orcaOfferRoot(t, "orca:\n  launch: auto\n"), r); !strings.Contains(got, "launches at once") {
		t.Errorf("auto offer: %q", got)
	}
	if got := orcaContinueOffer(orcaOfferRoot(t, "orca:\n  launch: off\n"), r); got != "" {
		t.Errorf("off must offer nothing, got %q", got)
	}
	in = false
	if got := orcaContinueOffer(orcaOfferRoot(t, ""), r); got != "" {
		t.Errorf("outside Orca there is nothing to offer, got %q", got)
	}
}
