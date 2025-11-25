package local

import (
	"github.com/spf13/cobra"
)

// LocalCmd is the root command for local operations
var LocalCmd = &cobra.Command{
	Use:   "local",
	Short: "Manage local Underleaf edge servers and configurations",
	Long:  "Commands to manage and interact with local Underleaf edge servers and their configurations.",
}

func init() {
	LocalCmd.AddCommand(AuthCmd)
	LocalCmd.AddCommand(RegisterCmd)
}
