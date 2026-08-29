package engine

import (
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strings"
)

// finalTaskAnchorRe matches the anchor comment in final.md. The action
// group accepts the three values task actions can take; any other
// action surfaces as a parse miss (not an action_mismatch) because the
// anchor is considered invalid in that case. The trailing group
// captures optional suffix text like `scope=a,b` used by Phase 14.
var finalTaskAnchorRe = regexp.MustCompile(
	`(?m)<!--\s*task-anchor:\s*(\S+)\s+(create|modify|delete)([^>]*)\s*-->`,
)

// scopeSuffixRe extracts the scope= value from the trailing text of an
// anchor. Only the first match is used; further tokens are ignored so
// authors can add informal notes after the scope.
var scopeSuffixRe = regexp.MustCompile(`\bscope=([A-Za-z_][A-Za-z0-9_,\-\s]*)`)

// TaskAnchorKey is the normalized "{path} {action}" string used to
// match final.md anchors against tasks.json Task.Anchor values.
type TaskAnchorKey struct {
	Path   string
	Action string
}

func (k TaskAnchorKey) String() string { return k.Path + " " + k.Action }

// CheckTaskAnchors enforces the tasks.json ↔ spec derivation contract.
//
// An anchor may come from either of two places, and the union is what
// counts:
//
//   - `<!-- task-anchor: {path} {action} -->` comments in final.md
//   - rows of wireframe.md's File Structure table, whose `File` and
//     `Action` columns already ARE the (path, action) pair
//
// The wireframe was always meant to be the source —
// bts-wireframe/SKILL.md § Step 4 says of that table, "This becomes the
// basis for task decomposition in /bts-implement" — but only final.md
// was ever read. So the blueprint had to carry a per-file section for
// every unit the implementation would touch, and that requirement is
// what turned blueprints into transcriptions: one measured recipe
// re-expanded 31 wireframe rows (47 lines) into 1,322 lines of per-file
// prose, which then drew over two hundred verify findings.
//
// Accepting both is also the migration: a recipe anchored in final.md
// keeps working untouched, and no `bts migrate` step is needed. That
// promise is what decides which source the orphan direction enforces —
// a wireframe File Structure table is historically a superset of
// tasks.json, so unioning it into the enforced set would have opened
// tens of criticals on every recipe written before this change.
//
// The check produces four issue categories:
//
//   - missing_anchor — tasks.json declares a Task whose Anchor (or
//     implicit File+Action if Anchor is empty) has no corresponding
//     <!-- task-anchor: ... --> in final.md. CRITICAL.
//   - orphan_anchor — the recipe's authoritative anchor source declares
//     an anchor with no matching Task in tasks.json. CRITICAL. The
//     authoritative source is final.md when it carries any anchor at
//     all, and the wireframe table otherwise; see the union comment in
//     the body for why the reverse direction is not the union.
//   - duplicate_anchor — the same (path, action) appears more than
//     once in final.md. MAJOR.
//   - action_mismatch — Task.Anchor says "foo.go create" but the
//     Task's own fields say File=foo.go, Action=modify. MAJOR.
//
// Missing either file returns nil (the caller — validator — relies on
// separate missing-file diagnostics). This keeps the checker composable.
func CheckTaskAnchors(finalPath, wireframePath, tasksPath string) []Issue {
	finalData, ferr := os.ReadFile(finalPath)
	wireframeData, werr := os.ReadFile(wireframePath)
	if ferr != nil && werr != nil {
		return nil
	}
	tasks, err := loadTasksForAnchor(tasksPath)
	if err != nil {
		return nil
	}

	finalAnchors, dupes := parseFinalAnchors(string(finalData))
	// A repeated File Structure row is visible to whoever wrote the
	// table; a repeated HTML comment is not, which is why duplicates are
	// reported for one source and silently folded for the other.
	wireframeAnchors := ParseWireframeFileTable(string(wireframeData))

	var issues []Issue
	for _, key := range dupes {
		issues = append(issues, Issue{
			Category: "task_anchor",
			Claim:    "duplicate_anchor: " + key.String(),
			Severity: "major",
			Detail:   "final.md declares <!-- task-anchor: " + key.String() + " --> more than once. Each anchor must be unique.",
		})
	}

	// Index tasks by their effective anchor key. Prefer Task.Anchor when
	// set; fall back to File+Action when the field is empty (pre-P9
	// recipes that haven't migrated yet).
	taskByKey := map[TaskAnchorKey][]int{}
	for i, t := range tasks {
		key := taskAnchorKey(t)
		taskByKey[key] = append(taskByKey[key], i)
		if t.Anchor != "" {
			declared := parseAnchorField(t.Anchor)
			if declared.Path != "" && (declared.Path != t.File || declared.Action != t.Action) {
				issues = append(issues, Issue{
					Category: "task_anchor",
					Claim:    "action_mismatch: " + t.ID,
					Severity: "major",
					Detail:   "Task " + t.ID + " declares anchor '" + t.Anchor + "' but its file+action is '" + t.File + " " + t.Action + "'. Reconcile one or the other.",
				})
			}
		}
	}

	// A task is anchored if EITHER source declares it, so the forward
	// direction reads the union.
	declaredSet := make(map[TaskAnchorKey]bool, len(finalAnchors)+len(wireframeAnchors))
	for _, a := range finalAnchors {
		declaredSet[a] = true
	}
	for _, a := range wireframeAnchors {
		declaredSet[a] = true
	}

	// The reverse direction is NOT the union, because the two sources do
	// not carry the same claim. A `<!-- task-anchor: -->` comment exists
	// only to be matched, so one with no task is a defect. A File
	// Structure row is a decomposition the wireframe wrote before
	// tasks.json existed, and under the old contract it is routinely a
	// superset: files later dropped, rows spelled with a different
	// relative path than the task carries. Enforcing those as orphans
	// turned one measured recipe's 31 rows against 12 final.md anchors
	// into 19 criticals on the next `bts verify`, with no migration to
	// run — the opposite of what this checker promises.
	//
	// So the table is authoritative exactly when it is the only source: a
	// recipe anchored in final.md keeps final.md's answer and is left
	// alone, and a recipe written against the table gets the both-ways
	// check the table was wired for.
	enforced := finalAnchors
	if len(finalAnchors) == 0 {
		enforced = wireframeAnchors
	}

	// missing_anchor: tasks.json key ∉ the declared union.
	var missing []TaskAnchorKey
	for key := range taskByKey {
		if !declaredSet[key] {
			missing = append(missing, key)
		}
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i].String() < missing[j].String() })
	for _, key := range missing {
		issues = append(issues, Issue{
			Category: "task_anchor",
			Claim:    "missing_anchor: " + key.String(),
			Severity: "critical",
			Detail: "tasks.json declares a Task for '" + key.String() + "' but neither wireframe.md's File Structure table nor final.md declares it. " +
				"Add the row to wireframe.md § File Structure (preferred — it is the decomposition), " +
				"or `<!-- task-anchor: " + key.String() + " -->` to final.md, or remove the task.",
		})
	}

	// orphan_anchor: an enforced declaration ∉ tasks.json set.
	taskSet := make(map[TaskAnchorKey]bool, len(taskByKey))
	for k := range taskByKey {
		taskSet[k] = true
	}
	var orphan []TaskAnchorKey
	seenOrphan := map[TaskAnchorKey]bool{}
	for _, a := range enforced {
		if taskSet[a] || seenOrphan[a] {
			continue
		}
		seenOrphan[a] = true
		orphan = append(orphan, a)
	}
	sort.Slice(orphan, func(i, j int) bool { return orphan[i].String() < orphan[j].String() })
	for _, key := range orphan {
		issues = append(issues, Issue{
			Category: "task_anchor",
			Claim:    "orphan_anchor: " + key.String(),
			Severity: "critical",
			Detail: "the spec declares '" + key.String() + "' (a wireframe File Structure row, or a `<!-- task-anchor: -->` in final.md) " +
				"but tasks.json has no matching Task. Add the task or remove the declaration.",
		})
	}
	return issues
}

