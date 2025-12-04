package servers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/agent"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

var StatusCmd = &cobra.Command{
	Use:   "status [server-id]",
	Short: "Check the status of a server",
	Long: `Check the running status and health of a server.

If no server ID is provided, checks the status of the local agent.

Examples:
  ufctl servers status              # Check local agent
  ufctl servers status server-123   # Check specific server`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		if len(args) == 0 {
			// Check local agent status
			return checkLocalStatus(deps.Printer)
		}

		// Check remote server status
		serverID := args[0]
		return checkRemoteStatus(deps, serverID)
	},
}

func checkLocalStatus(printer ui.Printer) error {
	// Check if local agent is running
	launcher := agent.NewLauncher(agent.LauncherConfig{})

	if !launcher.IsRunning() {
		printer.PrintWarning("Local agent is not running")
		printer.Print("  Start with: ufctl local agent start")
		return nil
	}

	status := launcher.GetStatus()
	printer.PrintSuccess("Local agent is running")

	// Build status table
	table := ui.NewTableBuilder().
		WithTitle("Agent Status").
		WithHeaders("Property", "Value")

	table.AddRow("PID", fmt.Sprintf("%d", status.PID))
	table.AddRow("Port", fmt.Sprintf("%d", status.Port))
	table.AddRow("Log File", status.LogFile)

	// Try to get health from agent
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d/health", status.Port))
	if err != nil {
		table.AddRow("Health", "Unknown (cannot connect)")
	} else {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			table.AddRow("Health", "Healthy")
		} else {
			table.AddRow("Health", fmt.Sprintf("Unhealthy (%d)", resp.StatusCode))
		}
	}

	printer.Print(table.Render())
	return nil
}

func checkRemoteStatus(deps *utils.DependencyManager, serverID string) error {
	ctx := context.Background()

	// Get server info from control plane
	server, err := deps.ServerSvc.GetServer(ctx, serverID)
	if err != nil {
		deps.Printer.PrintError("Failed to get server: " + err.Error())
		return err
	}

	// Build status table
	table := ui.NewTableBuilder().
		WithTitle(fmt.Sprintf("Server Status: %s", server.Name)).
		WithHeaders("Property", "Value")

	table.AddRow("ID", server.ID)
	table.AddRow("Name", server.Name)

	// Show status from API
	if server.Status != "" {
		table.AddRow("Status", server.Status)
	} else {
		table.AddRow("Status", "Registered")
	}

	// Show location if available
	if server.Location != "" {
		table.AddRow("Location", server.Location)
	}

	// Show platform
	if server.Platform != nil {
		table.AddRow("Platform", fmt.Sprintf("%s/%s", server.Platform.OS, server.Platform.Arch))
	} else if server.Config.Payload.Platform.OS != "" {
		table.AddRow("Platform", fmt.Sprintf("%s/%s", server.Config.Payload.Platform.OS, server.Config.Payload.Platform.Arch))
	}

	table.AddRow("Config Version", fmt.Sprintf("%d", server.Config.Version))

	// Show metrics if available
	if server.Metrics != nil {
		table.AddRow("CPU Usage", fmt.Sprintf("%.1f%%", server.Metrics.CPUUsage))
		table.AddRow("Memory Usage", fmt.Sprintf("%.1f%%", server.Metrics.MemoryUsage))
		table.AddRow("Disk Usage", fmt.Sprintf("%.1f%%", server.Metrics.DiskUsage))
	}

	// Show last check-in if available
	if server.LastCheckIn != nil && server.LastCheckIn.Valid {
		table.AddRow("Last Check-in", server.LastCheckIn.Time.Format("2006-01-02 15:04:05 MST"))
	}

	deps.Printer.Print(table.Render())
	return nil
}

func init() {
	ServersCmd.AddCommand(StatusCmd)
}
