package servers

import (
	"fmt"
	"strings"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var MetricsCmd = &cobra.Command{
	Use:   "metrics [server-id]",
	Short: "Show server metrics",
	Long: `Display current metrics for a specific server.

Examples:
  ufctl servers metrics server-abc123`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		serverID := args[0]

		metrics, err := deps.ServerSvc.GetServerMetrics(ctx, serverID)
		if err != nil {
			deps.Printer.PrintError("Failed to get server metrics: " + err.Error())
			return err
		}

		// Display metrics
		boldStyle := lipgloss.NewStyle().Bold(true)
		headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
		labelStyle := lipgloss.NewStyle().Width(20)

		deps.Printer.Print("")
		deps.Printer.Print(boldStyle.Render(fmt.Sprintf("Metrics for Server: %s", serverID)))
		deps.Printer.Print(strings.Repeat("─", 50))
		deps.Printer.Print("")

		deps.Printer.Print(headerStyle.Render("Current Metrics"))
		deps.Printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("CPU Usage:"), formatMetricValue(metrics.CPUUsage)))
		deps.Printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("Memory Usage:"), formatMetricValue(metrics.MemoryUsage)))
		deps.Printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("Disk Usage:"), formatMetricValue(metrics.DiskUsage)))

		if metrics.UpdatedAt != nil {
			deps.Printer.Print("")
			deps.Printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("Last Updated:"), metrics.UpdatedAt.Format("2006-01-02 15:04:05 MST")))
		}

		deps.Printer.Print("")
		return nil
	},
}

var MetricsHistoryCmd = &cobra.Command{
	Use:   "metrics:history [server-id]",
	Short: "Show server metrics history",
	Long: `Display historical metrics for a specific server.

Examples:
  ufctl servers metrics:history server-abc123
  ufctl servers metrics:history server-abc123 --period 7d --resolution 1h`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		serverID := args[0]
		period, _ := cmd.Flags().GetString("period")
		resolution, _ := cmd.Flags().GetString("resolution")

		history, err := deps.ServerSvc.GetMetricsHistory(ctx, serverID, period, resolution)
		if err != nil {
			deps.Printer.PrintError("Failed to get metrics history: " + err.Error())
			return err
		}

		if len(history.DataPoints) == 0 {
			deps.Printer.PrintInfo("No metrics history available.")
			return nil
		}

		// Build table
		table := ui.NewTableBuilder().
			WithTitle(fmt.Sprintf("Metrics History - %s (Period: %s, Resolution: %s)", serverID, history.Period, history.Resolution)).
			WithHeaders("Timestamp", "CPU %", "Memory %", "Disk %")

		for _, dp := range history.DataPoints {
			table.AddRow(
				dp.Timestamp.Format("2006-01-02 15:04"),
				fmt.Sprintf("%.1f", dp.CPUUsage),
				fmt.Sprintf("%.1f", dp.MemoryUsage),
				fmt.Sprintf("%.1f", dp.DiskUsage),
			)
		}

		deps.Printer.PrintTable(table)
		return nil
	},
}

func formatMetricValue(value float64) string {
	var color string
	if value < 50 {
		color = "2" // green
	} else if value < 80 {
		color = "3" // yellow
	} else {
		color = "1" // red
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(fmt.Sprintf("%.1f%%", value))
}

func init() {
	MetricsHistoryCmd.Flags().StringP("period", "p", "24h", "Time period (1h, 24h, 7d, 30d)")
	MetricsHistoryCmd.Flags().StringP("resolution", "r", "5m", "Data resolution (1m, 5m, 1h)")

	ServersCmd.AddCommand(MetricsCmd)
	ServersCmd.AddCommand(MetricsHistoryCmd)
}
