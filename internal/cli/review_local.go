package cli

import (
	"context"
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
	"github.com/imtemp-dev/claude-bts/internal/pages"
	"github.com/imtemp-dev/claude-bts/internal/review"
	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/imtemp-dev/claude-bts/internal/template"
	"github.com/imtemp-dev/claude-bts/pkg/version"
	"github.com/spf13/cobra"
)

// Local review pages (review.surface: local): the user's page server
// (`bts pages serve`) serves the page from this machine, so the review is
// tied to no claude.ai account and any session on this machine can take
// its rounds.  `bts review open` starts the server when it is down,
// registers the project and opens the page; `bts review wait` is what a
// session runs in the background to be woken by the next round.

func init() {
	reviewCmd.AddCommand(reviewOpenCmd, reviewWaitCmd, reviewReplyCmd, reviewExportCmd)
	reviewOpenCmd.Flags().String("point", "", "Review point the page carries: intent, scope or spec; a feature's page carries feature")
	reviewOpenCmd.Flags().Bool("no-browser", false, "Print the links only; open no browser tab")
	reviewWaitCmd.Flags().Duration("timeout", 100*time.Minute, "Give up after this long and say no round came (run it again)")
	reviewReplyCmd.Flags().String("note", "", "Note id")
	reviewReplyCmd.Flags().String("status", "", "answered, proposed, page, declined, or for a confirmation applied or declined")
	reviewReplyCmd.Flags().String("reply", "", "One or two sentences in the user's language, shown on the pin")
	reviewExportCmd.Flags().StringP("output", "o", "", "Where to write the file (default: ~/Downloads/<recipe or feature>-review.html)")
	reviewExportCmd.Flags().Bool("embed-remote", false, "Download remote scripts, stylesheets and images and inline them too")
}

// kitFS is the review kit embedded in this binary, by base name.
func kitFS() (fs.FS, error) {
	t, err := template.EmbeddedTemplates()
	if err != nil {
		return nil, err
	}
	return fs.Sub(t, ".claude/skills/bts-review-page/kit")
}

// reviewResolver turns a project root and a subject key into its review.
func reviewResolver(root, key string) (review.Subject, error) {
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

// pageServer is the user's running page server, started when none
// answers, with what links need: the store, its config and the token.
type pageServer struct {
	Home  string
	Cfg   pages.Config
	Token string
	Info  *pages.ServerInfo
}

func thisExe() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		return r
	}
	return exe
}

func ensurePageServer() (*pageServer, error) {
	home, err := pages.Home()
	if err != nil {
		return nil, err
	}
	cfg, err := pages.LoadConfig(home)
	if err != nil {
		return nil, err
	}
	token, err := pages.Token(home)
	if err != nil {
		return nil, err
	}
	info, notice, err := pages.Ensure(pages.EnsureOptions{Home: home, Version: version.GetVersion(), Exe: thisExe()})
	if err != nil {
		return nil, err
	}
	if notice != "" {
		fmt.Fprintln(os.Stderr, "[bts] "+notice)
	}
	// Links go to the port the server listens on, whatever the config
	// says now.
	cfg.Port = info.Port
	return &pageServer{Home: home, Cfg: cfg, Token: token, Info: info}, nil
}

// printLinks prints a page's links: on this machine, and on the tailnet.
func printLinks(l pages.Links) {
	fmt.Printf("  this Mac: %s\n", l.Local)
	if l.Public != "" {
		fmt.Printf("  tailnet:  %s\n", l.Public)
	} else {
		fmt.Println("  tailnet:  not set up (`bts pages setup` maps it, for the owner's phone)")
	}
}

