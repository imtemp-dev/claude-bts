package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/pages"
	"github.com/imtemp-dev/claude-bts/pkg/version"
	"github.com/spf13/cobra"
)

// `bts pages` runs and wires the user's page server: one server on this
// machine for every bts page, review pages included, on a fixed port bound
// to 127.0.0.1, reachable from the owner's other devices through
// `tailscale serve` once `bts pages setup` mapped it.

func init() {
	rootCmd.AddCommand(pagesCmd)
	pagesCmd.AddCommand(pagesServeCmd, pagesStatusCmd, pagesStopCmd, pagesOpenCmd, pagesSetupCmd, pagesTeardownCmd, pagesTokenCmd)
	pagesServeCmd.Flags().Bool("launchd", false, "Run under the LaunchAgent: never exit when idle")
	pagesServeCmd.Flags().Duration("idle", 12*time.Hour, "Exit after this long with no request and no open page (0: never)")
	pagesOpenCmd.Flags().Bool("no-browser", false, "Print the links only")
	pagesSetupCmd.Flags().Bool("dry-run", false, "Say what each step would do, change nothing")
	pagesSetupCmd.Flags().Int("port", 0, "The port to use, locally and on the tailnet (default: the configured one, else 7337)")
	pagesSetupCmd.Flags().Bool("no-hook", false, "Skip the user-level Artifact guard")
	pagesSetupCmd.Flags().Bool("no-tailscale", false, "Skip the tailnet mapping")
	pagesSetupCmd.Flags().Bool("no-agent", false, "Skip the LaunchAgent: the server starts on demand")
	pagesTeardownCmd.Flags().Bool("dry-run", false, "Say what each step would do, change nothing")
	pagesTokenCmd.Flags().Bool("rotate", false, "Replace the token: every link printed before stops working")
}

var pagesCmd = &cobra.Command{
	Use:   "pages",
	Short: "Run and set up the page server that serves every bts page from this machine",
}

var pagesServeCmd = &cobra.Command{
	Use:          "serve [--launchd]",
	Short:        "Run the page server in the foreground (commands that need it start it)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := pages.Home()
		if err != nil {
			return err
		}
		kit, err := kitFS()
		if err != nil {
			return err
		}
		idle, _ := cmd.Flags().GetDuration("idle")
		managed := ""
		if l, _ := cmd.Flags().GetBool("launchd"); l {
			managed, idle = "launchd", 0
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return pages.Serve(ctx, pages.ServeOptions{
			Home: home, Version: version.GetVersion(), Exe: thisExe(), Managed: managed,
			Kit: kit, ResolveReview: reviewResolver, Idle: idle,
			Ready: func(info pages.ServerInfo) {
				fmt.Printf("[bts] page server on http://127.0.0.1:%d (pid %d, bts %s, %s)\n", info.Port, info.PID, info.Version, time.Now().Format(time.RFC3339))
			},
		})
	},
}

var pagesStatusCmd = &cobra.Command{
	Use:          "status",
	Short:        "Show the page server, its tailnet mapping, the Artifact guard and the LaunchAgent",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := pages.Home()
		if err != nil {
			return err
		}
		cfg, err := pages.LoadConfig(home)
		if err != nil {
			return err
		}
		token, err := pages.Token(home)
		if err != nil {
			return err
		}
		fmt.Printf("store:     %s\n", home)
		info, _ := pages.ReadServerInfo(home)
		if info.Healthy(token) {
			how := "started on demand"
			if info.Managed != "" {
				how = "kept up by " + info.Managed
			}
			fmt.Printf("server:    running on 127.0.0.1:%d, pid %d, bts %s from %s, %s\n", info.Port, info.PID, info.Version, info.Exe, how)
		} else {
			fmt.Printf("server:    not running (port %d; a command that needs it starts it)\n", cfg.ListenPort())
		}
		if cfg.PublicURL != "" {
			m, err := pages.ServeMapping(cfg.ListenPort())
			switch {
			case err != nil:
				fmt.Printf("tailnet:   %s (could not read tailscale: %v)\n", cfg.PublicURL, err)
			case m == pages.LocalTarget(cfg.ListenPort()):
				fmt.Printf("tailnet:   %s -> %s\n", cfg.PublicURL, m)
			case m == "":
				fmt.Printf("tailnet:   %s recorded, but tailscale no longer maps it (run `bts pages setup`)\n", cfg.PublicURL)
			default:
				fmt.Printf("tailnet:   %s maps to %s, not this server\n", cfg.PublicURL, m)
			}
		} else {
			fmt.Println("tailnet:   not set up")
		}
		if sp, err := pages.ClaudeSettingsPath(); err == nil {
			if pages.GuardInstalled(sp) {
				fmt.Printf("guard:     in %s\n", sp)
			} else {
				fmt.Printf("guard:     not in %s (new pages still reach claude.ai)\n", sp)
			}
		}
		switch {
		case pages.AgentLoaded():
			fmt.Printf("agent:     %s loaded\n", pages.AgentLabel)
		case pages.AgentInstalled():
			fmt.Printf("agent:     %s installed but not loaded\n", pages.AgentLabel)
		default:
			fmt.Println("agent:     none (the server starts on demand and exits after 12 idle hours)")
		}
		list, _ := pages.List(home)
		projects, _ := pages.Projects(home)
		fmt.Printf("pages:     %d; projects with review pages: %d\n", len(list), len(projects))
		return nil
	},
}

