package tunnel

import (
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
)

var getOutput string

// GetCmd retrieves details for a single tunnel.
var GetCmd = &cobra.Command{
	Use:   "get <tunnel-id>",
	Short: "Get details for a tunnel",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		tunnelID := args[0]
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		if getOutput != "json" {
			deps.Printer.Print(lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("86")).
				Render(fmt.Sprintf("\n🔗 Fetching tunnel %s...\n", tunnelID)))
		}

		t, err := deps.CPlaneClient.Tunnels.GetTunnel(ctx, tunnelID)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to get tunnel: %v", err))
			return err
		}

		if getOutput == "json" {
			jsonBytes, _ := json.MarshalIndent(t, "", "  ")
			deps.Printer.Print(string(jsonBytes))
			return nil
		}

		publicURL := t.PublicURL
		if publicURL == "" {
			publicURL = "-"
		}

		deps.Printer.Print(fmt.Sprintf("   ID:         %s", t.ID))
		deps.Printer.Print(fmt.Sprintf("   Type:       %s", t.TargetType))
		deps.Printer.Print(fmt.Sprintf("   Target:     %s", t.Target))
		deps.Printer.Print(fmt.Sprintf("   Hostname:   %s", t.Hostname))
		deps.Printer.Print(fmt.Sprintf("   Status:     %s", t.Status))
		deps.Printer.Print(fmt.Sprintf("   Public URL: %s", publicURL))
		if t.ServerID != "" {
			deps.Printer.Print(fmt.Sprintf("   Server:     %s", t.ServerID))
		}
		if t.ErrorMessage != "" {
			deps.Printer.Print(fmt.Sprintf("   Error:      %s", t.ErrorMessage))
		}
		deps.Printer.Print(fmt.Sprintf("   Created:    %s", t.CreatedAt))
		return nil
	},
}

func init() {
	GetCmd.Flags().StringVarP(&getOutput, "output", "o", "table", "Output format: table|json")
}
