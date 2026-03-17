package tunnel

import (
	"github.com/spf13/cobra"
)

// TunnelCmd is the parent `ufctl tunnel` command.
var TunnelCmd = &cobra.Command{
	Use:   "tunnel",
	Short: "Manage on-demand public tunnels",
	Long: `Manage on-demand public tunnels via the Hyphae gateway.

A tunnel exposes a local port or remote URL at a public HTTPS address,
similar to cloudflared or ngrok but using the Underleaf Hyphae infrastructure.

Local tunnels (default) run in the foreground — press Ctrl+C to close them.
Remote tunnels (--server <id>) run persistently on the target server.

Examples:
  ufctl tunnel create 8080
  ufctl tunnel create http://192.168.1.50:3000 --hostname myapp
  ufctl tunnel create 8080 --server srv-xyz123
  ufctl tunnel list
  ufctl tunnel get <tunnel-id>
  ufctl tunnel close <tunnel-id>`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	TunnelCmd.AddCommand(CreateCmd)
	TunnelCmd.AddCommand(ListCmd)
	TunnelCmd.AddCommand(GetCmd)
	TunnelCmd.AddCommand(CloseCmd)
}
