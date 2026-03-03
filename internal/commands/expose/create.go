package expose

import (
	"encoding/json"
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	createDeploymentID string
	createServiceName  string
	createTargetPort   int
	createHostname     string
)

var CreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new public exposure for a service",
	Long: `Create a new public exposure for a deployment service.

The service must be part of an active deployment. The exposure will create
an mTLS reverse tunnel via Hyphae, making the service accessible at a
public hostname (auto-generated or custom).

Examples:
  ufctl expose create --deployment dep-123 --service web --port 8080
  ufctl expose create --deployment dep-123 --service api --port 3000 --hostname my-api`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		// Validate inputs
		if createDeploymentID == "" {
			deps.Printer.PrintError("--deployment is required")
			return fmt.Errorf("missing required flag: --deployment")
		}
		if createServiceName == "" {
			deps.Printer.PrintError("--service is required")
			return fmt.Errorf("missing required flag: --service")
		}
		if createTargetPort < 1 || createTargetPort > 65535 {
			deps.Printer.PrintError(fmt.Sprintf("--port must be between 1 and 65535, got %d", createTargetPort))
			return fmt.Errorf("invalid port: %d", createTargetPort)
		}

		deps.Printer.Print(lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86")).
			Render("\n🌐 Creating public exposure...\n"))

		// Call API
		resp, err := deps.CPlaneClient.API().CreateExposure(ctx, createDeploymentID, createServiceName, createTargetPort, createHostname)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to create exposure: %v", err))
			return err
		}

		// Parse response to get exposure details
		respMap, ok := resp.(map[string]interface{})
		if !ok {
			deps.Printer.PrintError("Unexpected response format")
			return fmt.Errorf("unexpected response format")
		}

		exposureID, _ := respMap["id"].(string)
		publicURL, _ := respMap["public_url"].(string)
		status, _ := respMap["status"].(string)

		// Display result
		deps.Printer.PrintSuccess("✅ Exposure created successfully!")
		deps.Printer.Print("\n📋 Exposure Details:")
		deps.Printer.Print(fmt.Sprintf("   ID:           %s", exposureID))
		deps.Printer.Print(fmt.Sprintf("   Service:      %s", createServiceName))
		deps.Printer.Print(fmt.Sprintf("   Port:         %d", createTargetPort))
		deps.Printer.Print(fmt.Sprintf("   Status:       %s", status))
		if publicURL != "" {
			deps.Printer.Print(fmt.Sprintf("   Public URL:   %s", publicURL))
		} else {
			deps.Printer.Print(fmt.Sprintf("   Public URL:   <pending tunnel bind>"))
		}

		// Show full response as JSON if verbose
		jsonBytes, _ := json.MarshalIndent(resp, "", "  ")
		deps.Printer.Print("\n📄 Full Response:")
		deps.Printer.Print(string(jsonBytes))

		return nil
	},
}

func init() {
	CreateCmd.Flags().StringVar(&createDeploymentID, "deployment", "", "Deployment ID (required)")
	CreateCmd.Flags().StringVar(&createServiceName, "service", "", "Service name (required)")
	CreateCmd.Flags().IntVar(&createTargetPort, "port", 0, "Local port (required, 1-65535)")
	CreateCmd.Flags().StringVar(&createHostname, "hostname", "", "Custom hostname (optional, auto-generated if empty)")

	CreateCmd.MarkFlagRequired("deployment")
	CreateCmd.MarkFlagRequired("service")
	CreateCmd.MarkFlagRequired("port")
}
