package servers

import "github.com/spf13/cobra"

var ServersCmd = &cobra.Command{
	Use:   "servers",
	Short: "Manage Underleaf servers",
	Long:  "Commands to manage and interact with Underleaf servers.",
}
