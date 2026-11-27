package hook

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// The Artifact guard is a user-level PreToolUse hook (`bts pages setup`
// adds it to ~/.claude/settings.json with the matcher "Artifact"), so it
// runs in every session on the machine, in a bts project or not.  A
// claude.ai artifact belongs to the account that published it, and the
// active Claude Code account switches when one hits its usage limit, so a
// new page is published with `bts page publish` and served by bts instead
// (docs/research/pages-everywhere-design.md).
//
// It denies only a publish that would make a new claude.ai page from a
// file.  An update to an existing artifact (a publish with url), every
// other action, and a review page (which the project's own hook handles)
// pass.  A typed artifact (type_url) and a page declaring runtime
// capabilities pass with a note (D1): bts serves static pages only.

// EventArtifactGuard is the guard's event name, the argument the
// settings entry passes to `bts hook`.
const EventArtifactGuard EventType = "artifact-guard"

type artifactGuardHandler struct{}

// NewArtifactGuardHandler handles the user-level Artifact guard.
func NewArtifactGuardHandler() Handler { return &artifactGuardHandler{} }

func (h *artifactGuardHandler) EventType() EventType { return EventArtifactGuard }

func (h *artifactGuardHandler) Handle(input *HookInput) (*HookOutput, error) {
	deny, note := artifactGuard(input)
	switch {
	case deny != "":
		return &HookOutput{HookSpecificOutput: &HookSpecificOutput{
			HookEventName: "PreToolUse", PermissionDecision: "deny", PermissionDecisionReason: deny,
		}}, nil
	case note != "":
		return &HookOutput{HookSpecificOutput: &HookSpecificOutput{
			HookEventName: "PreToolUse", AdditionalContext: note,
		}}, nil
	}
	return &HookOutput{}, nil
}

func nonEmpty(v interface{}) bool {
	switch x := v.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(x) != ""
	case bool:
		return x
	case map[string]interface{}:
		return len(x) > 0
	case []interface{}:
		return len(x) > 0
	}
	return true
}

// artifactGuard returns the reason to deny the call, or a note to add to
// it, or neither.
func artifactGuard(input *HookInput) (deny, note string) {
	if input == nil || input.ToolName != "Artifact" {
		return "", ""
	}
	in := input.ToolInput
	if action, _ := in["action"].(string); action != "" && action != "publish" {
		return "", ""
	}
	if nonEmpty(in["url"]) || nonEmpty(in["asset"]) {
		return "", ""
	}
	if nonEmpty(in["type_url"]) || nonEmpty(in["capabilities"]) {
		return "", "[bts] This artifact needs claude.ai itself (a typed artifact or runtime capabilities), so it stays on claude.ai " +
			"under the account this session runs as; a later session on another account cannot update it. " +
			"A static page goes through `bts page publish` instead."
	}
	paths := artifactPaths(in)
	if len(paths) == 0 {
		return "", ""
	}
	base := input.CWD
	for _, p := range paths {
		if reviewWorkRe.MatchString(filepath.ToSlash(p)) || reviewKitRe.MatchString(filepath.ToSlash(p)) {
			// A review page: the project's hook decides by its review.surface.
			return "", ""
		}
		if base != "" && !filepath.IsAbs(p) && reviewWorkRe.MatchString(filepath.ToSlash(filepath.Join(base, p))) {
			return "", ""
		}
	}
	return guardReason(in), ""
}

// guardReason names the bts command that publishes what the call would
// have published.
func guardReason(in map[string]interface{}) string {
	page, _ := in["file_path"].(string)
	if page == "" {
		page = "<page.html>"
	}
	cmd := "bts page publish " + shellQuote(page)
	var extra []string
	switch files := in["files"].(type) {
	case map[string]interface{}:
		for published, src := range files {
			from := ""
			switch v := src.(type) {
			case string:
				from = v
			case map[string]interface{}:
				from, _ = v["from"].(string)
			}
			if from != "" {
				extra = append(extra, " --file "+shellQuote(published+"="+from))
			}
		}
	case []interface{}:
		for _, f := range files {
			if m, ok := f.(map[string]interface{}); ok {
				if p, _ := m["path"].(string); p != "" {
					extra = append(extra, " --file "+shellQuote(p+"="+p))
				}
			}
		}
	}
	sort.Strings(extra)
	cmd += strings.Join(extra, "")
	if d, _ := in["description"].(string); strings.TrimSpace(d) != "" {
		cmd += " --description " + shellQuote(d)
	}
	md := ""
	if strings.EqualFold(filepath.Ext(page), ".md") {
		md = "bts serves HTML pages only: build this page as HTML (the artifact-design rules still apply) and publish that file. "
	}
	return md + fmt.Sprintf("New pages are published with bts, not as claude.ai artifacts: an artifact belongs to one claude.ai account, "+
		"and the account this machine runs as changes. Keep the page as written and run `%s` (the title comes from its <title>, "+
		"or pass --title). It prints the page id and its links: on this Mac, and on the tailnet for the owner's phone. "+
		"Give the owner those links instead of a claude.ai URL. To update the page later, publish the same file again or pass "+
		"--page <id>; `bts page export <id>` writes one HTML file for people outside the tailnet. Existing claude.ai artifacts "+
		"are updated as before, with a publish that names their url.", cmd)
}

// shellSafe says whether r needs no quoting in a POSIX shell word.
func shellSafe(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./=:@+,%", r)
}

// shellQuote quotes s for a POSIX shell when it needs it.
func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool { return !shellSafe(r) }) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
