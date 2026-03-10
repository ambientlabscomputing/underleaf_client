package mmesh

import (
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/agent"
	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show MMA event stream status",
	Long: `Display the status of the UA→MMA event stream server including:
- Running status
- Cluster and node information
- Socket address
- Active subscriber count
- Buffer usage statistics`,
	RunE: func(cmd *cobra.Command, args []string) error {
		printer := ui.GetPrinter(cmd.Context())

		// Try to get port from config first
		config := policy_manager.GetConfig(cmd.Context())
		port := 2240 // default
		if portVal, ok := config.Get("agent.port"); ok {
			if portInt, ok := portVal.(int); ok {
				port = portInt
			}
		}

		// Check if agent is running using the configured port
		launcher := agent.NewLauncher(agent.LauncherConfig{
			Port: port,
		})
		agentStatus := launcher.GetStatus()

		if !agentStatus.Running {
			printer.PrintWarning("Agent is not running")
			printer.PrintInfo("\nStart the agent first:")
			printer.PrintInfo("  ufctl agent start --dev")
			printer.PrintInfo("  or")
			printer.PrintInfo("  ufctl start")
			return fmt.Errorf("agent is not running")
		}

		// Create agent client using the configured port
		client := agent.NewClient(agentStatus.Port)

		// Get MMA status
		status, err := client.GetMMeshStatus()
		if err != nil {
			printer.PrintError(fmt.Sprintf("Failed to get MMA status: %v", err))
			printer.PrintInfo("\nTroubleshooting:")
			printer.PrintInfo("- Ensure the agent is running: ufctl agent status")
			printer.PrintInfo("- Check if MMA event stream is enabled in config")
			return err
		}

		// Check if disabled
		if statusStr, ok := status["status"].(string); ok && statusStr == "disabled" {
			printer.PrintWarning("MMA event stream server is not initialized")
			printer.PrintInfo("\nThe Mycelium Mesh Agent integration is not enabled.")
			printer.PrintInfo("It will be automatically enabled when the agent is assigned to a cluster.")
			return nil
		}

		// Display status
		table := ui.NewTableBuilder().
			WithTitle("Mycelium Mesh Agent Status").
			WithHeaders("Property", "Value")

		if s, ok := status["status"].(string); ok {
			table.AddRow("Status", s)
		}

		if cid, ok := status["cluster_id"].(string); ok {
			table.AddRow("Cluster ID", cid)
		}

		if nid, ok := status["node_id"].(string); ok {
			table.AddRow("Node ID", nid)
		}

		if addr, ok := status["socket_address"].(string); ok {
			table.AddRow("Socket Address", addr)
		}

		if count, ok := status["subscriber_count"].(float64); ok {
			table.AddRow("Active Subscribers", fmt.Sprintf("%.0f", count))
		}

		// Add buffer info
		if bufferInfo, ok := status["buffer"].(map[string]interface{}); ok {
			if size, ok := bufferInfo["size"].(float64); ok {
				table.AddRow("Buffer Size", fmt.Sprintf("%.0f events", size))
			}
			if capacity, ok := bufferInfo["capacity"].(float64); ok {
				table.AddRow("Buffer Capacity", fmt.Sprintf("%.0f events", capacity))
			}
			if usage, ok := bufferInfo["usage"].(string); ok {
				table.AddRow("Buffer Usage", usage)
			}
		}

		printer.Print(table.Render())
		return nil
	},
}
