package mmesh

import (
	"encoding/json"
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/agent"
	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

var (
	eventType  string
	payload    string
	entityKind string
	entityID   string
)

var publishCmd = &cobra.Command{
	Use:   "publish",
	Short: "Publish a test event to MMA",
	Long: `Publish a test event to the Mycelium Mesh Agent event stream.
This is useful for testing and debugging the UA→MMA integration.

Example:
  ufctl mmesh publish --event-type "test.event" --payload '{"message":"hello"}' --entity-kind "service" --entity-id "svc-123"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		printer := ui.GetPrinter(cmd.Context())

		// Validate required flags
		if eventType == "" {
			printer.PrintError("--event-type is required")
			return fmt.Errorf("event-type is required")
		}

		if payload == "" {
			printer.PrintError("--payload is required")
			return fmt.Errorf("payload is required")
		}

		// Parse payload JSON
		var payloadMap map[string]interface{}
		if err := json.Unmarshal([]byte(payload), &payloadMap); err != nil {
			printer.PrintError(fmt.Sprintf("Invalid JSON payload: %v", err))
			return err
		}

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

		// Publish event
		result, err := client.PublishMMeshEvent(eventType, payloadMap, entityKind, entityID)
		if err != nil {
			printer.PrintError(fmt.Sprintf("Failed to publish event: %v", err))
			return err
		}

		// Display result
		printer.PrintSuccess("Event published successfully")

		if status, ok := result["status"].(string); ok {
			printer.Print(fmt.Sprintf("\nStatus: %s", status))
		}

		if et, ok := result["event_type"].(string); ok {
			printer.Print(fmt.Sprintf("Event Type: %s", et))
		}

		if ek, ok := result["entity_kind"].(string); ok && ek != "" {
			printer.Print(fmt.Sprintf("Entity Kind: %s", ek))
		}

		if eid, ok := result["entity_id"].(string); ok && eid != "" {
			printer.Print(fmt.Sprintf("Entity ID: %s", eid))
		}

		return nil
	},
}

func init() {
	publishCmd.Flags().StringVar(&eventType, "event-type", "", "Event type (required)")
	publishCmd.Flags().StringVar(&payload, "payload", "", "Event payload as JSON (required)")
	publishCmd.Flags().StringVar(&entityKind, "entity-kind", "", "Entity kind (optional)")
	publishCmd.Flags().StringVar(&entityID, "entity-id", "", "Entity ID (optional)")
	publishCmd.MarkFlagRequired("event-type")
	publishCmd.MarkFlagRequired("payload")
}
