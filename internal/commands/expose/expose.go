package expose

import (
	"github.com/spf13/cobra"
)

var ExposeCmd = &cobra.Command{
	Use:   "expose",
	Short: "Manage public exposures for deployment services",
	Long: `Manage public exposures for deployment services via the Hyphae gateway.

Exposures allow services to be accessed publicly via mTLS reverse tunnels.

Examples:
  ufctl expose create --deployment <id> --service <name> --port <port>
  ufctl expose list --deployment <id>
  ufctl expose get <exposure-id>
  ufctl expose revoke <exposure-id>`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	ExposeCmd.AddCommand(CreateCmd)
	ExposeCmd.AddCommand(ListCmd)
	ExposeCmd.AddCommand(GetCmd)
	ExposeCmd.AddCommand(RevokeCmd)
}
