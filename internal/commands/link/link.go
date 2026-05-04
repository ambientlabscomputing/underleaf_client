package link

import "github.com/spf13/cobra"

var LinkCmd = &cobra.Command{
	Use:   "link",
	Short: "Manage unified Rhizo links",
	Long:  "Manage unified Rhizo links across exposures, tunnels, and channels.",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	LinkCmd.AddCommand(CreateCmd)
	LinkCmd.AddCommand(ListCmd)
	LinkCmd.AddCommand(GetCmd)
	LinkCmd.AddCommand(CloseCmd)
}
