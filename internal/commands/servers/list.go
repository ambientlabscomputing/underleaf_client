package servers

import (
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/types"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var ListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registered edge servers",
	Long: `List all registered edge servers with optional filtering.

Examples:
  ufctl servers list
  ufctl servers list --status online
  ufctl servers list --location us-east-1
  ufctl servers list --search web --limit 20`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		// Get filter flags
		status, _ := cmd.Flags().GetString("status")
		location, _ := cmd.Flags().GetString("location")
		search, _ := cmd.Flags().GetString("search")
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")

		params := types.ListServersParams{
			Status:   status,
			Location: location,
			Search:   search,
			Limit:    limit,
			Offset:   offset,
		}

		servers, err := deps.ServerSvc.ListServersWithParams(ctx, params)
		if err != nil {
			deps.Printer.PrintError("Failed to list edge servers: " + err.Error())
			return err
		}

		if len(servers) == 0 {
			deps.Printer.PrintInfo("No edge servers registered.")
			return nil
		}

		// Get local server ID for highlighting
		localServerID := ""
		if val, ok := deps.ConfigClient.Get("local.server_id"); ok {
			localServerID = val.(string)
		}

		// Build table with new columns
		table := ui.NewTableBuilder().
			WithTitle("Edge Servers").
			WithHeaders("ID", "Name", "Status", "Location", "Platform", "CPU", "Memory")

		for _, server := range servers {
			platform := ""
			if server.Platform != nil {
				platform = server.Platform.OS + "/" + server.Platform.Arch
			} else if server.Config.Payload.Platform.OS != "" {
				platform = server.Config.Payload.Platform.OS + "/" + server.Config.Payload.Platform.Arch
			}

			statusDisplay := formatStatus(server.Status)
			location := server.Location
			if location == "" {
				location = "-"
			}

			cpuUsage := "-"
			memUsage := "-"
			if server.Metrics != nil {
				cpuUsage = fmt.Sprintf("%.1f%%", server.Metrics.CPUUsage)
				memUsage = fmt.Sprintf("%.1f%%", server.Metrics.MemoryUsage)
			}

			// Highlight this machine's server
			serverID := server.ID
			if serverID == localServerID {
				serverID = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true).Render(serverID + " (this)")
			}

			table.AddRow(serverID, server.Name, statusDisplay, location, platform, cpuUsage, memUsage)
		}

		deps.Printer.Print(table.Render())
		return nil
	},
}

func formatStatus(status string) string {
	switch status {
	case "online":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("● online")
	case "offline":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("○ offline")
	case "degraded":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render("◐ degraded")
	default:
		if status == "" {
			return "-"
		}
		return status
	}
}

func init() {
	ListCmd.Flags().StringP("status", "s", "", "Filter by status (online, offline, degraded)")
	ListCmd.Flags().StringP("location", "l", "", "Filter by location")
	ListCmd.Flags().String("search", "", "Search by name or hostname")
	ListCmd.Flags().Int("limit", 50, "Maximum number of results")
	ListCmd.Flags().Int("offset", 0, "Pagination offset")

	ServersCmd.AddCommand(ListCmd)
}
