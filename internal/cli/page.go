package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/pages"
	"github.com/spf13/cobra"
)

// `bts page` makes the pages a Claude session would otherwise publish as
// claude.ai artifacts: a page belongs to no claude.ai account, any session
// on the machine updates it, and the owner opens it on this Mac or, over
// the tailnet, on the phone (docs/research/pages-everywhere-design.md).

func init() {
	rootCmd.AddCommand(pageCmd)
	pageCmd.AddCommand(pagePublishCmd, pageListCmd, pageOpenCmd, pageReadCmd, pageExportCmd, pageDeleteCmd)
	pagePublishCmd.Flags().String("title", "", "The page's title (default: its <title>, then the file's name)")
	pagePublishCmd.Flags().String("description", "", "One sentence for the index")
	pagePublishCmd.Flags().StringArray("file", nil, "A supporting file as published=source (img/chart.png=/tmp/chart.png); published= alone removes it; repeat for each")
	pagePublishCmd.Flags().String("page", "", "Update this page (its id) instead of matching by file")
	pagePublishCmd.Flags().Bool("new", false, "Make a new page even though this file was published before")
	pagePublishCmd.Flags().Bool("no-browser", false, "Print the links only; open no browser tab")
	pageListCmd.Flags().Bool("json", false, "Print JSON")
	pageOpenCmd.Flags().Bool("no-browser", false, "Print the links only")
	pageExportCmd.Flags().StringP("output", "o", "", "Where to write the file (default: ~/Downloads/<id>-v<n>.html)")
	pageExportCmd.Flags().Bool("embed-remote", false, "Download remote scripts, stylesheets and images and inline them too")
	pageExportCmd.Flags().Int("version", 0, "Export this version (default: the current one)")
	pageDeleteCmd.Flags().Bool("yes", false, "Confirm: the page and every version of it are removed")
}

var pageCmd = &cobra.Command{
	Use:   "page",
	Short: "Publish, update, list and export pages bts serves (in place of claude.ai artifacts)",
}

func parseFileFlags(list []string) (map[string]string, error) {
	out := map[string]string{}
	for _, f := range list {
		pub, src, ok := strings.Cut(f, "=")
		if !ok || strings.TrimSpace(pub) == "" {
			return nil, fmt.Errorf("--file %q: give published=source", f)
		}
		if src != "" {
			abs, err := filepath.Abs(src)
			if err != nil {
				return nil, err
			}
			src = abs
		}
		out[strings.TrimSpace(pub)] = src
	}
	return out, nil
}