// parseFinalAnchors returns anchors in document order (preserving first
// occurrence) plus the list of duplicates (second-onward occurrences).
func parseFinalAnchors(content string) (anchors []TaskAnchorKey, duplicates []TaskAnchorKey) {
	matches := finalTaskAnchorRe.FindAllStringSubmatch(content, -1)
	seen := map[TaskAnchorKey]int{}
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		key := TaskAnchorKey{
			Path:   strings.TrimSpace(m[1]),
			Action: strings.TrimSpace(m[2]),
		}
		seen[key]++
		switch seen[key] {
		case 1:
			anchors = append(anchors, key)
		case 2:
			duplicates = append(duplicates, key)
		}
	}
	return anchors, duplicates
}

// taskAnchorKey returns the effective key the Task should match. Prefers
// the explicit Anchor field; falls back to File+Action for recipes
// written before Phase 9 adds the field.
func taskAnchorKey(t taskMinimal) TaskAnchorKey {
	if t.Anchor != "" {
		parsed := parseAnchorField(t.Anchor)
		if parsed.Path != "" {
			return parsed
		}
	}
	return TaskAnchorKey{Path: t.File, Action: t.Action}
}

// parseAnchorField turns "src/foo.ts create" into {Path, Action}. Tokens
// beyond the first two (e.g. "scope=a,b" in Phase 14) are ignored here —
// modify-scope has its own checker.
func parseAnchorField(s string) TaskAnchorKey {
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return TaskAnchorKey{}
	}
	return TaskAnchorKey{Path: fields[0], Action: fields[1]}
}

