package tunnel

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
)

var closeForce bool

// CloseCmd closes (and revokes) a tunnel.
var CloseCmd = &cobra.Command{
	Use:   "close <tunnel-id>",
	Short: "Close a tunnel",
	Long:  `Close a tunnel and revoke its Hyphae lease.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		tunnelID := args[0]
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		if !closeForce {
			deps.Printer.Print(fmt.Sprintf("Close tunnel %s? [y/N] ", tunnelID))
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
				if answer != "y" && answer != "yes" {
					deps.Printer.PrintInfo("Aborted.")
					return nil
				}
			}
		}

		deps.Printer.Print(lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86")).
			Render(fmt.Sprintf("\nClosing tunnel %s...\n", tunnelID)))

		if err := deps.CPlaneClient.Tunnels.CloseTunnel(ctx, tunnelID); err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to close tunnel: %v", err))
			return err
		}

		deps.Printer.PrintSuccess(fmt.Sprintf("✅ Tunnel %s closed.", tunnelID))
		return nil
	},
}

func init() {
	CloseCmd.Flags().BoolVar(&closeForce, "force", false, "Skip confirmation prompt")
}