var pagePublishCmd = &cobra.Command{
	Use:   "publish <page.html> [--title t] [--file published=source]... [--page <id> | --new]",
	Short: "Publish a page, or a new version of one, and print its links",
	Long: `Publishes an HTML page into the user's page store and serves it from this
machine.  The rules are the Artifact tool's: --page <id> updates that page;
a file an earlier publish used updates that page; anything else makes a new
page.  Every version is kept.  Supporting files the page loads are passed as
--file published=source; files of the previous version that are not named
are kept.  It prints the page id and its links (this Mac, and the tailnet
when ` + "`bts pages setup`" + ` mapped it) and opens the page.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		list, _ := cmd.Flags().GetStringArray("file")
		files, err := parseFileFlags(list)
		if err != nil {
			return err
		}
		title, _ := cmd.Flags().GetString("title")
		desc, _ := cmd.Flags().GetString("description")
		id, _ := cmd.Flags().GetString("page")
		fresh, _ := cmd.Flags().GetBool("new")
		if id != "" && fresh {
			return errors.New("--page and --new contradict each other")
		}
		home, err := pages.Home()
		if err != nil {
			return err
		}
		cwd, _ := os.Getwd()
		p, created, err := pages.Publish(home, pages.PublishOptions{
			File: args[0], Files: files, Title: title, Description: desc, Page: id, New: fresh, From: cwd,
		})
		if err != nil {
			return err
		}
		ps, err := ensurePageServer()
		if err != nil {
			return err
		}
		links := pages.LinksFor(ps.Cfg, ps.Token, pages.PagePath(p.ID))
		verb := "updated"
		if created {
			verb = "published"
		}
		fmt.Printf("[bts] page %s %s, v%d: %s\n", p.ID, verb, p.Version, p.Title)
		printLinks(links)
		if noBrowser, _ := cmd.Flags().GetBool("no-browser"); !noBrowser && created {
			openInBrowser(cwdRoot(), links.Local)
		} else if !created {
			fmt.Println("  an open tab shows the new version on reload")
		}
		fmt.Printf("  update it: publish the same file again, or `bts page publish <file> --page %s`; share outside the tailnet: `bts page export %s`\n", p.ID, p.ID)
		return nil
	},
}

var pageListCmd = &cobra.Command{
	Use:          "list [--json]",
	Short:        "List the pages, the most recently updated first",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := pages.Home()
		if err != nil {
			return err
		}
		list, err := pages.List(home)
		if err != nil {
			return err
		}
		if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
			if list == nil {
				list = []*pages.Page{}
			}
			out, _ := json.MarshalIndent(list, "", "  ")
			fmt.Println(string(out))
			return nil
		}
		if len(list) == 0 {
			fmt.Println("[bts] no pages yet: `bts page publish <page.html>` makes one")
			return nil
		}
		for _, p := range list {
			fmt.Printf("%-48s v%-3d %s  %s\n", p.ID, p.Version, pages.ShortTime(p.Updated), p.Title)
		}
		return nil
	},
}

var pageOpenCmd = &cobra.Command{
	Use:          "open <id>",
	Short:        "Print a page's links and open it",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := pages.Home()
		if err != nil {
			return err
		}
		p, err := pages.Get(home, args[0])
		if err != nil {
			return err
		}
		ps, err := ensurePageServer()
		if err != nil {
			return err
		}
		links := pages.LinksFor(ps.Cfg, ps.Token, pages.PagePath(p.ID))
		fmt.Printf("[bts] page %s, v%d: %s\n", p.ID, p.Version, p.Title)
		printLinks(links)
		if noBrowser, _ := cmd.Flags().GetBool("no-browser"); !noBrowser {
			openInBrowser(cwdRoot(), links.Local)
		}
		return nil
	},
}

var pageReadCmd = &cobra.Command{
	Use:   "read <id>",
	Short: "Say where a page's current files are, to edit a copy and publish it again",
	Long: `Prints the page's record and where its current version's files are.  Edit
a copy (in the session's scratchpad), never the stored files, and publish
the copy with --page <id>: the store keeps every version as published.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := pages.Home()
		if err != nil {
			return err
		}
		p, err := pages.Get(home, args[0])
		if err != nil {
			return err
		}
		dir := pages.CurrentDir(home, p)
		files, err := pages.VersionFiles(dir)
		if err != nil {
			return err
		}
		fmt.Printf("[bts] page %s, v%d: %s\n", p.ID, p.Version, p.Title)
		if p.Description != "" {
			fmt.Printf("  description: %s\n", p.Description)
		}
		fmt.Printf("  updated %s, last published from %s\n", pages.ShortTime(p.Updated), p.Source)
		fmt.Printf("  files of v%d in %s:\n", p.Version, dir)
		for _, f := range files {
			fmt.Printf("    %s\n", f)
		}
		fmt.Printf("  to change it: copy the files out, edit the copy, then `bts page publish <copy>/%s --page %s` (with --file for any supporting file you changed)\n", pages.PageFile, p.ID)
		return nil
	},
}

var pageExportCmd = &cobra.Command{
	Use:   "export <id> [-o file] [--embed-remote]",
	Short: "Write a page as one HTML file that opens with no server, to share",
	Long: `Writes the page as one HTML file for people outside the owner's tailnet.
Local files the page loads are inlined; remote ones stay links unless
--embed-remote downloads them too; an element marked data-external="1" is
left alone.  What could not be embedded is listed.  bts only writes the
file; sending it is a separate step.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := pages.Home()
		if err != nil {
			return err
		}
		p, err := pages.Get(home, args[0])
		if err != nil {
			return err
		}
		n, _ := cmd.Flags().GetInt("version")
		if n == 0 {
			n = p.Version
		}
		if n < 1 || n > p.Version {
			return fmt.Errorf("page %s has versions 1 to %d", p.ID, p.Version)
		}
		dir := pages.VersionDir(home, p.ID, n)
		doc, err := os.ReadFile(filepath.Join(dir, pages.PageFile))
		if err != nil {
			return err
		}
		embed, _ := cmd.Flags().GetBool("embed-remote")
		out, rep, err := pages.Export(doc, pages.ExportOptions{
			Resolve:     pages.DirResolver(dir),
			EmbedRemote: embed,
			LocalHosts:  exportLocalHosts(),
			Footer:      fmt.Sprintf("%s · v%d · exported from bts on %s", p.Title, n, time.Now().Format("2006-01-02")),
		})
		if err != nil {
			return err
		}
		return writeExport(cmd, fmt.Sprintf("%s-v%d.html", p.ID, n), out, rep)
	},
}

var pageDeleteCmd = &cobra.Command{
	Use:          "delete <id> --yes",
	Short:        "Remove a page and every version of it (only when the owner asks)",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if yes, _ := cmd.Flags().GetBool("yes"); !yes {
			return errors.New("deleting removes every version and breaks every link to it: pass --yes once the owner asked for it")
		}
		home, err := pages.Home()
		if err != nil {
			return err
		}
		if err := pages.Delete(home, args[0]); err != nil {
			return err
		}
		fmt.Printf("[bts] page %s deleted\n", args[0])
		return nil
	},
}
