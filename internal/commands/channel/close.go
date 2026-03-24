package channel

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

// CloseCmd closes (and revokes) a channel.
var CloseCmd = &cobra.Command{
	Use:   "close <channel-id>",
	Short: "Close a relay channel",
	Long:  `Close a relay channel and revoke its Hyphae grant.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		channelID := args[0]
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		if !closeForce {
			deps.Printer.Print(fmt.Sprintf("Close channel %s? [y/N] ", channelID))
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
			Render(fmt.Sprintf("\nClosing channel %s...\n", channelID)))

		if err := deps.CPlaneClient.Channels.CloseChannel(ctx, channelID); err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to close channel: %v", err))
			return err
		}

		deps.Printer.PrintSuccess(fmt.Sprintf("✅ Channel %s closed.", channelID))
		return nil
	},
}

func init() {
	CloseCmd.Flags().BoolVar(&closeForce, "force", false, "Skip confirmation prompt")
}
