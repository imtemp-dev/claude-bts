package cli

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(evidenceCmd)
	evidenceCmd.AddCommand(evidenceGetCmd, evidencePutCmd, evidenceListCmd, evidencePruneCmd)

	for _, c := range []*cobra.Command{evidenceGetCmd, evidencePutCmd} {
		c.Flags().String("library", "", "Library or platform the claim is about (e.g. swiftui, react)")
		c.Flags().String("topic", "", "Topic within the library (e.g. safeAreaInset propagation)")
		c.Flags().String("claim", "", "The specific claim being checked")
		_ = c.MarkFlagRequired("library")
		_ = c.MarkFlagRequired("claim")
	}
	evidencePutCmd.Flags().String("verdict", "", "contradicts | confirms | silent | unofficial | unavailable")
	evidencePutCmd.Flags().String("gathered", "", "The Gathered: line, e.g. Context7:hit or WebFetch:<url>:200")
	evidencePutCmd.Flags().StringSlice("url", nil, "Source URL (repeatable)")
	evidencePutCmd.Flags().String("summary", "", "One-line summary of what the source said")
	_ = evidencePutCmd.MarkFlagRequired("verdict")
}

var evidenceCmd = &cobra.Command{
	Use:     "evidence",
	Short:   "Cache of framework/platform claim research",
	GroupID: "tools",
	Long: `Memoises the evidence lookups required by bts-evidence-policy.md so a
verification loop does not re-research the same framework claim on every round.

Verifiers should check the cache BEFORE spending a Context7/WebFetch/WebSearch
call, and record the outcome afterwards:

  bts evidence get --library swiftui --topic "safeAreaInset" --claim "..."
  bts evidence put --library swiftui --topic "safeAreaInset" --claim "..." \
      --verdict silent --gathered "Context7:miss | WebFetch:developer.apple.com:200"

Entries live in .bts/local/evidence-cache.json (machine-local, never committed).
Successful lookups expire after verify.evidence_ttl_days; "unavailable" results
expire after one hour so an outage never pins a claim for long.`,
}

var evidenceGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Look up a cached claim (exit 10 on miss)",
	RunE: func(cmd *cobra.Command, args []string) error {
		root, ttl, err := evidenceContext()
		if err != nil {
			return err
		}
		library, _ := cmd.Flags().GetString("library")
		topic, _ := cmd.Flags().GetString("topic")
		claim, _ := cmd.Flags().GetString("claim")

		e, err := state.GetEvidence(root, library, topic, claim, ttl)
		if err != nil {
			return fmt.Errorf("read evidence cache: %w", err)
		}
		if e == nil {
			fmt.Printf("MISS %s\nNo live cache entry — gather evidence per bts-evidence-policy.md, then record it with `bts evidence put`.\n",
				state.EvidenceKey(library, topic, claim))
			os.Exit(10)
		}
		fmt.Printf("HIT %s\nVerdict:  %s\nGathered: %s\n", e.Key, e.Verdict, e.Gathered)
		if len(e.URLs) > 0 {
			fmt.Printf("Source:   %s\n", strings.Join(e.URLs, " | "))
		}
		if e.Summary != "" {
			fmt.Printf("Summary:  %s\n", e.Summary)
		}
		fmt.Printf("Fetched:  %s\n", e.FetchedAt)
		return nil
	},
}

var evidencePutCmd = &cobra.Command{
	Use:   "put",
	Short: "Record the outcome of an evidence lookup",
	RunE: func(cmd *cobra.Command, args []string) error {
		root, _, err := evidenceContext()
		if err != nil {
			return err
		}
		verdict, _ := cmd.Flags().GetString("verdict")
		switch verdict {
		case state.EvidenceContradicts, state.EvidenceConfirms, state.EvidenceSilent,
			state.EvidenceUnofficial, state.EvidenceUnavailable:
		default:
			return fmt.Errorf("invalid --verdict %q (want contradicts|confirms|silent|unofficial|unavailable)", verdict)
		}
		library, _ := cmd.Flags().GetString("library")
		topic, _ := cmd.Flags().GetString("topic")
		claim, _ := cmd.Flags().GetString("claim")
		gathered, _ := cmd.Flags().GetString("gathered")
		urls, _ := cmd.Flags().GetStringSlice("url")
		summary, _ := cmd.Flags().GetString("summary")

		// Citations are load-bearing: bts-evidence-policy.md forbids
		// inventing them, and a cached verdict is replayed verbatim into
		// later rounds. A verdict that claims an official source must
		// carry the URL that backs it.
		if (verdict == state.EvidenceContradicts || verdict == state.EvidenceConfirms) && len(urls) == 0 {
			return fmt.Errorf("--verdict %s requires at least one --url (the official source it is based on)", verdict)
		}

		e := &state.EvidenceEntry{
			Library: library, Topic: topic, Claim: claim,
			Verdict: verdict, Gathered: gathered, URLs: urls, Summary: summary,
		}
		if err := state.PutEvidence(root, e); err != nil {
			return fmt.Errorf("write evidence cache: %w", err)
		}
		fmt.Printf("Cached %s: %s\n", e.Key, verdict)
		return nil
	},
}

var evidenceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List cached evidence lookups",
	RunE: func(cmd *cobra.Command, args []string) error {
		root, _, err := evidenceContext()
		if err != nil {
			return err
		}
		entries, err := state.ListEvidence(root)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			fmt.Println("Evidence cache is empty.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "KEY\tVERDICT\tLIBRARY\tTOPIC\tFETCHED")
		for _, e := range entries {
			topic := e.Topic
			if len(topic) > 40 {
				topic = topic[:37] + "..."
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", e.Key, e.Verdict, e.Library, topic, e.FetchedAt)
		}
		return w.Flush()
	},
}

var evidencePruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Drop expired cache entries",
	RunE: func(cmd *cobra.Command, args []string) error {
		root, ttl, err := evidenceContext()
		if err != nil {
			return err
		}
		n, err := state.PruneEvidence(root, ttl)
		if err != nil {
			return err
		}
		fmt.Printf("Pruned %d expired entries.\n", n)
		return nil
	},
}

// evidenceContext resolves the project root and the configured TTL.
func evidenceContext() (string, int, error) {
	cwd, _ := os.Getwd()
	root, err := state.FindRoot(cwd)
	if err != nil {
		return "", 0, fmt.Errorf("not a bts project: %w", err)
	}
	settings, err := engine.LoadSettings(root)
	if err != nil {
		return "", 0, fmt.Errorf("load settings: %w", err)
	}
	return root, settings.Verify.EvidenceTTLDays, nil
}
