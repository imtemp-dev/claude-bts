package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/imtemp-dev/claude-bts/internal/hook"
	"github.com/spf13/cobra"
)

func init() {
	recipeCmd.AddCommand(recipeFinishCmd)
}

// errFinishBlocked makes `bts recipe finish` exit non-zero after it printed
// the gate's reason.
var errFinishBlocked = errors.New("not finished: the completion gate refused (reason above)")

// bts recipe finish <id> (trial problem #155): the completion gate, run
// now rather than when the session stops.  A Multica run ends at its stop,
// so a worker that completed its recipe there had no turn left for
// `bts feature pr`; it piped a Stop payload to `bts hook stop` instead.
var recipeFinishCmd = &cobra.Command{
	Use:   "finish <recipe-id>",
	Short: "Complete the recipe through the same gate the Stop hook runs on its completion marker, without ending the session",
	Long: `Runs the gate a completion marker meets at the Stop hook (<bts>DONE</bts> in the spec loop,
<bts>IMPLEMENT DONE</bts> in an implement phase, <bts>FIX DONE</bts> for a fix recipe), now:
the same checks, the same phase change recorded as via the gate, and the same rule that a
session which does not own the recipe cannot complete it (the session is read from
CLAUDE_CODE_SESSION_ID).  It exits 0 when the recipe moved on, and 1 with the gate's reason
when it did not, changing nothing.  For a session that must go on after completion, such as
a Multica run; an interactive session keeps ending its turn with the marker.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		res, err := hook.Finish(root, args[0], os.Getenv("CLAUDE_CODE_SESSION_ID"))
		if err != nil {
			return err
		}
		if res.Blocked != "" {
			fmt.Printf("[bts] %s refused for %s (phase %s):\n%s\n", res.Marker, args[0], res.From, res.Blocked)
			return errFinishBlocked
		}
		if res.To == res.From {
			return fmt.Errorf("%s: the gate let the stop through, but %s is still in phase %s; nothing was completed", res.Marker, args[0], res.From)
		}
		fmt.Printf("[bts] %s passed for %s: phase %s → %s\n", res.Marker, args[0], res.From, res.To)
		return nil
	},
}
