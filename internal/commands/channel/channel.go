package channel

import (
	"github.com/spf13/cobra"
)

// ChannelCmd is the parent `ufctl channel` command.
var ChannelCmd = &cobra.Command{
	Use:   "channel",
	Short: "Manage server-to-server relay channels",
	Long: `Manage server-to-server relay channels via the Hyphae gateway.

A channel creates a private, authenticated TCP relay between two servers in
your organization. The source server initiates traffic; the destination server
listens. Both sides authenticate using an ES256 grant JWT signed at creation time.

The grant is returned only once (at creation) and cannot be retrieved later.

Examples:
  ufctl channel create --source srv-abc123 --dest srv-xyz789 --purpose "db-replication"
  ufctl channel list
  ufctl channel list --source srv-abc123 --status active
  ufctl channel get <channel-id>
  ufctl channel close <channel-id>`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	ChannelCmd.AddCommand(CreateCmd)
	ChannelCmd.AddCommand(ListCmd)
	ChannelCmd.AddCommand(GetCmd)
	ChannelCmd.AddCommand(CloseCmd)
}