// taskMinimal is a slim projection of tasks.json's task shape used by
// the anchor checker. Decoupled from state.Task so the engine package
// stays free of state imports.
type taskMinimal struct {
	ID     string `json:"id"`
	File   string `json:"file"`
	Action string `json:"action"`
	Anchor string `json:"anchor"`
}

func loadTasksForAnchor(path string) ([]taskMinimal, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Tasks []taskMinimal `json:"tasks"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return raw.Tasks, nil
}

// taskScopeMinimal deserializes the fields relevant to Phase 14 scope
// checking. Kept separate from taskMinimal so adding fields here does
// not require touching the anchor checker's wire format.
type taskScopeMinimal struct {
	ID           string   `json:"id"`
	File         string   `json:"file"`
	Action       string   `json:"action"`
	Status       string   `json:"status"`
	Anchor       string   `json:"anchor"`
	ModifyScope  []string `json:"modify_scope"`
	PreImageSha  string   `json:"pre_image_sha"`
	PostImageSha string   `json:"post_image_sha"`
}

// CheckModifyScope enforces the Phase 14 rule: Action=="modify" tasks
// must declare a ModifyScope that matches the `scope=` suffix of their
// anchor comment in final.md, AND when a post-image has been captured,
// the symbols actually touched must stay within that scope.
//
// Findings:
//   - modify_scope_required — Action=="modify" but ModifyScope empty
//     AND the anchor has no scope= suffix.  MAJOR
//   - scope_mismatch — ModifyScope disagrees with the anchor's scope=
//     (e.g. spec says {a,b}, tasks.json says {a,c}).  MAJOR
//   - scope_symbol_missing — a declared symbol cannot be found in the
//     target file (grep for the name). CRITICAL — suggests the scope
//     itself is wrong.
//   - scope_violation — reserved for per-task check (Phase 10) when
//     post-image diffs show out-of-scope changes. This checker does
//     not compute diffs; the runtime check lives in the implement
//     loop. Kept here as a placeholder for the validator to surface
//     findings recorded elsewhere.
//
// projectRoot is the working directory where task files resolve to
// (used for scope_symbol_missing). Empty projectRoot disables that
// specific check.
func CheckModifyScope(finalPath, tasksPath, projectRoot string) []Issue {
	finalData, err := os.ReadFile(finalPath)
	if err != nil {
		return nil
	}
	tasks, err := loadTasksWithScope(tasksPath)
	if err != nil {
		return nil
	}

	// Build a map of anchor key -> declared scope from final.md so we
	// can cross-reference.
	anchorScopes := parseAnchorScopes(string(finalData))

	var issues []Issue
	for _, t := range tasks {
		if t.Action != "modify" {
			continue
		}
		key := TaskAnchorKey{Path: t.File, Action: t.Action}
		if t.Anchor != "" {
			key = parseAnchorField(t.Anchor)
		}
		declared, anchorHasScope := anchorScopes[key]

		// modify_scope_required: both Task.ModifyScope and the anchor's
		// scope= must supply SOMETHING. If both are empty we cannot
		// verify anything and the task is effectively unbounded.
		if len(t.ModifyScope) == 0 && !anchorHasScope {
			issues = append(issues, Issue{
				Category: "modify_scope",
				Claim:    "modify_scope_required: " + t.ID,
				Severity: "major",
				Detail:   "Task " + t.ID + " has action=modify but no ModifyScope and the final.md anchor has no `scope=` suffix. Declare the symbols the task is allowed to touch on both sides (Phase 14).",
			})
			continue
		}

		// scope_mismatch: when both are declared they must agree (as sets).
		if len(t.ModifyScope) > 0 && anchorHasScope && !sameStringSet(t.ModifyScope, declared) {
			issues = append(issues, Issue{
				Category: "modify_scope",
				Claim:    "scope_mismatch: " + t.ID,
				Severity: "major",
				Detail:   "Task " + t.ID + " modify_scope " + joinSorted(t.ModifyScope) + " does not match final.md anchor scope= " + joinSorted(declared) + ". Reconcile one or the other.",
			})
		}

		// scope_symbol_missing — only runnable when projectRoot is set.
		// The `legacy` token is a migration placeholder: it means "the
		// migrator could not infer a scope from the description; a human
		// will refine this later". CheckModifyScope treats it as
		// unchecked so validation passes, but downstream monitoring
		// (Phase 17) can report how many tasks still carry it.
		if projectRoot != "" {
			scope := t.ModifyScope
			if len(scope) == 0 {
				scope = declared
			}
			if !isLegacyScope(scope) {
				missing := missingSymbolsInFile(projectRoot, t.File, scope)
				for _, sym := range missing {
					issues = append(issues, Issue{
						Category: "modify_scope",
						Claim:    "scope_symbol_missing: " + t.ID + ":" + sym,
						Severity: "critical",
						Detail:   "Task " + t.ID + " declares symbol '" + sym + "' in scope, but " + t.File + " does not contain that identifier. Update scope= in final.md or the task's modify_scope.",
					})
				}
			}
		}
	}
	return issues
}

// parseAnchorScopes walks final.md anchors and returns a map from
// anchor key to the declared scope list. Anchors with no scope= are
// absent from the map (vs. empty slice) so the caller can distinguish
// "no scope declared" from "empty scope declared".
func parseAnchorScopes(content string) map[TaskAnchorKey][]string {
	out := map[TaskAnchorKey][]string{}
	for _, m := range finalTaskAnchorRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 4 {
			continue
		}
		key := TaskAnchorKey{
			Path:   strings.TrimSpace(m[1]),
			Action: strings.TrimSpace(m[2]),
		}
		if key.Action != "modify" {
			continue
		}
		sm := scopeSuffixRe.FindStringSubmatch(m[3])
		if len(sm) < 2 {
			continue
		}
		symbols := splitScopeList(sm[1])
		out[key] = symbols
	}
	return out
}

// splitScopeList parses "a, b , c" into ["a","b","c"]. Trims whitespace
// and drops empty entries.
func splitScopeList(s string) []string {
	raw := strings.Split(s, ",")
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		r = strings.TrimSpace(r)
		if r != "" {
			out = append(out, r)
		}
	}
	return out
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, x := range a {
		seen[x]++
	}
	for _, y := range b {
		seen[y]--
	}
	for _, v := range seen {
		if v != 0 {
			return false
		}
	}
	return true
}

func joinSorted(xs []string) string {
	cp := append([]string{}, xs...)
	sort.Strings(cp)
	return "{" + strings.Join(cp, ",") + "}"
}

// missingSymbolsInFile returns the subset of symbols that do not appear
// as identifiers in the file's source text. We use a conservative word-
// boundary match rather than language-specific parsing because scope
// symbols are function/class/constant names which should match plainly.
// If the file cannot be read, returns all symbols as "missing" would be
// misleading — return nil instead (the anchor/scope checker produces a
// different error for missing files).
func missingSymbolsInFile(root, file string, symbols []string) []string {
	// Resolve file under root if it's relative.
	path := file
	if !isAbsolutePath(file) {
		path = rootJoin(root, file)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	content := string(data)
	var missing []string
	for _, sym := range symbols {
		if sym == "" {
			continue
		}
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(sym) + `\b`)
		if !re.MatchString(content) {
			missing = append(missing, sym)
		}
	}
	return missing
}

func isAbsolutePath(p string) bool {
	return len(p) > 0 && p[0] == '/'
}

// isLegacyScope detects the migration placeholder `["legacy"]`. The
// Phase 14 migrator uses this token when the description heuristic
// cannot infer real symbols; CheckModifyScope skips the symbol-missing
// pass for these so validation does not block on migrated recipes.
// Monitoring tracks remaining legacy tokens as an adoption metric.
func isLegacyScope(scope []string) bool {
	return len(scope) == 1 && strings.EqualFold(scope[0], "legacy")
}

func rootJoin(root, p string) string {
	if len(root) == 0 {
		return p
	}
	if root[len(root)-1] == '/' {
		return root + p
	}
	return root + "/" + p
}

func loadTasksWithScope(path string) ([]taskScopeMinimal, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Tasks []taskScopeMinimal `json:"tasks"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return raw.Tasks, nil
}
