package mmesh

import (
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/agent"
	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

var subscribersCmd = &cobra.Command{
	Use:   "subscribers",
	Short: "List active MMA subscribers",
	Long: `Display information about active Mycelium Mesh Agent (MMA) subscribers
connected to the UA event stream.`,
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
			return fmt.Errorf("agent is not running")
		}

		// Create agent client using the configured port
		client := agent.NewClient(agentStatus.Port)

		// Get subscriber info
		result, err := client.GetMMeshSubscribers()
		if err != nil {
			printer.PrintError(fmt.Sprintf("Failed to get MMA subscribers: %v", err))
			return err
		}

		count := 0
		if c, ok := result["subscriber_count"].(float64); ok {
			count = int(c)
		}

		if count == 0 {
			printer.PrintInfo("No active MMA subscribers")
			printer.PrintInfo("\nThe Mycelium Mesh Agent is not currently connected to this node.")
			return nil
		}

		printer.PrintSuccess(fmt.Sprintf("Active subscribers: %d", count))

		// TODO: Display subscriber details when available
		// For now, we just show the count
		printer.PrintInfo("\nNote: Detailed subscriber information is not yet available.")

		return nil
	},
}
