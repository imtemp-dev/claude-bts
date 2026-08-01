package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// `bts docs check` — structural drift detection across maintained prose.
//
// bts ships four README translations plus llms.txt, and every one of them
// names commands and skills. Nothing checked that those names still exist,
// so a renamed command silently left four documents wrong. The Go tests
// cover behavior; nothing covered the documentation surface.
//
// The check is deliberately structural. It verifies names resolve, links
// point somewhere, and counted claims match what is shipped. It does not
// lint prose, tone, or translation fidelity: those need a human, and a
// keyword heuristic pretending otherwise would be noise.
//
// The source of truth for what is shipped is internal/template/templates/,
// NOT the repo's own deployed .claude/. That directory is an artifact of
// `bts init` on this project, is untracked, and legitimately lags the
// templates until `bts update` runs — `bts doctor` already reports that.

func init() {
	rootCmd.AddCommand(docsCmd)
	docsCmd.AddCommand(docsCheckCmd)
}

var docsCmd = &cobra.Command{
	Use:   "docs",
	Short: "Checks over the project's own documentation",
}

var docsCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Verify documentation names, links, and counted claims still hold",
	Long: `Structural checks over maintained prose surfaces:

  - every "/bts-<skill>" named in the docs is a shipped skill
  - every "bts <command>" named in the docs is a real CLI command
  - every relative markdown link resolves to a file that exists
  - counted claims ("N skills, N hooks, N rules") match what ships

Run from the bts source repository. Exits non-zero when anything fails, so
it can gate CI.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		repo, err := findSourceRepo()
		if err != nil {
			return err
		}
		surfaces, err := maintainedSurfaces(repo)
		if err != nil {
			return err
		}
		inv, err := shippedInventory(repo)
		if err != nil {
			return err
		}

		audiences, err := loadAudiences(repo)
		if err != nil {
			return err
		}

		var problems []string
		problems = append(problems, checkAudienceCoverage(surfaces, audiences)...)
		// Name and count checks describe what ships TODAY, so they apply
		// only to user-facing reference. A roadmap naming a command bts
		// does not have yet is the roadmap working.
		current := surfacesWithAudience(surfaces, audiences, audienceUserReference)
		problems = append(problems, checkSkillNames(repo, current, inv)...)
		problems = append(problems, checkCommandNames(repo, current)...)
		problems = append(problems, checkCountedClaims(repo, current, inv)...)
		// A broken link is a broken link on every surface.
		problems = append(problems, checkRelativeLinks(repo, surfaces)...)

		fmt.Printf("bts docs check — %d surface(s), %d skills, %d hooks, %d rules, %d agents\n",
			len(surfaces), len(inv.skills), inv.hooks, inv.rules, inv.agents)
		if len(problems) == 0 {
			fmt.Println("✓ No documentation drift found")
			return nil
		}
		for _, p := range problems {
			fmt.Printf("✗ %s\n", p)
		}
		return fmt.Errorf("%d documentation problem(s)", len(problems))
	},
}

// inventory is what the templates actually ship.
type inventory struct {
	skills []string
	hooks  int
	rules  int
	agents int
}

// findSourceRepo walks up looking for the template tree that defines what
// bts ships. The check is a maintainer tool; outside the source repo there
// is nothing to compare documentation against.
func findSourceRepo() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "internal", "template", "templates", ".claude")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("bts docs check runs in the bts source repository (internal/template/templates/.claude not found)")
}

func templateRoot(repo string) string {
	return filepath.Join(repo, "internal", "template", "templates", ".claude")
}

func shippedInventory(repo string) (*inventory, error) {
	inv := &inventory{}
	tmpl := templateRoot(repo)

	entries, err := os.ReadDir(filepath.Join(tmpl, "skills"))
	if err != nil {
		return nil, fmt.Errorf("read shipped skills: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			inv.skills = append(inv.skills, e.Name())
		}
	}
	sort.Strings(inv.skills)

	inv.hooks = countFiles(filepath.Join(tmpl, "hooks"), ".sh")
	inv.rules = countFiles(filepath.Join(tmpl, "rules"), ".md")
	inv.agents = countFiles(filepath.Join(tmpl, "agents"), ".md")
	return inv, nil
}

func countFiles(dir, ext string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ext) {
			n++
		}
	}
	return n
}

// maintainedSurfaces lists the prose that describes bts to its users and
// maintainers. Deliberately excludes .claude/ (a deployed artifact),
// internal/template/ (the shipped payload, checked as inventory rather
// than as prose), and docs/research/ (working notes, not maintained).
func maintainedSurfaces(repo string) ([]string, error) {
	var out []string
	candidates := []string{"README.md", "README.ko.md", "README.zh.md", "README.ja.md", "llms.txt"}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(repo, c)); err == nil {
			out = append(out, c)
		}
	}
	err := filepath.WalkDir(filepath.Join(repo, "docs"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "research" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		rel, rerr := filepath.Rel(repo, path)
		if rerr == nil {
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// Audience names, mirroring docs/audiences.json.
const (
	audienceUserReference = "user-reference"
)

// loadAudiences reads the classification that decides which checks apply
// to which surface.
func loadAudiences(repo string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(repo, "docs", "audiences.json"))
	if err != nil {
		return nil, fmt.Errorf("read docs/audiences.json: %w", err)
	}
	var doc struct {
		Surfaces map[string]string `json:"surfaces"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse docs/audiences.json: %w", err)
	}
	return doc.Surfaces, nil
}

