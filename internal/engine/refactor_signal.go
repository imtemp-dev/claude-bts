package engine

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RefactorSignal identifies a pattern in the changelog/tasks history
// that typically indicates the current decomposition is wrong and a
// redesign will save more than continued patching. These are
// diagnostic, not blocking — they surface as hints in the session-start
// summary and in `jig refactor-signal`.
type RefactorSignal struct {
	Kind     string   `json:"kind"`     // "test_fix_cascade" | "cross_module_churn"
	Evidence []string `json:"evidence"` // one short string per supporting observation
	Suggest  string   `json:"suggest"`  // recommended next step
}

// DetectRefactorSignals inspects a recipe directory's changelog and
// tasks.json to flag patch-of-patches patterns. The detection is
// intentionally conservative — false negatives are preferable to false
// positives here, because each signal prompts the user to consider a
// redesign and noisy triggers would desensitize them.
//
// Heuristics (Phase 6.4):
//
//   - test_fix_cascade: a test-phase log entry is followed within the
//     same recipe session by 3+ implement entries touching distinct
//     files. Indicates one test failure propagated into multi-module
//     fixes — the bug's blast radius exceeds the code's modularity.
//
//   - cross_module_churn: the same task id transitions through
//     pending → in_progress → done → pending again, or retry_count
//     exceeds a threshold while last_error changes across modules.
//     Captured via changelog action frequency rather than tasks.json
//     directly because retries are logged even when tasks state is
//     ultimately "done".
func DetectRefactorSignals(recipeDir string) ([]RefactorSignal, error) {
	changelog := filepath.Join(recipeDir, "changelog.jsonl")
	tasks := filepath.Join(recipeDir, "tasks.json")

	entries, err := loadChangelog(changelog)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	var signals []RefactorSignal
	if s := detectTestFixCascade(entries); s != nil {
		signals = append(signals, *s)
	}
	if s := detectCrossModuleChurn(entries, tasks); s != nil {
		signals = append(signals, *s)
	}
	return signals, nil
}

type changelogEntry struct {
	Time    string `json:"time"`
	Action  string `json:"action"`
	Output  string `json:"output"`
	Result  string `json:"result"`
	BasedOn string `json:"based_on"`
}

func loadChangelog(path string) ([]changelogEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []changelogEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e changelogEntry
		if err := json.Unmarshal([]byte(line), &e); err == nil {
			entries = append(entries, e)
		}
	}
	return entries, scanner.Err()
}

// detectTestFixCascade walks the changelog for a `test` action followed
// by 3+ `implement` actions touching distinct output paths. Returns a
// signal once — one signal per recipe is enough to prompt review.
func detectTestFixCascade(entries []changelogEntry) *RefactorSignal {
	for i := range entries {
		if entries[i].Action != "test" {
			continue
		}
		touched := map[string]bool{}
		for j := i + 1; j < len(entries); j++ {
			if entries[j].Action == "test" {
				break // next test cycle — stop counting this cascade
			}
			if entries[j].Action == "implement" {
				out := modulePrefix(entries[j].Output)
				if out != "" {
					touched[out] = true
				}
			}
		}
		if len(touched) >= 3 {
			mods := make([]string, 0, len(touched))
			for m := range touched {
				mods = append(mods, m)
			}
			sort.Strings(mods)
			return &RefactorSignal{
				Kind: "test_fix_cascade",
				Evidence: []string{
					"single test cycle triggered implement actions across " + strings.Join(mods, ", "),
					"cascade starts at changelog entry after index " + iToS(i),
				},
				Suggest: "One test failure pulled changes across 3+ modules. Review whether invariant ownership (domain.md §2) is respected in the current decomposition; the bug's blast radius suggests coupled state. Consider /jig-architect to propose an alternative decomposition.",
			}
		}
	}
	return nil
}

// detectCrossModuleChurn counts implement-action outputs across the
// whole recipe. If any single module appears 4+ times, or 3+ distinct
// modules each appear 3+ times, churn is flagged.
func detectCrossModuleChurn(entries []changelogEntry, tasksPath string) *RefactorSignal {
	counts := map[string]int{}
	for _, e := range entries {
		if e.Action != "implement" {
			continue
		}
		if m := modulePrefix(e.Output); m != "" {
			counts[m]++
		}
	}

	var hotspots []string
	for m, n := range counts {
		if n >= 4 {
			hotspots = append(hotspots, m+"×"+iToS(n))
		}
	}
	if len(hotspots) == 0 {
		// Alternative: 3+ modules each touched 3+ times.
		busy := 0
		var threshMods []string
		for m, n := range counts {
			if n >= 3 {
				busy++
				threshMods = append(threshMods, m+"×"+iToS(n))
			}
		}
		if busy >= 3 {
			sort.Strings(threshMods)
			return &RefactorSignal{
				Kind: "cross_module_churn",
				Evidence: []string{
					"3+ modules with 3+ implement actions each: " + strings.Join(threshMods, ", "),
				},
				Suggest: "Multiple modules are being repeatedly edited. This pattern precedes the 'patches of patches' trap. Re-check domain.md invariant owners and consider whether a single source-of-truth refactor would collapse several of these modules.",
			}
		}
		return nil
	}
	sort.Strings(hotspots)
	return &RefactorSignal{
		Kind: "cross_module_churn",
		Evidence: []string{
			"module(s) with 4+ implement actions: " + strings.Join(hotspots, ", "),
		},
		Suggest: "The hot module is being edited in place repeatedly. Before the next edit, confirm via domain.md that its responsibility (single-job rule, jig-wireframe §Step 1) is actually singular — repeated edits often mean two jobs in one node.",
	}
}

// modulePrefix strips a path down to a recognizable module. For
// "pkg/auth/handler.go" → "pkg/auth"; for "src/components/Card.tsx" →
// "src/components". Single-segment paths are returned as-is.
func modulePrefix(p string) string {
	if p == "" {
		return ""
	}
	p = filepath.ToSlash(p)
	// Strip a leading "./"
	p = strings.TrimPrefix(p, "./")
	parts := strings.Split(p, "/")
	if len(parts) <= 1 {
		return p
	}
	return strings.Join(parts[:len(parts)-1], "/")
}

// iToS is a minimal int-to-string to avoid importing strconv just for
// one-off diagnostic messages.
func iToS(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}
