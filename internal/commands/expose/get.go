package expose

import (
	"encoding/json"
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var GetCmd = &cobra.Command{
	Use:   "get <exposure-id>",
	Short: "Get details of a specific exposure",
	Long: `Retrieve detailed information about a specific public exposure.

Examples:
  ufctl expose get exp-12345678`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)
		exposureID := args[0]

		deps.Printer.Print(lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86")).
			Render(fmt.Sprintf("\n🌐 Retrieving exposure %s...\n", exposureID)))

		// Call API
		resp, err := deps.CPlaneClient.API().GetExposure(ctx, exposureID)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to get exposure: %v", err))
			return err
		}

		// Parse response
		respMap, ok := resp.(map[string]interface{})
		if !ok {
			deps.Printer.PrintError("Unexpected response format")
			return fmt.Errorf("unexpected response format")
		}

		// Display details
		deps.Printer.PrintSuccess("✅ Exposure Details:")

		deps.Printer.Print(fmt.Sprintf("\n📋 General:"))
		deps.Printer.Print(fmt.Sprintf("   ID:              %v", respMap["id"]))
		deps.Printer.Print(fmt.Sprintf("   Org ID:          %v", respMap["org_id"]))
		deps.Printer.Print(fmt.Sprintf("   Deployment ID:   %v", respMap["deployment_id"]))
		deps.Printer.Print(fmt.Sprintf("   Server ID:       %v", respMap["server_id"]))

		deps.Printer.Print(fmt.Sprintf("\n🔧 Configuration:"))
		deps.Printer.Print(fmt.Sprintf("   Service:         %v", respMap["service_name"]))
		deps.Printer.Print(fmt.Sprintf("   Target Port:     %v", respMap["target_port"]))
		deps.Printer.Print(fmt.Sprintf("   Hostname:        %v", respMap["hostname"]))
		deps.Printer.Print(fmt.Sprintf("   Lease ID:        %v", respMap["lease_id"]))

		deps.Printer.Print(fmt.Sprintf("\n🌍 Status:"))
		status, _ := respMap["status"].(string)
		publicURL, _ := respMap["public_url"].(string)
		errorMsg, _ := respMap["error_message"].(string)

		statusIcon := "❓"
		switch status {
		case "pending":
			statusIcon = "⏳"
		case "bound":
			statusIcon = "✅"
		case "error":
			statusIcon = "❌"
		case "revoked":
			statusIcon = "🚫"
		}

		deps.Printer.Print(fmt.Sprintf("   Status:          %s %s", statusIcon, status))
		if publicURL != "" {
			deps.Printer.Print(fmt.Sprintf("   Public URL:      %s", publicURL))
		}
		if errorMsg != "" {
			deps.Printer.Print(fmt.Sprintf("   Error:           %s", errorMsg))
		}

		deps.Printer.Print(fmt.Sprintf("\n⏰ Timestamps:"))
		deps.Printer.Print(fmt.Sprintf("   Created:         %v", respMap["created_at"]))
		deps.Printer.Print(fmt.Sprintf("   Updated:         %v", respMap["updated_at"]))

		// Show full JSON
		jsonBytes, _ := json.MarshalIndent(resp, "", "  ")
		deps.Printer.Print("\n📄 Full Response:")
		deps.Printer.Print(string(jsonBytes))

		return nil
	},
}
