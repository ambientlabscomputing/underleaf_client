package expose

import (
	"encoding/json"
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	listDeploymentID string
	listServerID     string
	listStatus       string
	listLimit        int
	listOffset       int
	listOutput       string
)

var ListCmd = &cobra.Command{
	Use:   "list",
	Short: "List public exposures",
	Long: `List public exposures for deployments and servers.

Can filter by deployment, server, or status.

Examples:
  ufctl expose list
  ufctl expose list --deployment dep-123
  ufctl expose list --server srv-456
  ufctl expose list --status bound
  ufctl expose list --limit 20 --offset 0`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		deps.Printer.Print(lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86")).
			Render("\n🌐 Listing public exposures...\n"))

		// Call API
		resp, err := deps.CPlaneClient.API().QueryExposures(ctx, listDeploymentID, listServerID, listStatus, listLimit, listOffset)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to list exposures: %v", err))
			return err
		}

		// Handle response as map or slice
		var exposures []interface{}
		switch v := resp.(type) {
		case map[string]interface{}:
			// QueryExposuresResponse wraps the list: {"exposures": [...], "total": N}
			if data, ok := v["exposures"].([]interface{}); ok {
				exposures = data
			} else if data, ok := v["data"].([]interface{}); ok {
				exposures = data
			} else {
				exposures = []interface{}{v}
			}
		case []interface{}:
			exposures = v
		default:
			exposures = []interface{}{resp}
		}

		if len(exposures) == 0 {
			deps.Printer.PrintInfo("No exposures found")
			return nil
		}

		// If output format is requested, output as JSON
		if listOutput == "json" {
			jsonBytes, _ := json.MarshalIndent(exposures, "", "  ")
			deps.Printer.Print(string(jsonBytes))
			return nil
		}

		// Display as table
		deps.Printer.Print(fmt.Sprintf("\n%-15s %-12s %-15s %-8s %-25s %-10s %-25s", "ID", "DEPLOYMENT", "SERVICE", "PORT", "HOSTNAME", "STATUS", "URL"))
		deps.Printer.Print("──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────")
		for _, exp := range exposures {
			expMap, ok := exp.(map[string]interface{})
			if !ok {
				continue
			}

			id, _ := expMap["id"].(string)
			deploymentID, _ := expMap["deployment_id"].(string)
			serviceName, _ := expMap["service_name"].(string)
			targetPort, _ := expMap["target_port"].(float64)
			hostname, _ := expMap["hostname"].(string)
			status, _ := expMap["status"].(string)
			publicURL, _ := expMap["public_url"].(string)

			if publicURL == "" {
				publicURL = "-"
			}

			deps.Printer.Print(fmt.Sprintf("%-15s %-12s %-15s %-8v %-25s %-10s %-25s",
				truncate(id, 13),
				truncate(deploymentID, 10),
				serviceName,
				int(targetPort),
				truncate(hostname, 23),
				status,
				truncate(publicURL, 23),
			))
		}

		deps.Printer.Print(fmt.Sprintf("\nTotal: %d exposure(s)", len(exposures)))

		return nil
	},
}

func init() {
	ListCmd.Flags().StringVar(&listDeploymentID, "deployment", "", "Filter by deployment ID")
	ListCmd.Flags().StringVar(&listServerID, "server", "", "Filter by server ID")
	ListCmd.Flags().StringVar(&listStatus, "status", "", "Filter by status (pending, bound, error, revoked)")
	ListCmd.Flags().IntVar(&listLimit, "limit", 50, "Maximum results to return")
	ListCmd.Flags().IntVar(&listOffset, "offset", 0, "Result offset for pagination")
	ListCmd.Flags().StringVarP(&listOutput, "output", "o", "table", "Output format (table or json)")
}

// truncate shortens a string to maxLen characters
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