// checkAudienceCoverage is the inventory check: a surface nobody
// classified is a surface nobody decided the rules for, and it would
// otherwise be silently exempt from everything.
func checkAudienceCoverage(surfaces []string, audiences map[string]string) []string {
	var problems []string
	known := map[string]bool{}
	for _, s := range surfaces {
		known[s] = true
		if _, ok := audiences[filepath.ToSlash(s)]; !ok {
			problems = append(problems, fmt.Sprintf(
				"%s is not classified in docs/audiences.json — add it so its checks are decided", s))
		}
	}
	classified := make([]string, 0, len(audiences))
	for s := range audiences {
		classified = append(classified, s)
	}
	sort.Strings(classified)
	for _, s := range classified {
		if !known[s] {
			problems = append(problems, fmt.Sprintf(
				"docs/audiences.json classifies %s, which no longer exists", s))
		}
	}
	return problems
}

func surfacesWithAudience(surfaces []string, audiences map[string]string, want string) []string {
	var out []string
	for _, s := range surfaces {
		if audiences[filepath.ToSlash(s)] == want {
			out = append(out, s)
		}
	}
	return out
}

var (
	// A slash-command reference, never a path segment: `scripts/bts-monitor.ts`
	// must not read as a reference to a skill named bts-monitor. RE2 has no
	// lookbehind, so the allowed preceding character is captured and skipped.
	skillRefRe = regexp.MustCompile("(?m)(^|[\\s`(\\[|])/(bts-[a-z0-9-]+)")
	cmdRefRe   = regexp.MustCompile("`bts ([a-z][a-z-]*)")
	linkRe     = regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)
)

// countedClaims are the "N skills" sentences that nobody revisits when a
// file is added. They are matched per language, because the translations
// carry the same claim and drift independently — README.md, README.ko.md,
// README.zh.md and README.ja.md all said 21 skills while 24 shipped.
//
// Each pattern captures the number immediately preceding a noun. The
// Korean "개" and Chinese "个" counters are optional so both "21개 스킬"
// and "21 스킬" match.
var countedClaims = []struct {
	concept string
	re      *regexp.Regexp
	// actual returns the shipped count for this concept.
	actual func(*inventory) int
}{
	{
		"skills",
		regexp.MustCompile(`(\d+)\s*(?:개|个)?\s*(?:skills|스킬|技能|スキル)`),
		func(i *inventory) int { return len(i.skills) },
	},
	{
		"lifecycle hooks",
		regexp.MustCompile(`(\d+)\s*(?:개|个)?\s*(?:lifecycle hooks|라이프사이클 훅|生命周期钩子|ライフサイクルフック)`),
		func(i *inventory) int { return i.hooks },
	},
	{
		"rules",
		regexp.MustCompile(`(\d+)\s*(?:개|个)?\s*(?:rules|규칙|规则|ルール)`),
		func(i *inventory) int { return i.rules },
	},
}

