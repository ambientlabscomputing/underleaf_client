package link

import (
	"github.com/spf13/cobra"
)

// LinkCmd is the parent `ufctl link` command.
//
// A "link" is a unified read-side projection across exposures, tunnels,
// and channels — three resource kinds that all establish reachable paths
// between local and remote services. UNDF-139 introduces a read-only
// aggregate view; create/delete still happens via the per-kind commands.
var LinkCmd = &cobra.Command{
	Use:   "link",
	Short: "Inspect public exposures, tunnels, and peer channels as a unified set",
	Long: `Inspect Links — a read-side aggregate over exposures, tunnels, and channels.

This command surfaces every reachable path your org has provisioned, regardless of
underlying mechanism. Use it to answer "what's exposed right now?" without having
to run three separate commands.

Subcommands:
  ls    List links across all kinds (filterable)

Examples:
  ufctl link ls
  ufctl link ls --kind tunnel
  ufctl link ls --visibility public --status success
	ufctl --format json link ls --server srv-xyz123`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	LinkCmd.AddCommand(ListCmd)
}
