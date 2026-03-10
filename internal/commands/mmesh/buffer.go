package mmesh

import (
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/agent"
	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

var bufferCmd = &cobra.Command{
	Use:   "buffer",
	Short: "Show event buffer statistics",
	Long: `Display detailed statistics about the UA→MMA event stream ring buffer,
including size, capacity, and usage percentage.`,
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

		// Get buffer stats
		stats, err := client.GetMMeshBuffer()
		if err != nil {
			printer.PrintError(fmt.Sprintf("Failed to get buffer statistics: %v", err))
			return err
		}

		// Display buffer stats
		table := ui.NewTableBuilder().
			WithTitle("Event Stream Buffer Statistics").
			WithHeaders("Property", "Value")

		if size, ok := stats["size"].(float64); ok {
			table.AddRow("Current Size", fmt.Sprintf("%.0f events", size))
		}

		if capacity, ok := stats["capacity"].(float64); ok {
			table.AddRow("Capacity", fmt.Sprintf("%.0f events", capacity))
		}

		if free, ok := stats["free"].(float64); ok {
			table.AddRow("Free Space", fmt.Sprintf("%.0f events", free))
		}

		if usage, ok := stats["usage"].(string); ok {
			table.AddRow("Usage", usage)
		}

		printer.Print(table.Render())

		// Add info about buffer behavior
		printer.PrintInfo("\nThe ring buffer stores recent events for catch-up when MMA reconnects.")
		printer.PrintInfo("When full, the oldest events are overwritten (ring buffer behavior).")

		return nil
	},
}
