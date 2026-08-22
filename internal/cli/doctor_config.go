package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/state"
	"gopkg.in/yaml.v3"
)

// checkDuplicateHookRegistration reports bts hooks registered more than
// once for the same event.
//
// Claude Code merges hook configuration across scopes, so a bts hook
// present in both the project settings and the user's own settings runs
// twice per event. Everything downstream doubles silently: a measured
// project's metrics.jsonl held 11,394 lines of which only 5,991 were
// distinct, and every tool-use figure `bts stats` and `bts doctor`
// reported was inflated about twofold. `bts init` only ever inspects the
// project file, so the second registration is invisible to the thing
// that would otherwise notice.
func checkDuplicateHookRegistration(root string) []doctorIssue {
	home, herr := os.UserHomeDir()
	if herr != nil {
		home = ""
	}
	return duplicateHookRegistration(root, home)
}

// duplicateHookRegistration takes the user scope's location as an
// argument so it can be tested. Reading os.UserHomeDir() directly made
// the result depend on the developer's own machine: on anyone with bts
// installed at user scope — the normal state for someone working on this
// repo — a one-scope fixture reports a duplicate, and the test asserting
// otherwise fails for reasons that have nothing to do with the code.
func duplicateHookRegistration(root, home string) []doctorIssue {
	scopes := []struct {
		label string
		path  string
	}{
		{"project .claude/settings.local.json", filepath.Join(root, ".claude", "settings.local.json")},
		{"project .claude/settings.json", filepath.Join(root, ".claude", "settings.json")},
	}
	if home != "" {
		scopes = append(scopes,
			struct {
				label string
				path  string
			}{"user ~/.claude/settings.json", filepath.Join(home, ".claude", "settings.json")})
	}

	// event -> the scopes that register a bts hook for it
	seen := map[string][]string{}
	for _, sc := range scopes {
		for _, event := range btsHookEvents(sc.path) {
			seen[event] = append(seen[event], sc.label)
		}
	}

	var doubled []string
	for event, where := range seen {
		if len(where) > 1 {
			doubled = append(doubled, fmt.Sprintf("%s (%s)", event, strings.Join(where, " + ")))
		}
	}
	if len(doubled) == 0 {
		return nil
	}
	sort.Strings(doubled)
	return []doctorIssue{{
		level:   "warning",
		section: "config",
		message: fmt.Sprintf(
			"bts hooks are registered in more than one settings scope, so they fire twice per event: %s",
			strings.Join(doubled, ", ")),
		fix: "keep one registration. Everything derived from hook events — metrics counts, tool traces, " +
			"`bts stats` — is doubled while both are active",
	}}
}

// btsHookEvents returns the hook events a settings file registers a bts
// handler for. Unreadable or unparseable files contribute nothing: this
// is a diagnostic, not a gate.
func btsHookEvents(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return nil
	}
	// One event per settings file, however many handlers it registers.
	// Counting each matching hook separately made a single file that
	// registers two bts handlers for one event look like two scopes, and
	// the check reported a duplicate registration against itself.
	//
	// The name test mirrors template.isBtsHookCommand: the deployed
	// scripts have gone by bts_handle_ and forge-handle- as well, and a
	// check that only knows the current spelling silently stops seeing
	// the older installs that are most likely to be doubled.
	seen := map[string]bool{}
	var out []string
	for event, groups := range cfg.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				if !isBtsHookCommand(h.Command) || seen[event] {
					continue
				}
				seen[event] = true
				out = append(out, event)
			}
		}
	}
	return out
}

func isBtsHookCommand(cmd string) bool {
	for _, marker := range []string{"bts-handle-", "bts_handle_", "forge-handle-", "forge_handle_"} {
		if strings.Contains(cmd, marker) {
			return true
		}
	}
	return false
}