// checkSkillNames catches a renamed or removed skill still named in prose.
// `/bts-recipe` is the slash COMMAND (commands/bts-recipe.md), not a skill
// directory, so it is accepted as a known alias.
func checkSkillNames(repo string, surfaces []string, inv *inventory) []string {
	shipped := map[string]bool{}
	for _, s := range inv.skills {
		shipped[s] = true
	}
	// Slash commands live beside skills and are referenced the same way.
	cmdDir := filepath.Join(templateRoot(repo), "commands")
	if entries, err := os.ReadDir(cmdDir); err == nil {
		for _, e := range entries {
			shipped[strings.TrimSuffix(e.Name(), ".md")] = true
		}
	}

	var problems []string
	for _, surface := range surfaces {
		data, err := os.ReadFile(filepath.Join(repo, surface))
		if err != nil {
			continue
		}
		seen := map[string]bool{}
		for _, m := range skillRefRe.FindAllStringSubmatch(string(data), -1) {
			name := m[2]
			if shipped[name] || seen[name] {
				continue
			}
			seen[name] = true
			problems = append(problems, fmt.Sprintf(
				"%s references /%s, which is not a shipped skill or command", surface, name))
		}
	}
	return problems
}

// checkCommandNames catches documentation for a CLI command that no longer
// exists. Only backtick-quoted `bts <word>` is considered, so ordinary
// prose ("bts then verifies…") is not mistaken for a command.
func checkCommandNames(repo string, surfaces []string) []string {
	known := map[string]bool{}
	for _, c := range rootCmd.Commands() {
		known[c.Name()] = true
		for _, alias := range c.Aliases {
			known[alias] = true
		}
	}
	var problems []string
	for _, surface := range surfaces {
		data, err := os.ReadFile(filepath.Join(repo, surface))
		if err != nil {
			continue
		}
		seen := map[string]bool{}
		for _, m := range cmdRefRe.FindAllStringSubmatch(string(data), -1) {
			name := m[1]
			if known[name] || seen[name] {
				continue
			}
			seen[name] = true
			problems = append(problems, fmt.Sprintf(
				"%s documents `bts %s`, which is not a registered command", surface, name))
		}
	}
	return problems
}

// checkRelativeLinks verifies every relative markdown link resolves.
// External URLs and pure anchors are out of scope: this check is about
// files moving, not about the internet.
func checkRelativeLinks(repo string, surfaces []string) []string {
	var problems []string
	for _, surface := range surfaces {
		data, err := os.ReadFile(filepath.Join(repo, surface))
		if err != nil {
			continue
		}
		base := filepath.Dir(filepath.Join(repo, surface))
		for _, m := range linkRe.FindAllStringSubmatch(string(data), -1) {
			target := strings.TrimSpace(m[1])
			if target == "" || strings.HasPrefix(target, "#") ||
				strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			if i := strings.Index(target, "#"); i >= 0 {
				target = target[:i]
			}
			if target == "" {
				continue
			}
			if _, err := os.Stat(filepath.Join(base, target)); err != nil {
				problems = append(problems, fmt.Sprintf(
					"%s links to %s, which does not exist", surface, target))
			}
		}
	}
	return problems
}

// checkCountedClaims compares "N skills, N lifecycle hooks, N rules" against
// what the templates actually ship. A counted claim is the kind of sentence
// that is never revisited when a file is added.
func checkCountedClaims(repo string, surfaces []string, inv *inventory) []string {
	var problems []string
	for _, surface := range surfaces {
		data, err := os.ReadFile(filepath.Join(repo, surface))
		if err != nil {
			continue
		}
		for _, claim := range countedClaims {
			want := claim.actual(inv)
			reported := map[string]bool{}
			for _, m := range claim.re.FindAllStringSubmatch(string(data), -1) {
				if atoi(m[1]) == want || reported[m[0]] {
					continue
				}
				reported[m[0]] = true
				problems = append(problems, fmt.Sprintf(
					"%s says %q, but the templates ship %d %s",
					surface, strings.TrimSpace(m[0]), want, claim.concept))
			}
		}
	}
	return problems
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return -1
		}
		n = n*10 + int(r-'0')
	}
	return n
}
