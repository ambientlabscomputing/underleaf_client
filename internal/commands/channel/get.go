package channel

import (
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
)

var getOutput string

// GetCmd retrieves details for a single channel.
var GetCmd = &cobra.Command{
	Use:   "get <channel-id>",
	Short: "Get details for a channel",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		channelID := args[0]
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		if getOutput != "json" {
			deps.Printer.Print(lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("86")).
				Render(fmt.Sprintf("\n🔗 Fetching channel %s...\n", channelID)))
		}

		ch, err := deps.CPlaneClient.Channels.GetChannel(ctx, channelID)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to get channel: %v", err))
			return err
		}

		if getOutput == "json" {
			jsonBytes, _ := json.MarshalIndent(ch, "", "  ")
			deps.Printer.Print(string(jsonBytes))
			return nil
		}

		purpose := ch.Purpose
		if purpose == "" {
			purpose = "-"
		}

		deps.Printer.Print(fmt.Sprintf("   ID:      %s", ch.ID))
		deps.Printer.Print(fmt.Sprintf("   Source:  %s", ch.SourceServerID))
		deps.Printer.Print(fmt.Sprintf("   Dest:    %s", ch.DestServerID))
		deps.Printer.Print(fmt.Sprintf("   Purpose: %s", purpose))
		deps.Printer.Print(fmt.Sprintf("   Status:  %s", ch.Status))
		deps.Printer.Print(fmt.Sprintf("   Expires: %s", ch.ExpiresAt))
		if ch.ErrorMessage != "" {
			deps.Printer.Print(fmt.Sprintf("   Error:   %s", ch.ErrorMessage))
		}
		deps.Printer.Print(fmt.Sprintf("   Created: %s", ch.CreatedAt))
		return nil
	},
}

func init() {
	GetCmd.Flags().StringVarP(&getOutput, "output", "o", "table", "Output format: table|json")
}
