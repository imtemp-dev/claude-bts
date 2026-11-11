package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/imtemp-dev/claude-bts/internal/multica"
	"github.com/imtemp-dev/claude-bts/internal/template"
	"github.com/imtemp-dev/claude-bts/pkg/version"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().Bool("force", false, "Reinitialize (overwrites existing bts files)")
	initCmd.Flags().Bool("exclude", false, "For a repository bts only visits: keep bts's files out of git through the repository's info/exclude (shared by all its worktrees) instead of .gitignore, change no tracked file, and leave out .mcp.json and .vscode/")
	initCmd.Flags().String("session-root", "", "Also link a Claude session root that sits above the project (a Multica task's workdir): symlinks to bts's skills, agents, rules and commands, bts's hooks in its settings.local.json, and a .bts-root pointer")
	initCmd.Flags().Bool("multica", false, "Inside a Multica run, from the task's workdir: initialize the repository worktree below it (or [directory]) with --exclude and link the workdir as the session root.  Safe to run again")
}

var initCmd = &cobra.Command{
	Use:     "init [directory]",
	Short:   "Initialize bts in a project",
	Long:    "Deploy skills, agents, hooks, and rules to .claude/ and create .bts/ for state management.",
	Args:    cobra.MaximumNArgs(1),
	GroupID: "project",
	RunE:    runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")
	exclude, _ := cmd.Flags().GetBool("exclude")
	sessionRoot, _ := cmd.Flags().GetString("session-root")
	multicaMode, _ := cmd.Flags().GetBool("multica")

	// Determine project root
	projectRoot := "."
	if len(args) > 0 {
		projectRoot = args[0]
	}

	if multicaMode {
		cwd, _ := os.Getwd()
		tc := multica.FindTaskContext(cwd)
		if tc == nil {
			return fmt.Errorf("no Multica task here: run bts init --multica from the task's workdir, the directory holding %s", multica.ContextFile)
		}
		sessionRoot = tc.Dir
		if len(args) == 0 {
			wt, err := findWorktree(tc.Dir)
			if err != nil {
				return err
			}
			projectRoot = wt
		}
		exclude = true
		switch {
		case tc.IssueID != "":
			fmt.Printf("Multica task for issue %s\n", tc.IssueID)
		case tc.ChatSessionID != "":
			fmt.Printf("Multica chat task %s\n", tc.ChatSessionID)
		}
	}

	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	// Check if already initialized
	btsDir := filepath.Join(absRoot, ".bts")
	if _, err := os.Stat(btsDir); err == nil && !force {
		if !multicaMode {
			return fmt.Errorf(".bts/ already exists. Use --force to reinitialize")
		}
		// A later run on the same task, or a fresh bare cache under an
		// existing worktree: make sure of the exclusion and the link only.
		if err := ensureExcludeReport(absRoot); err != nil {
			return err
		}
		return linkSessionReport(sessionRoot, absRoot)
	}

	if exclude {
		if err := ensureExcludeReport(absRoot); err != nil {
			return err
		}
	}

	fmt.Println("Initializing bts...")

	// Create .bts directories
	stateDirs := []string{
		filepath.Join(btsDir, "config"),
		filepath.Join(btsDir, "specs", "recipes"),
		filepath.Join(btsDir, "specs", "debates"),
		filepath.Join(btsDir, "local"),
	}
	for _, dir := range stateDirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}

	// Deploy templates. .gitignore is user-owned — never deploy/overwrite it;
	// EnsureGitignore below merges the bts rule in without clobbering user rules.
	skipFiles := []string{".bts/config/settings.yaml", ".mcp.json", ".gitignore"}
	if exclude {
		skipFiles = append(skipFiles, ".vscode/markdown.code-snippets")
	}
	var created []string
	switch {
	case force:
		created, err = template.DeployForce(absRoot, skipFiles)
	case exclude:
		created, err = template.DeployExcept(absRoot, template.SkipInExcludeMode)
	default:
		created, err = template.Deploy(absRoot)
	}
	if err != nil {
		return fmt.Errorf("deploy templates: %w", err)
	}

	if exclude {
		// .gitignore is tracked; under --exclude it is never written.  Say so
		// if git does not ignore bts's files after all.
		if exec.Command("git", "-C", absRoot, "check-ignore", "-q", ".bts/local/probe").Run() != nil {
			fmt.Fprintf(os.Stderr, "warning: git does not ignore .bts/ under %s; check %s\n", absRoot, excludePathOrUnknown(absRoot))
		}
	} else if err := template.EnsureGitignore(absRoot); err != nil {
		// Ensure .gitignore ignores bts local data without destroying existing rules.
		fmt.Fprintf(os.Stderr, "warning: update .gitignore: %v\n", err)
	}

	// Record template version
	tv := version.GetVersion()
	if version.Commit != "none" && len(version.Commit) >= 7 {
		tv += "-" + version.Commit[:7]
	}
	if err := os.WriteFile(filepath.Join(absRoot, ".bts", "config", ".template-version"), []byte(tv), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "warning: save template version: %v\n", err)
	}
	if h := template.TemplatesHash(); h != "" {
		if err := os.WriteFile(filepath.Join(absRoot, filepath.FromSlash(template.TemplateHashFile)), []byte(h+"\n"), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "warning: save template hash: %v\n", err)
		}
	}

	// Merge statusline and hook configs into .claude/settings.local.json
	if err := template.MergeHookSettings(absRoot); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not configure hooks: %v\n", err)
	}

	fmt.Printf("\nbts initialized successfully.\n")
	fmt.Printf("  Files created: %d\n", len(created))
	fmt.Printf("  Skills:        .claude/skills/bts-*/\n")
	fmt.Printf("  Agents:        .claude/agents/bts-*/\n")
	fmt.Printf("  Commands:      .claude/commands/bts-*/\n")
	fmt.Printf("  Rules:         .claude/rules/bts-*/\n")
	fmt.Printf("  Hooks:         .claude/hooks/bts-*/\n")
	fmt.Printf("  State:         .bts/\n")
	if !exclude {
		fmt.Printf("  VS Code:       .vscode/markdown.code-snippets (type btsc/btscb/btscq + Tab in any .md)\n")
	}

	if sessionRoot != "" {
		if err := linkSessionReport(sessionRoot, absRoot); err != nil {
			return err
		}
	}

	fmt.Printf("\nStart Claude Code and try: /bts-recipe-blueprint \"your feature\"\n")

	return nil
}

