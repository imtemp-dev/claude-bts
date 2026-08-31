package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// baselineSettingsKeys are the keys the very first settings.yaml
// shipped with. Everything added after them must be registered in
// settingsInsertions so `bts migrate settings` can bring an old
// project's file up to date.
//
// This list should never grow. A new key belongs in settingsInsertions.
var baselineSettingsKeys = map[string]bool{
	"verify.max_iterations":         true,
	"debate.rounds":                 true,
	"debate.max_extensions":         true,
	"debate.expert_count":           true,
	"simulate.min_scenarios":        true,
	"implement.max_build_retries":   true,
	"implement.max_test_iterations": true,
	"fix.debate_rounds":             true,
	"debug.debate_rounds":           true,
	"debug.perspective_count":       true,
	"vision.size_threshold":         true,
	"vision.max_roadmap_items":      true,
	"vision.min_roadmap_items":      true,
}

// `bts migrate settings` exists because each release may add keys, and
// a project that init'd earlier ends up without them. Its own comment
// says "adding a new key to a future release means appending one entry
// here" — and for eleven releases nobody did. The table held a single
// v0.5.0 entry while ten keys shipped past it, so a real project asked
// for its new defaults and was told "settings.yaml already has all
// known defaults" with none of them present.
//
// Diligence was not the missing part; the check was.
func TestEverySettingsKeyIsMigratable(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "template", "templates",
		".bts", "config", "settings.yaml"))
	if err != nil {
		t.Fatalf("read the shipped settings template: %v", err)
	}

	registered := map[string]bool{}
	for _, ins := range settingsInsertions {
		registered[ins.Parent+"."+ins.Key] = true
	}

	sectionRe := regexp.MustCompile(`^([a-z_]+):`)
	keyRe := regexp.MustCompile(`^  ([a-z_]+):\s*\S`)
	var section string
	var missing []string
	for _, line := range strings.Split(string(data), "\n") {
		if m := sectionRe.FindStringSubmatch(line); m != nil {
			section = m[1]
			continue
		}
		m := keyRe.FindStringSubmatch(line)
		if m == nil || section == "" || section == "agents" {
			continue
		}
		full := section + "." + m[1]
		if baselineSettingsKeys[full] || registered[full] {
			continue
		}
		missing = append(missing, full)
	}

	if len(missing) > 0 {
		t.Errorf("keys shipped in the template that `bts migrate settings` cannot add: %v\n"+
			"Append a settingsInsertion for each, or a project that init'd before "+
			"the key will never see it and will be told it is already up to date.",
			missing)
	}
}

// The registered keys must actually be in the template — an entry for a
// key that no longer ships would inject a dead setting into every old
// project that migrates.
func TestNoStaleSettingsInsertions(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "template", "templates",
		".bts", "config", "settings.yaml"))
	if err != nil {
		t.Fatalf("read the shipped settings template: %v", err)
	}
	body := string(data)
	for _, ins := range settingsInsertions {
		if !regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(ins.Key) + `:`).MatchString(body) {
			t.Errorf("settingsInsertions has %s.%s (%s) but the template no longer ships it",
				ins.Parent, ins.Key, ins.Since)
		}
	}
}
