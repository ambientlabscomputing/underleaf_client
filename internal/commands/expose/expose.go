package expose

import (
	"github.com/spf13/cobra"
)

// ExposeCmd is the parent `ufctl expose` command.
var ExposeCmd = &cobra.Command{
	Use:   "expose",
	Short: "Manage deployment exposures",
	Long: `Manage Hyphae deployment exposures.

An exposure binds a deployment's service port to a public HTTPS endpoint
via the Hyphae tunnel gateway.

Examples:
  ufctl expose list --deployment <deployment-id>
  ufctl expose list --deployment <deployment-id> -o json`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	ExposeCmd.AddCommand(ListCmd)
}
