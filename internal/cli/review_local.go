package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/feature"
	"github.com/imtemp-dev/claude-bts/internal/orca"
	"github.com/imtemp-dev/claude-bts/internal/review"
	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/imtemp-dev/claude-bts/internal/template"
	"github.com/imtemp-dev/claude-bts/pkg/version"
	"github.com/spf13/cobra"
)

// Local review pages (review.surface: local): bts serves the page itself
// from the project, so the review is tied to no claude.ai account and any
// session on this machine can take its rounds.  `bts review open` starts
// (or reuses) the project's server and opens the page; `bts review wait`
// is what a session runs in the background to be woken by the next round.

func init() {
	reviewCmd.AddCommand(reviewOpenCmd, reviewServeCmd, reviewWaitCmd, reviewReplyCmd)
	reviewOpenCmd.Flags().String("point", "", "Review point the page carries: intent, scope or spec; a feature's page carries feature")
	reviewOpenCmd.Flags().Bool("no-browser", false, "Print the link only; open no browser tab")
	reviewServeCmd.Flags().String("root", "", "The bts project to serve (default: the working directory's)")
	reviewServeCmd.Flags().Bool("stop", false, "Stop the project's running server")
	reviewServeCmd.Flags().Duration("idle", 12*time.Hour, "Exit after this long with no request and no open page (0: never)")
	reviewWaitCmd.Flags().Duration("timeout", 100*time.Minute, "Give up after this long and say no round came (run it again)")
	reviewReplyCmd.Flags().String("note", "", "Note id")
	reviewReplyCmd.Flags().String("status", "", "answered, proposed, page, declined, or for a confirmation applied or declined")
	reviewReplyCmd.Flags().String("reply", "", "One or two sentences in the user's language, shown on the pin")
}

// kitFS is the review kit embedded in this binary, by base name.
func kitFS() (fs.FS, error) {
	t, err := template.EmbeddedTemplates()
	if err != nil {
		return nil, err
	}
	return fs.Sub(t, ".claude/skills/bts-review-page/kit")
}

// serverResolver turns a server's subject key into its review, for the
// project at root.
func serverResolver(root string) func(string) (review.Subject, error) {
	return func(key string) (review.Subject, error) {
		if id, ok := strings.CutPrefix(key, "feature:"); ok {
			dir, err := feature.Find(id)
			if err != nil {
				return review.Subject{}, err
			}
			return feature.ReviewSubject(root, dir)
		}
		if !review.ValidDocID(key) {
			return review.Subject{}, fmt.Errorf("no review %q", key)
		}
		if fi, err := os.Stat(state.RecipeDir(root, key)); err != nil || !fi.IsDir() {
			return review.Subject{}, fmt.Errorf("no recipe %s in this project", key)
		}
		return review.RecipeSubject(root, key), nil
	}
}

// ensureServer returns the project's running server, starting one in the
// background when none answers or the running one is another bts build.
func ensureServer(root string) (*review.ServerInfo, error) {
	info, err := review.ReadServerInfo(root)
	if err != nil {
		return nil, err
	}
	if info.Healthy() && info.Version == version.GetVersion() {
		return info, nil
	}
	if info != nil {
		if _, err := review.StopServer(root); err != nil {
			return nil, fmt.Errorf("stopping the old review server: %w", err)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	logPath := filepath.Join(state.LocalPath(root), "review-server.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		return nil, err
	}
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	defer logf.Close()
	cmd := exec.Command(exe, "review", "serve", "--root", root)
	cmd.Dir = root
	cmd.Stdout, cmd.Stderr = logf, logf
	// Its own session: the server outlives the command that started it
	// and the terminal's job control.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	for i := 0; i < 100; i++ {
		time.Sleep(50 * time.Millisecond)
		if info, _ := review.ReadServerInfo(root); info != nil && info.PID == pid && info.Healthy() {
			return info, nil
		}
	}
	return nil, fmt.Errorf("the review server did not start; see %s", logPath)
}

// openInBrowser shows the page: in Orca's own browser beside the terminal
// when the session runs in Orca, otherwise in the system browser.
func openInBrowser(root, link string) {
	if orca.InOrca() {
		if openInOrcaTab(root, link, "[bts] opened in Orca's browser beside this terminal") {
			return
		}
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", link)
	default:
		cmd = exec.Command("xdg-open", link)
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[bts] could not open a browser (%v); open the link above\n", err)
		return
	}
	_ = cmd.Process.Release()
	fmt.Println("[bts] opened in the system browser")
}

var reviewOpenCmd = &cobra.Command{
	Use:   "open [recipe-id] --point intent|scope|spec  |  open --feature <feature>",
	Short: "Serve the review page from this machine and open it (review.surface: local)",
	Long: `Starts the project's review server, or reuses it, records the page's local
link in review.json in place of a claude.ai link, and opens the page in
Orca's browser beside the terminal (or the system browser).  Write the page
first: ` + "`bts review page`" + ` prints where and what it shows.  The page's notes,
rounds and proposals live in the review's data/ directory, where ` + "`bts review\nimport`" + ` reads them; run ` + "`bts review wait`" + ` in the background to be woken by the
next round.`,
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		sub, err := resolveSubject(cmd, args)
		if err != nil {
			return err
		}
		point, _ := cmd.Flags().GetString("point")
		if point == "" && sub.Kind == review.KindFeature {
			point = review.FeaturePoint
		}
		if point != "" && !containsStr(sub.Points(), point) {
			return fmt.Errorf("--point takes %s, got %q", strings.Join(sub.Points(), " or "), point)
		}
		if _, err := os.Stat(filepath.Join(sub.Work, "page.html")); err != nil {
			return fmt.Errorf("no page yet: write %s first (`bts review page %s` prints what it shows)", filepath.Join(sub.Work, "page.html"), sub.Arg())
		}
		info, err := ensureServer(sub.Root)
		if err != nil {
			return err
		}
		link := info.PageURL(review.SubjectKey(sub))
		prev := ""
		if old, err := review.Load(sub); err == nil {
			prev = old.URL
		}
		s, err := review.AttachLocal(sub, link, point)
		if err != nil {
			return err
		}
		fmt.Printf("[bts] review page for %s %s, served from this machine: %s (points: %s)\n", sub.Kind, sub.ID, link, strings.Join(s.Points, ", "))
		if noBrowser, _ := cmd.Flags().GetBool("no-browser"); !noBrowser && link != prev {
			openInBrowser(sub.Root, link)
		}
		fmt.Printf("  next: run `bts review wait %s` in the background (run_in_background) and end the turn; it returns when the user sends a round\n", sub.Arg())
		return nil
	},
}

