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
	table.AddRow("Platform", fmt.Sprintf("%s/%s", server.Config.Payload.Platform.OS, server.Config.Payload.Platform.Arch))
	table.AddRow("Config Version", fmt.Sprintf("%d", server.Config.Version))

	// Note: Actual health status would need to come from control plane API
	// which tracks heartbeats. For now, we just show the server exists.
	table.AddRow("Status", "Registered")

	deps.Printer.Print(table.Render())
	return nil
}

func init() {
	ServersCmd.AddCommand(StatusCmd)
}