// openInBrowser shows the page: in Orca's own browser beside the terminal
// when the session runs in Orca, otherwise in the system browser.
func openInBrowser(root, link string) {
	if orca.InOrca() {
		dir := root
		if dir == "" {
			dir, _ = os.Getwd()
		}
		if openInOrcaTab(dir, link, "[bts] opened in Orca's browser beside this terminal") {
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
	Long: `Starts the user's page server when it is down (` + "`bts pages serve`" + `), registers
this project with it, records the page's local link in review.json in place
of a claude.ai link, prints the page's links (this Mac, and the tailnet when
` + "`bts pages setup`" + ` mapped it), and opens the page in Orca's browser beside the
terminal (or the system browser).  Write the page first: ` + "`bts review page`" + `
prints where and what it shows.  The page's notes, rounds and proposals live
in the review's data/ directory, where ` + "`bts review import`" + ` reads them; run
` + "`bts review wait`" + ` in the background to be woken by the next round.`,
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
		if stopped, err := review.StopLegacyServer(sub.Root); err != nil {
			fmt.Fprintf(os.Stderr, "[bts] could not stop this project's old review server: %v\n", err)
		} else if stopped {
			fmt.Println("[bts] stopped this project's old review server; the user's page server serves its pages now")
		}
		ps, err := ensurePageServer()
		if err != nil {
			return err
		}
		proj, err := pages.RegisterProject(ps.Home, sub.Root)
		if err != nil {
			return err
		}
		links := pages.LinksFor(ps.Cfg, ps.Token, pages.ReviewPath(proj, sub))
		prev := ""
		if old, err := review.Load(sub); err == nil {
			prev = old.URL
		}
		s, err := review.AttachLocal(sub, links.Record, point)
		if err != nil {
			return err
		}
		fmt.Printf("[bts] review page for %s %s, served from this machine (points: %s)\n", sub.Kind, sub.ID, strings.Join(s.Points, ", "))
		printLinks(links)
		if noBrowser, _ := cmd.Flags().GetBool("no-browser"); !noBrowser && links.Record != prev {
			openInBrowser(sub.Root, links.Local)
		}
		fmt.Printf("  next: run `bts review wait %s` in the background (run_in_background) and end the turn; it returns when the user sends a round\n", sub.Arg())
		return nil
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

var reviewExportCmd = &cobra.Command{
	Use:   "export [recipe-id]  |  export --feature <feature>",
	Short: "Write the review page as one read-only HTML file to share",
	Long: `Writes the subject's review page as one HTML file that opens with no server,
for people outside the owner's tailnet.  It is a read-only snapshot: the
review kit's controls are left out, and no notes, rounds or replies are
included.  Local files the page loads are inlined; remote ones stay links
unless --embed-remote.  What could not be embedded is listed.  bts only
writes the file; sending it is a separate step.`,
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		sub, err := resolveSubject(cmd, args)
		if err != nil {
			return err
		}
		doc, err := os.ReadFile(filepath.Join(sub.Work, "page.html"))
		if err != nil {
			return fmt.Errorf("no page to export: %s", filepath.Join(sub.Work, "page.html"))
		}
		kit, err := kitFS()
		if err != nil {
			return err
		}
		dir := pages.DirResolver(sub.Work)
		resolve := func(ref string) ([]byte, error) {
			if ref == "bts-review-kit.css" {
				return fs.ReadFile(kit, ref)
			}
			return dir(ref)
		}
		title := pages.HTMLTitle(doc)
		if title == "" {
			title = sub.Kind + " " + sub.ID
		}
		embed, _ := cmd.Flags().GetBool("embed-remote")
		out, rep, err := pages.Export(review.SetPageConfig(doc, map[string]interface{}{"transport": "static"}), pages.ExportOptions{
			Resolve:     resolve,
			EmbedRemote: embed,
			Drop:        func(src string) bool { return filepath.Base(src) == "bts-review-kit.js" },
			LocalHosts:  exportLocalHosts(),
			Footer:      fmt.Sprintf("%s · review snapshot of %s %s · exported from bts on %s", title, sub.Kind, sub.ID, time.Now().Format("2006-01-02")),
		})
		if err != nil {
			return err
		}
		name := sub.ID + "-review.html"
		return writeExport(cmd, name, out, rep)
	},
}

// exportLocalHosts are the host names a teammate cannot reach.
func exportLocalHosts() []string {
	hosts := []string{"127.0.0.1", "localhost"}
	if home, err := pages.Home(); err == nil {
		if cfg, err := pages.LoadConfig(home); err == nil {
			if h := cfg.PublicHost(); h != "" {
				if i := strings.LastIndex(h, ":"); i > 0 {
					h = h[:i]
				}
				hosts = append(hosts, h)
			}
		}
	}
	return hosts
}

// exportPath is where an export goes: -o, else ~/Downloads, else the
// store's exports/ directory.
func exportPath(cmd *cobra.Command, name string) (string, error) {
	if o, _ := cmd.Flags().GetString("output"); o != "" {
		return filepath.Abs(o)
	}
	if h, err := os.UserHomeDir(); err == nil {
		d := filepath.Join(h, "Downloads")
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			return filepath.Join(d, name), nil
		}
	}
	home, err := pages.Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "exports", name), nil
}

func writeExport(cmd *cobra.Command, name string, out []byte, rep *pages.ExportReport) error {
	dest, err := exportPath(cmd, name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(dest, out, 0644); err != nil {
		return err
	}
	fmt.Printf("[bts] exported to %s (%d KB)\n", dest, (len(out)+1023)/1024)
	if len(rep.Embedded) > 0 {
		fmt.Printf("  inlined: %s\n", strings.Join(rep.Embedded, ", "))
	}
	if len(rep.Remote) > 0 {
		fmt.Printf("  left as links (load from the network; --embed-remote inlines them): %s\n", strings.Join(rep.Remote, ", "))
	}
	for _, m := range rep.Missing {
		fmt.Printf("  missing (the file shows the page without it): %s\n", m)
	}
	for _, w := range rep.Warnings {
		fmt.Printf("  note: %s\n", w)
	}
	return nil
}