var reviewServeCmd = &cobra.Command{
	Use:          "serve [--stop]",
	Short:        "Run the project's local review server in the foreground (bts review open starts it)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, _ := cmd.Flags().GetString("root")
		if root == "" {
			root = cwdRoot()
		}
		if root == "" {
			return errors.New("run this inside a bts project, or pass --root")
		}
		if stop, _ := cmd.Flags().GetBool("stop"); stop {
			running, err := review.StopServer(root)
			if err != nil {
				return err
			}
			if running {
				fmt.Println("[bts] review server stopped")
			} else {
				fmt.Println("[bts] no review server was running")
			}
			return nil
		}
		kit, err := kitFS()
		if err != nil {
			return err
		}
		idle, _ := cmd.Flags().GetDuration("idle")
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return review.Serve(ctx, review.ServeOptions{
			Root: root, Version: version.GetVersion(), Resolve: serverResolver(root), Kit: kit, Idle: idle,
			Ready: func(info review.ServerInfo) {
				fmt.Printf("[bts] review server for %s on http://127.0.0.1:%d (pid %d)\n", root, info.Port, info.PID)
			},
		})
	},
}

var reviewWaitCmd = &cobra.Command{
	Use:   "wait [recipe-id]  |  wait --feature <feature>",
	Short: "Wait for the user to send a round from a local review page, then print it",
	Long: `Returns as soon as the page holds a round the user sent that no session has
taken yet, or after --timeout with "no round yet".  Run it with
run_in_background after ` + "`bts review open`" + ` and after each round: Claude Code wakes
the session when it returns.  While it waits the page shows that a session
is waiting.`,
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		sub, err := resolveSubject(cmd, args)
		if err != nil {
			return err
		}
		if !review.IsLocal(sub) {
			return fmt.Errorf("%s %s has no local review page: `bts review open %s` serves one", sub.Kind, sub.ID, sub.Arg())
		}
		timeout, _ := cmd.Flags().GetDuration("timeout")
		deadline := time.Now().Add(timeout)
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		defer review.ClearWaiting(sub)
		for {
			_ = review.TouchWaiting(sub)
			rounds, err := review.SubmittedRounds(sub)
			if err != nil {
				return err
			}
			if len(rounds) > 0 {
				nums := make([]string, len(rounds))
				for i, n := range rounds {
					nums[i] = fmt.Sprint(n)
				}
				fmt.Printf("[bts] round %s sent from the review page of %s %s\n", strings.Join(nums, ", "), sub.Kind, sub.ID)
				fmt.Printf("  next: `bts review import %s`, then handle it as /bts-review-page says\n", sub.Arg())
				return nil
			}
			if timeout > 0 && time.Now().After(deadline) {
				fmt.Printf("[bts] no round yet from the review page of %s %s: run `bts review wait %s` again\n", sub.Kind, sub.ID, sub.Arg())
				return nil
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(2 * time.Second):
			}
		}
	},
}

var reviewReplyCmd = &cobra.Command{
	Use:   "reply [recipe-id] --note <id> --status <status> --reply <text>",
	Short: "Write the session's answer onto a note on the page",
	Long: `Sets a note's status and the reply shown on its pin.  dispose and propose
take --reply for review notes; this is for the rest, a confirmation's
outcome above all (--status applied, or declined with bts's reason).  On a
local page it lands in the page data; on a claude.ai page it writes the
document to upload (notes collection, ArtifactData update).`,
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		sub, err := resolveSubject(cmd, args)
		if err != nil {
			return err
		}
		note, _ := cmd.Flags().GetString("note")
		status, _ := cmd.Flags().GetString("status")
		reply, _ := cmd.Flags().GetString("reply")
		path, err := review.ReplyNote(sub, strings.TrimSpace(note), strings.TrimSpace(status), reply)
		if err != nil {
			return err
		}
		printNoteWrite(sub, note, path)
		return nil
	},
}

// printNoteWrite says where a note's answer went.
func printNoteWrite(sub review.Subject, note, path string) {
	if review.IsLocal(sub) {
		fmt.Printf("  on the page: note %s\n", note)
		return
	}
	fmt.Printf("  upload: ArtifactData update, collection notes, doc_id %s, file_path %s\n", note, path)
}