// ensureExcludeReport writes the bts block into the repository's info/exclude
// and says what it did.
func ensureExcludeReport(root string) error {
	wrote, err := template.EnsureExclude(root)
	if err != nil {
		return fmt.Errorf("--exclude: %w", err)
	}
	path := excludePathOrUnknown(root)
	if wrote {
		fmt.Printf("  Exclude:       bts block added to %s\n", path)
	} else {
		fmt.Printf("  Exclude:       already in %s\n", path)
	}
	return nil
}

func excludePathOrUnknown(root string) string {
	if p, err := template.ExcludePath(root); err == nil {
		return p
	}
	return "the repository's info/exclude"
}

// linkSessionReport links the session root to the project and says what it did.
func linkSessionReport(sessionRoot, root string) error {
	res, err := template.LinkSession(sessionRoot, root)
	if err != nil {
		return fmt.Errorf("--session-root: %w", err)
	}
	fmt.Printf("  Session root:  %d links, hooks in %s, pointer %s\n", res.Links, res.Settings, res.Pointer)
	for _, p := range res.Skipped {
		fmt.Fprintf(os.Stderr, "warning: %s exists and is not a symlink; left alone\n", p)
	}
	return nil
}

// findWorktree returns the one git checkout directly under dir: the
// repository a Multica task checked out into its workdir.
func findWorktree(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var found []string
	for _, e := range entries {
		if !e.IsDir() || e.Name()[0] == '.' {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, e.Name(), ".git")); err == nil {
			found = append(found, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(found)
	switch len(found) {
	case 0:
		return "", fmt.Errorf("no repository under %s: check one out first (multica repo checkout <url>), then run bts init --multica again", dir)
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("several repositories under %s (%v): name the one bts should use, bts init --multica <directory>", dir, found)
	}
}