// settingsKeysRead is every settings.yaml key something actually reads —
// Go engine or skill prompt. Anything else in a user's file is a knob
// that does nothing.
//
// A measured project's settings.yaml carried `verify.convergence.*`
// (the convergence thresholds themselves), `context.clear_threshold: 80`,
// and `privacy.*`. Nothing read any of them: the thresholds are fixed in
// the stop hook, and that project's sessions ran to 95% context against
// a threshold of 80. A setting that looks authoritative and does nothing
// is worse than an absent one, because it is consulted and believed.
//
// Five of those keys were shipped by bts's own template, so a stock
// `bts init` produced a project this check immediately complained about
// and `--strict` exited 1 on. They were deleted from the template rather
// than whitelisted here: whitelisting a dead knob keeps the knob.
var settingsKeysRead = map[string]bool{
	"verify.max_iterations":                     true,
	"verify.evidence_ttl_days":                  true,
	"verify.confirm_passes":                     true,
	"verify.max_section_lines":                  true,
	"verify.section_span_severity":              true,
	"debate.rounds":                             true,
	"debate.max_extensions":                     true,
	"debate.expert_count":                       true,
	"simulate.min_scenarios":                    true,
	"simulate.cross_boundary_ratio":             true,
	"implement.max_build_retries":               true,
	"implement.max_test_iterations":             true,
	"implement.midrun_review_every":             true,
	"implement.retry_ladder.syntactic_max":      true,
	"implement.retry_ladder.semantic_max":       true,
	"implement.retry_ladder.spec_escalate":      true,
	"implement.retry_ladder.domain_escalate":    true,
	"implement.retry_ladder.architect_escalate": true,
	"fix.debate_rounds":                         true,
	"debug.debate_rounds":                       true,
	"debug.perspective_count":                   true,
	"vision.size_threshold":                     true,
	"vision.max_roadmap_items":                  true,
	"vision.min_roadmap_items":                  true,
	"agents":                                    true, // free-form map of agent → model
}

// checkUnreadSettings reports settings.yaml keys nothing consults.
func checkUnreadSettings(root string) []doctorIssue {
	path := filepath.Join(root, ".bts", "config", "settings.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc map[string]interface{}
	if yaml.Unmarshal(data, &doc) != nil {
		return nil
	}
	var unread []string
	collectLeafKeys(doc, "", &unread)
	if len(unread) == 0 {
		return nil
	}
	sort.Strings(unread)
	return []doctorIssue{{
		level:   "warning",
		section: "config",
		message: fmt.Sprintf(
			"settings.yaml sets %d key(s) nothing reads: %s",
			len(unread), strings.Join(unread, ", ")),
		fix: "delete them. A setting that looks authoritative and changes nothing is worse than an absent one — " +
			"`verify.convergence.*` in particular was removed because the completion thresholds are fixed in the stop hook",
	}}
}

// collectLeafKeys walks a parsed YAML tree and appends the dotted paths
// of scalar leaves that no reader knows about. A prefix present in
// settingsKeysRead stops the walk, so free-form maps like `agents` are
// accepted whole.
func collectLeafKeys(node interface{}, prefix string, out *[]string) {
	if prefix != "" && settingsKeysRead[prefix] {
		return
	}
	m, ok := node.(map[string]interface{})
	if !ok {
		if prefix != "" && !settingsKeysRead[prefix] {
			*out = append(*out, prefix)
		}
		return
	}
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		collectLeafKeys(v, key, out)
	}
}

// checkEvidenceProviderNeverSucceeded reports a documentation provider
// that has never produced a usable answer in this project.
//
// The evidence policy names Context7 as the first source and falls back
// to WebFetch when it is unavailable, which is the right behaviour. What
// nothing reported is a project where the fallback is the ONLY behaviour:
// a measured recipe made thirteen lookups, twelve recorded
// "Context7:unavailable" and one "not-attempted", and zero ever
// succeeded. That is an MCP configuration problem wearing the costume of
// a resilient system.
func checkEvidenceProviderNeverSucceeded(root string) []doctorIssue {
	entries, err := state.ListEvidence(root)
	if err != nil || len(entries) < 3 {
		return nil // too little history to say anything
	}
	unavailable := 0
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Gathered), "context7:unavailable") ||
			strings.Contains(strings.ToLower(e.Gathered), "context7:not-attempted") {
			unavailable++
		}
	}
	if unavailable < len(entries) {
		return nil // it has worked at least once
	}
	return []doctorIssue{{
		level:   "warning",
		section: "config",
		message: fmt.Sprintf(
			"Context7 has never resolved a claim in this project — all %d cached lookups fell back to WebFetch",
			len(entries)),
		fix: "the fallback works, but permanent fallback usually means the MCP server is not configured here. " +
			"Check .mcp.json and `claude mcp list`; if Context7 is deliberately unused, this warning is the only cost",
	}}
}
