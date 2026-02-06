package mmesh

import (
	"github.com/spf13/cobra"
)

var MMeshCmd = &cobra.Command{
	Use:   "mmesh",
	Short: "Manage Mycelium Mesh Agent (MMA) integration",
	Long: `Control and monitor the Mycelium Mesh Agent (MMA) event stream integration.

The UA→MMA event stream allows the Underleaf Agent to publish cluster membership,
service lifecycle, and policy events to the Mycelium Mesh Agent for mesh networking.`,
}

func init() {
	// Add subcommands
	MMeshCmd.AddCommand(statusCmd)
	MMeshCmd.AddCommand(subscribersCmd)
	MMeshCmd.AddCommand(bufferCmd)
	MMeshCmd.AddCommand(publishCmd)
}
