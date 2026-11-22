package hook

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// With review.surface: local a bts review page (a recipe's intent, scope
// or spec, a feature's intent, contracts and plan) is served by bts from
// this machine, tied to no claude.ai account.  A session still reaches for
// the Artifact tool by habit, and the harness urges it to publish; prose in
// a skill does not stop that, so the PreToolUse hook does.  Only review
// pages are held: any other artifact is the session's own business.

// localReviewPublishDenial is the reason to refuse an Artifact publish of a
// bts review page in a project whose review.surface is local; "" lets the
// call through.
func localReviewPublishDenial(root string, input *HookInput) string {
	if input.ToolName != "Artifact" {
		return ""
	}
	if action, _ := input.ToolInput["action"].(string); action != "" && action != "publish" {
		return ""
	}
	base := input.CWD
	if base == "" {
		base = root
	}
	arg, ok := reviewPageSubject(root, base, artifactPaths(input.ToolInput))
	if !ok {
		return ""
	}
	set, err := engine.LoadSettings(root)
	if err != nil || set.Review.Surface != engine.ReviewSurfaceLocal {
		return ""
	}
	return fmt.Sprintf("review.surface is local in .bts/config/settings.yaml: bts serves this review page from this machine, "+
		"tied to no claude.ai account, so it is not published as an artifact. Run `bts review open %s` instead: it serves "+
		"page.html with the kit and opens it. Then run `bts review wait` in the background and end the turn "+
		"(/bts-review-page § Local page).", arg)
}

// artifactPaths lists the local files an Artifact call names: file_path,
// file_paths, and the sources in files (a list of {path} or a map of
// published path to a source path or {from}).
func artifactPaths(in map[string]interface{}) []string {
	var out []string
	add := func(v interface{}) {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	add(in["file_path"])
	if list, ok := in["file_paths"].([]interface{}); ok {
		for _, v := range list {
			add(v)
		}
	}
	switch files := in["files"].(type) {
	case []interface{}:
		for _, f := range files {
			if m, ok := f.(map[string]interface{}); ok {
				add(m["path"])
			}
		}
	case map[string]interface{}:
		for published, src := range files {
			switch v := src.(type) {
			case string:
				add(v)
			case map[string]interface{}:
				add(v["from"])
			}
			add(published)
		}
	}
	return out
}

var (
	reviewWorkRe = regexp.MustCompile(`(?:^|/)\.bts/local/(recipes|features)/([^/]+)/review/`)
	reviewKitRe  = regexp.MustCompile(`(?:^|/)bts-review-kit\.(?:js|css)$`)
)

// reviewPageSubject says whether any of paths is part of a bts review page
// (a file in a review's work directory, or the review kit) and, when it can
// tell which review, how `bts review open` names it.
func reviewPageSubject(root, base string, paths []string) (string, bool) {
	kit := false
	for _, p := range paths {
		abs := p
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(base, p)
		}
		rel := filepath.ToSlash(abs)
		if r, err := filepath.Rel(root, abs); err == nil && !strings.HasPrefix(r, "..") {
			rel = filepath.ToSlash(r)
		}
		if m := reviewWorkRe.FindStringSubmatch(rel); m != nil {
			if m[1] == "features" {
				return "--feature " + m[2], true
			}
			return m[2] + " --point <intent|scope|spec>", true
		}
		if reviewKitRe.MatchString(filepath.ToSlash(p)) {
			kit = true
		}
	}
	if kit {
		return "<recipe-id> --point <intent|scope|spec> (or --feature <f>)", true
	}
	return "", false
}

// artifactAskRe matches a prompt that asks for an artifact or a review
// page, in English or Korean (the Korean words "artifact" and "review
// page" are written as code points so this file stays ASCII).
var artifactAskRe = regexp.MustCompile(`(?i)artifact|review ?page|\x{C544}\x{D2F0}\x{D329}\x{D2B8}|\x{B9AC}\x{BDF0} ?\x{D398}\x{C774}\x{C9C0}`)

// localSurfaceReminder is the line added to a prompt that asks for an
// artifact or a review page in a project whose review.surface is local,
// while a review can be under way: the session's first move should be the
// served page, not the Artifact tool.
func localSurfaceReminder(root, prompt string) string {
	if !artifactAskRe.MatchString(prompt) {
		return ""
	}
	set, err := engine.LoadSettings(root)
	if err != nil || set.Review.Surface != engine.ReviewSurfaceLocal {
		return ""
	}
	if r, err := state.GetActiveRecipe(root); err != nil || r == nil {
		if dirs, _ := filepath.Glob(filepath.Join(state.LocalPath(root), "features", "*", "review")); len(dirs) == 0 {
			return ""
		}
	}
	return "[bts] review.surface is local here: a review page for a recipe's intent, scope or spec, or a feature's plan, " +
		"is served by bts (`bts review open`, /bts-review-page § Local page), never published with the Artifact tool. " +
		"Other artifacts are unaffected."
}
