package servers

import (
	"fmt"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	servertypes "github.com/ambientlabscomputing/underleaf_client/internal/types/server"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var ActivityCmd = &cobra.Command{
	Use:   "activity [server-id]",
	Short: "Show server activity feed",
	Long: `Display recent activity for a specific server.

Examples:
  ufctl servers activity server-abc123
  ufctl servers activity server-abc123 --type alert
  ufctl servers activity server-abc123 --type deployment --limit 20`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		serverID := args[0]

		// Get filter flags
		activityType, _ := cmd.Flags().GetString("type")
		fromStr, _ := cmd.Flags().GetString("from")
		toStr, _ := cmd.Flags().GetString("to")
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")

		params := servertypes.GetActivityParams{
			Type:   activityType,
			Limit:  limit,
			Offset: offset,
		}

		// Parse time filters if provided
		if fromStr != "" {
			t, err := time.Parse(time.RFC3339, fromStr)
			if err != nil {
				deps.Printer.PrintError("Invalid 'from' date format. Use RFC3339 format (e.g., 2025-11-01T00:00:00Z)")
				return err
			}
			params.From = &t
		}
		if toStr != "" {
			t, err := time.Parse(time.RFC3339, toStr)
			if err != nil {
				deps.Printer.PrintError("Invalid 'to' date format. Use RFC3339 format (e.g., 2025-11-30T23:59:59Z)")
				return err
			}
			params.To = &t
		}

		activity, err := deps.ServerSvc.GetServerActivity(ctx, serverID, params)
		if err != nil {
			deps.Printer.PrintError("Failed to get server activity: " + err.Error())
			return err
		}

		if len(activity.Results) == 0 {
			deps.Printer.PrintInfo("No activity found.")
			return nil
		}

		// Build table
		table := ui.NewTableBuilder().
			WithTitle(fmt.Sprintf("Server Activity - %s (showing %d of %d)", serverID, activity.Count, activity.TotalCount)).
			WithHeaders("Timestamp", "Type", "Description", "Status")

		for _, act := range activity.Results {
			statusDisplay := formatActivityStatus(act.Status)
			typeDisplay := formatActivityType(act.Type)

			// Truncate description if too long
			desc := act.Description
			if len(desc) > 50 {
				desc = desc[:47] + "..."
			}

			table.AddRow(
				act.Timestamp.Format("2006-01-02 15:04"),
				typeDisplay,
				desc,
				statusDisplay,
			)
		}

		deps.Printer.Print(table.Render())
		return nil
	},
}

func formatActivityStatus(status string) string {
	switch status {
	case "success":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("✓ success")
	case "failed":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("✗ failed")
	case "pending":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render("◌ pending")
	default:
		return status
	}
}

func formatActivityType(actType string) string {
	switch actType {
	case "deployment":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Render("deployment")
	case "health_check":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render("health_check")
	case "system_event":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Render("system_event")
	case "alert":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true).Render("alert")
	case "command_execution":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render("command")
	default:
		return actType
	}
}

func init() {
	ActivityCmd.Flags().StringP("type", "t", "", "Filter by activity type (deployment, health_check, system_event, alert, command_execution)")
	ActivityCmd.Flags().String("from", "", "Start date (RFC3339 format, e.g., 2025-11-01T00:00:00Z)")
	ActivityCmd.Flags().String("to", "", "End date (RFC3339 format, e.g., 2025-11-30T23:59:59Z)")
	ActivityCmd.Flags().Int("limit", 50, "Maximum number of results")
	ActivityCmd.Flags().Int("offset", 0, "Pagination offset")

	ServersCmd.AddCommand(ActivityCmd)
}
