package cli

import (
	"strings"
	"testing"
)

// settings.yaml is user-owned and preserved across updates, so removing
// a key from the template only affects fresh installs. Every existing
// project kept its copy — and `bts doctor` reports unread keys, so
// upgrading turned a healthy project's `--strict` run red with a remedy
// that said "delete them" and no way to do it but by hand.
func TestStripYAMLKeys_RemovesDeadKeysAndKeepsEverythingElse(t *testing.T) {
	in := `# Verify
verify:
  max_iterations: 3            # budget
  convergence:
    require_zero_major: true
  confirm_passes: 2

# Fix recipe
fix:
  debate_rounds: 1
  simulate_scenarios: 3        # dead

# Resume
resume:
  changelog_tail: 5

# Context
context:
  clear_threshold: 80

# Privacy
privacy:
  strip_private_tags: true
  detect_secrets: true

# Vision
vision:
  size_threshold: 400
`
	out, removed := stripYAMLKeys(in, deadSettingsKeys)

	for _, gone := range []string{
		"simulate_scenarios", "changelog_tail", "clear_threshold",
		"strip_private_tags", "detect_secrets", "require_zero_major",
	} {
		if strings.Contains(out, gone) {
			t.Errorf("%s survived the prune:\n%s", gone, out)
		}
	}
	// Sections emptied by the removal go with their comment headers.
	for _, gone := range []string{"resume:", "context:", "privacy:", "# Resume", "# Privacy"} {
		if strings.Contains(out, gone) {
			t.Errorf("emptied section %q survived:\n%s", gone, out)
		}
	}
	// Everything a user reads settings.yaml for is untouched.
	for _, kept := range []string{
		"# Verify", "max_iterations: 3", "# budget", "confirm_passes: 2",
		"# Fix recipe", "debate_rounds: 1", "vision:", "size_threshold: 400",
	} {
		if !strings.Contains(out, kept) {
			t.Errorf("%q was lost:\n%s", kept, out)
		}
	}
	if len(removed) != len(deadSettingsKeys) {
		t.Errorf("removed = %v, want one entry per dead key", removed)
	}
	if strings.Contains(out, "\n\n\n") {
		t.Errorf("prune left a run of blank lines:\n%s", out)
	}

	// Idempotent: a second pass finds nothing.
	if _, again := stripYAMLKeys(out, deadSettingsKeys); len(again) != 0 {
		t.Errorf("second prune removed %v, want nothing", again)
	}
}

// A key a user added themselves is theirs. doctor reports it; update
// leaves it alone.
func TestStripYAMLKeys_LeavesUnknownKeysAlone(t *testing.T) {
	in := "custom:\n  my_own_knob: 7\n"
	out, removed := stripYAMLKeys(in, deadSettingsKeys)
	if len(removed) != 0 || out != in {
		t.Errorf("removed = %v, out = %q — an unrecognised key must survive", removed, out)
	}
}
