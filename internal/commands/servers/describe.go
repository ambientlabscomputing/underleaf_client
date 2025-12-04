package servers

import (
	"fmt"
	"strings"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/server"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var DescribeCmd = &cobra.Command{
	Use:   "describe [server-id]",
	Short: "Show detailed information about a server",
	Long: `Display detailed information about a specific server.

Examples:
  ufctl servers describe server-abc123
  ufctl servers describe my-server-name`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		serverID := args[0]

		// Get server details from control plane
		srv, err := deps.ServerSvc.GetServer(ctx, serverID)
		if err != nil {
			deps.Printer.PrintError("Failed to get server: " + err.Error())
			return err
		}

		// Display server details
		showServerDetails(deps.Printer, srv)

		return nil
	},
}

func showServerDetails(printer ui.Printer, srv server.Server) {
	boldStyle := lipgloss.NewStyle().Bold(true)
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	labelStyle := lipgloss.NewStyle().Width(20)

	printer.Print("")
	printer.Print(boldStyle.Render(fmt.Sprintf("Server: %s", srv.Name)))
	printer.Print(strings.Repeat("─", 50))
	printer.Print("")

	// Basic Info
	printer.Print(headerStyle.Render("Basic Information"))
	printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("ID:"), srv.ID))
	printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("Name:"), srv.Name))

	// Status
	if srv.Status != "" {
		statusDisplay := formatStatus(srv.Status)
		printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("Status:"), statusDisplay))
	}

	// Location, IP, Hostname
	if srv.Location != "" {
		printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("Location:"), srv.Location))
	}
	if srv.IPAddress != "" {
		printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("IP Address:"), srv.IPAddress))
	}
	if srv.Hostname != "" {
		printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("Hostname:"), srv.Hostname))
	}

	// Platform
	if srv.Platform != nil {
		printer.Print(fmt.Sprintf("%s%s/%s", labelStyle.Render("Platform:"), srv.Platform.OS, srv.Platform.Arch))
	} else {
		platform := srv.Config.Payload.Platform
		if platform.OS != "" {
			printer.Print(fmt.Sprintf("%s%s/%s", labelStyle.Render("Platform:"), platform.OS, platform.Arch))
		}
	}

	// Metrics
	if srv.Metrics != nil {
		printer.Print("")
		printer.Print(headerStyle.Render("Current Metrics"))
		printer.Print(fmt.Sprintf("%s%.1f%%", labelStyle.Render("CPU Usage:"), srv.Metrics.CPUUsage))
		printer.Print(fmt.Sprintf("%s%.1f%%", labelStyle.Render("Memory Usage:"), srv.Metrics.MemoryUsage))
		printer.Print(fmt.Sprintf("%s%.1f%%", labelStyle.Render("Disk Usage:"), srv.Metrics.DiskUsage))
		if srv.Metrics.UpdatedAt != nil {
			printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("Metrics Updated:"), srv.Metrics.UpdatedAt.Format("2006-01-02 15:04:05 MST")))
		}
	}

	// Tags
	if len(srv.Tags) > 0 {
		printer.Print("")
		printer.Print(headerStyle.Render("Tags"))
		for k, v := range srv.Tags {
			printer.Print(fmt.Sprintf("%s%s", labelStyle.Render(k+":"), v))
		}
	}

	// Command Settings
	printer.Print("")
	printer.Print(headerStyle.Render("Command Settings"))
	commands := srv.Config.Payload.Commands
	printer.Print(fmt.Sprintf("%s%v", labelStyle.Render("Allow Literal:"), commands.AllowLiteralCommands))

	// Config version
	printer.Print("")
	printer.Print(fmt.Sprintf("%s%d", labelStyle.Render("Config Version:"), srv.Config.Version))

	// Timestamps
	if srv.LastCheckIn != nil && srv.LastCheckIn.Valid {
		printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("Last Check-in:"), srv.LastCheckIn.Time.Format("2006-01-02 15:04:05 MST")))
	}
	if srv.CreatedAt != nil && srv.CreatedAt.Valid {
		printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("Created:"), srv.CreatedAt.Time.Format("2006-01-02 15:04:05 MST")))
	}
	if srv.UpdatedAt != nil && srv.UpdatedAt.Valid {
		printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("Updated:"), srv.UpdatedAt.Time.Format("2006-01-02 15:04:05 MST")))
	}

	printer.Print("")
}

func init() {
	ServersCmd.AddCommand(DescribeCmd)
}
