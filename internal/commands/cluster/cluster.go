package cluster

import (
	"github.com/spf13/cobra"
)

// ClusterCmd is the root command for cluster operations
var ClusterCmd = &cobra.Command{
	Use:   "cluster",
	Short: "Manage Raft cluster operations",
	Long: `Manage Raft cluster operations including viewing status, joining/leaving clusters,
promoting/demoting nodes, and enabling maintenance mode.

The cluster commands interact with the local agent's Raft subsystem to manage
quorum-based KV storage across multiple nodes.`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	// Add subcommands
	ClusterCmd.AddCommand(statusCmd)
	ClusterCmd.AddCommand(joinCmd)
	ClusterCmd.AddCommand(leaveCmd)
	ClusterCmd.AddCommand(promoteCmd)
	ClusterCmd.AddCommand(demoteCmd)
	ClusterCmd.AddCommand(maintenanceCmd)
	ClusterCmd.AddCommand(kvCmd)
}
