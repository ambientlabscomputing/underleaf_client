package expose

import (
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
)

var (
	listDeploymentID string
	listOutput       string
)

// ListCmd lists exposures for a deployment.
var ListCmd = &cobra.Command{
	Use:   "list",
	Short: "List exposures for a deployment",
	Long:  `List Hyphae exposures for a specific deployment.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		if listDeploymentID == "" {
			return fmt.Errorf("--deployment flag is required")
		}

		if listOutput != "json" {
			deps.Printer.Print(lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("86")).
				Render("\n🔗 Listing exposures...\n"))
		}

		resp, err := deps.CPlaneClient.Exposures.GetDeploymentExposures(ctx, listDeploymentID)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to list exposures: %v", err))
			return err
		}

		if len(resp.Exposures) == 0 {
			if listOutput == "json" {
				deps.Printer.Print("[]")
				return nil
			}
			deps.Printer.PrintInfo("No exposures found")
			return nil
		}

		if listOutput == "json" {
			jsonBytes, _ := json.MarshalIndent(resp.Exposures, "", "  ")
			deps.Printer.Print(string(jsonBytes))
			return nil
		}

		deps.Printer.Print(fmt.Sprintf("%-38s %-15s %-8s %-8s %s", "ID", "SERVICE", "PORT", "STATUS", "PUBLIC URL"))
		deps.Printer.Print("────────────────────────────────────────────────────────────────────────────────────────────────────")
		for _, e := range resp.Exposures {
			publicURL := e.PublicURL
			if publicURL == "" {
				publicURL = "-"
			}
			deps.Printer.Print(fmt.Sprintf("%-38s %-15s %-8d %-8s %s",
				e.ID,
				e.ServiceName,
				e.TargetPort,
				e.Status,
				publicURL,
			))
		}
		deps.Printer.Print(fmt.Sprintf("\nTotal: %d", resp.Total))
		return nil
	},
}

func init() {
	ListCmd.Flags().StringVar(&listDeploymentID, "deployment", "", "Deployment ID to list exposures for (required)")
	ListCmd.Flags().StringVarP(&listOutput, "output", "o", "table", "Output format: table|json")
}