var pagesStopCmd = &cobra.Command{
	Use:          "stop",
	Short:        "Stop the page server (under the LaunchAgent, launchd starts it again)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := pages.Home()
		if err != nil {
			return err
		}
		running, err := pages.Stop(home)
		if err != nil {
			return err
		}
		switch {
		case !running:
			fmt.Println("[bts] no page server was running")
		case pages.AgentLoaded():
			fmt.Println("[bts] page server stopped; the LaunchAgent starts it again (`bts pages teardown` removes the agent)")
		default:
			fmt.Println("[bts] page server stopped")
		}
		return nil
	},
}

var pagesOpenCmd = &cobra.Command{
	Use:          "open",
	Short:        "Print the links to the index of every page and open it",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ps, err := ensurePageServer()
		if err != nil {
			return err
		}
		links := pages.LinksFor(ps.Cfg, ps.Token, "/")
		fmt.Println("[bts] the index of every page")
		printLinks(links)
		if noBrowser, _ := cmd.Flags().GetBool("no-browser"); !noBrowser {
			openInBrowser(cwdRoot(), links.Local)
		}
		return nil
	},
}

var pagesSetupCmd = &cobra.Command{
	Use:   "setup [--dry-run] [--port N] [--no-hook] [--no-tailscale] [--no-agent]",
	Short: "Wire the pages into this machine: config, the Artifact guard, the tailnet mapping, the LaunchAgent",
	Long: `Each step runs once and says what it changed:

  1. the store's config and token (~/.bts/pages)
  2. the user-level Artifact guard in ~/.claude/settings.json: every session
     on this machine publishes a new page with ` + "`bts page publish`" + ` instead of
     as a claude.ai artifact (a copy of the file is kept beside it)
  3. ` + "`tailscale serve --bg --https=<port> http://127.0.0.1:<port>`" + `, tailnet only,
     refused if that tailnet port already maps elsewhere; the tailnet URL is
     recorded so links carry it
  4. a LaunchAgent that keeps the server running (macOS)

` + "`bts pages teardown`" + ` undoes steps 2 to 4.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		dry, _ := cmd.Flags().GetBool("dry-run")
		home, err := pages.Home()
		if err != nil {
			return err
		}
		cfg, err := pages.LoadConfig(home)
		if err != nil {
			return err
		}
		if p, _ := cmd.Flags().GetInt("port"); p > 0 {
			cfg.Port = p
		}
		port := cfg.ListenPort()
		say := func(done, would string) {
			if dry {
				fmt.Println("would: " + would)
			} else {
				fmt.Println(done)
			}
		}

		// 1. config and token
		if dry {
			fmt.Printf("would: keep the store in %s, port %d\n", home, port)
		} else {
			if err := pages.SaveConfig(home, cfg); err != nil {
				return err
			}
			if _, err := pages.Token(home); err != nil {
				return err
			}
			fmt.Printf("store: %s, port %d\n", home, port)
		}

		// 2. the guard
		if skip, _ := cmd.Flags().GetBool("no-hook"); !skip {
			sp, err := pages.ClaudeSettingsPath()
			if err != nil {
				return err
			}
			if pages.GuardInstalled(sp) {
				fmt.Printf("guard: already in %s\n", sp)
			} else if dry {
				fmt.Printf("would: add a PreToolUse entry (matcher Artifact, `%s`) to %s, keeping a copy of the file\n", pages.GuardCommand, sp)
			} else {
				if _, err := pages.SetGuard(sp, true); err != nil {
					return err
				}
				fmt.Printf("guard: added to %s (a copy of the file as it was sits beside it)\n", sp)
			}
		}

		// 3. the tailnet
		if skip, _ := cmd.Flags().GetBool("no-tailscale"); !skip {
			name, err := pages.TailnetName()
			if err != nil {
				fmt.Printf("tailnet: skipped (%v); links stay on this Mac\n", err)
			} else {
				public := pages.PublicURLFor(name, port)
				cur, err := pages.ServeMapping(port)
				switch {
				case err != nil:
					return err
				case cur == pages.LocalTarget(port):
					fmt.Printf("tailnet: %s already maps to %s\n", public, cur)
				case cur != "":
					return fmt.Errorf("the tailnet's HTTPS port %d already maps to %s; pass --port with a free one", port, cur)
				default:
					say("tailnet: mapped "+public+" to "+pages.LocalTarget(port)+" (tailscale serve --bg, tailnet only)",
						fmt.Sprintf("run `tailscale serve --bg --https=%d %s`, tailnet only", port, pages.LocalTarget(port)))
					if !dry {
						if _, err := pages.EnableServe(port); err != nil {
							return err
						}
					}
				}
				if !dry && cfg.PublicURL != public {
					cfg.PublicURL = public
					if err := pages.SaveConfig(home, cfg); err != nil {
						return err
					}
				}
			}
		}

		// 4. the LaunchAgent
		if skip, _ := cmd.Flags().GetBool("no-agent"); !skip {
			exe := thisExe()
			path, _ := pages.AgentPlistPath()
			switch {
			case home != pages.DefaultHome():
				fmt.Printf("agent: skipped, it serves %s only (%s is set)\n", pages.DefaultHome(), pages.HomeEnv)
			case dry:
				fmt.Printf("would: write %s running `%s pages serve --launchd`, and load it\n", path, exe)
			default:
				// The agent takes the port over from a server started on
				// demand.
				_, _ = pages.Stop(home)
				if err := pages.InstallAgent(exe, home); err != nil {
					return err
				}
				fmt.Printf("agent: %s loaded, running `%s pages serve --launchd`\n", pages.AgentLabel, exe)
			}
		}
		if dry {
			return nil
		}
		ps, err := ensurePageServer()
		if err != nil {
			return err
		}
		fmt.Println("[bts] pages are set up; the index of every page:")
		printLinks(pages.LinksFor(ps.Cfg, ps.Token, "/"))
		return nil
	},
}

var pagesTeardownCmd = &cobra.Command{
	Use:          "teardown [--dry-run]",
	Short:        "Undo `bts pages setup`: the guard, the tailnet mapping and the LaunchAgent (pages are kept)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		dry, _ := cmd.Flags().GetBool("dry-run")
		home, err := pages.Home()
		if err != nil {
			return err
		}
		cfg, err := pages.LoadConfig(home)
		if err != nil {
			return err
		}
		sp, err := pages.ClaudeSettingsPath()
		if err != nil {
			return err
		}
		if pages.GuardInstalled(sp) {
			if dry {
				fmt.Printf("would: remove the Artifact guard from %s\n", sp)
			} else {
				if _, err := pages.SetGuard(sp, false); err != nil {
					return err
				}
				fmt.Printf("guard: removed from %s\n", sp)
			}
		}
		if cfg.PublicURL != "" {
			port := cfg.ListenPort()
			if m, err := pages.ServeMapping(port); err == nil && m == pages.LocalTarget(port) {
				if dry {
					fmt.Printf("would: run `tailscale serve --https=%d off`\n", port)
				} else {
					if _, err := pages.DisableServe(port); err != nil {
						return err
					}
					fmt.Printf("tailnet: %s removed\n", cfg.PublicURL)
				}
			}
			if !dry {
				cfg.PublicURL = ""
				if err := pages.SaveConfig(home, cfg); err != nil {
					return err
				}
			}
		}
		if pages.AgentInstalled() || pages.AgentLoaded() {
			if dry {
				fmt.Printf("would: unload and remove the LaunchAgent %s\n", pages.AgentLabel)
			} else {
				if _, err := pages.UninstallAgent(); err != nil {
					return err
				}
				fmt.Printf("agent: %s removed; the server now starts on demand\n", pages.AgentLabel)
			}
		}
		if !dry {
			fmt.Printf("[bts] teardown done; the pages in %s are kept\n", home)
		}
		return nil
	},
}

var pagesTokenCmd = &cobra.Command{
	Use:          "token --rotate",
	Short:        "Replace the page server's token: every link printed before stops working",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if r, _ := cmd.Flags().GetBool("rotate"); !r {
			return errors.New("pass --rotate: the token is only ever shown inside the links bts prints")
		}
		home, err := pages.Home()
		if err != nil {
			return err
		}
		if _, err := pages.RotateToken(home); err != nil {
			return err
		}
		fmt.Println("[bts] token replaced: open a newly printed link (`bts pages open`) on each device")
		return nil
	},
}
