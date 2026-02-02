package cluster

import (
	"encoding/json"
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/agent"
	"github.com/ambientlabscomputing/underleaf_client/internal/logging"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

var (
	agentPort int
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Raft cluster status",
	Long:  `Display the current status of the Raft cluster including role, leader, and cluster members.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := logging.GetLogger(ctx)
		printer := ui.GetPrinter(ctx)

		// Create agent client
		client := agent.NewClient(agentPort)

		// Get Raft status
		resp, err := client.DoRequest("GET", "/api/v1/raft/status", nil)
		if err != nil {
			logger.Error("failed to get raft status", "error", err)
			return fmt.Errorf("failed to get raft status: %w", err)
		}

		var status map[string]interface{}
		if err := json.Unmarshal(resp, &status); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		// Print status
		printer.PrintSuccess("Raft Cluster Status")
		printer.Print(fmt.Sprintf("Role: %v", status["role"]))
		printer.Print(fmt.Sprintf("Is Leader: %v", status["is_leader"]))
		printer.Print(fmt.Sprintf("Maintenance Mode: %v", status["maintenance_mode"]))

		// Print nodes if available
		if nodes, ok := status["nodes"].([]interface{}); ok {
			printer.Print(fmt.Sprintf("\nCluster Members (%d):", len(nodes)))
			for _, node := range nodes {
				if nodeMap, ok := node.(map[string]interface{}); ok {
					printer.Print(fmt.Sprintf("  - %v @ %v (suffrage: %v)",
						nodeMap["id"], nodeMap["address"], nodeMap["suffrage"]))
				}
			}
		}

		return nil
	},
}

var joinCmd = &cobra.Command{
	Use:   "join <node-id> <address>",
	Short: "Add a node to the cluster",
	Long:  `Add a new node to the Raft cluster. The node will be added as a non-voter initially.`,
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := logging.GetLogger(ctx)
		printer := ui.GetPrinter(ctx)

		nodeID := args[0]
		address := args[1]

		// Create agent client
		client := agent.NewClient(agentPort)

		// Add node
		payload := map[string]string{
			"node_id": nodeID,
			"address": address,
		}

		payloadBytes, _ := json.Marshal(payload)
		_, err := client.DoRequest("POST", "/api/v1/raft/nodes", payloadBytes)
		if err != nil {
			logger.Error("failed to add node", "error", err, "node_id", nodeID)
			return fmt.Errorf("failed to add node: %w", err)
		}

		printer.PrintSuccess(fmt.Sprintf("Successfully added node %s to cluster", nodeID))
		printer.Print("Note: New nodes are added as non-voters. Use 'cluster promote' to make them voters.")

		return nil
	},
}

var leaveCmd = &cobra.Command{
	Use:   "leave <node-id>",
	Short: "Remove a node from the cluster",
	Long:  `Remove a node from the Raft cluster. This should only be used when a node is being permanently removed.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := logging.GetLogger(ctx)
		printer := ui.GetPrinter(ctx)

		nodeID := args[0]

		// Create agent client
		client := agent.NewClient(agentPort)

		// Remove node
		_, err := client.DoRequest("DELETE", fmt.Sprintf("/api/v1/raft/nodes/%s", nodeID), nil)
		if err != nil {
			logger.Error("failed to remove node", "error", err, "node_id", nodeID)
			return fmt.Errorf("failed to remove node: %w", err)
		}

		printer.PrintSuccess(fmt.Sprintf("Successfully removed node %s from cluster", nodeID))

		return nil
	},
}

var promoteCmd = &cobra.Command{
	Use:   "promote <node-id>",
	Short: "Promote a non-voter to voter",
	Long: `Promote a non-voting node to a voting member of the cluster.
This requires the cluster to be in maintenance mode.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := logging.GetLogger(ctx)
		printer := ui.GetPrinter(ctx)

		nodeID := args[0]

		// Create agent client
		client := agent.NewClient(agentPort)

		// Promote node
		_, err := client.DoRequest("POST", fmt.Sprintf("/api/v1/raft/nodes/%s/promote", nodeID), nil)
		if err != nil {
			logger.Error("failed to promote node", "error", err, "node_id", nodeID)
			return fmt.Errorf("failed to promote node: %w (hint: cluster must be in maintenance mode)", err)
		}

		printer.PrintSuccess(fmt.Sprintf("Successfully promoted node %s to voter", nodeID))

		return nil
	},
}

var demoteCmd = &cobra.Command{
	Use:   "demote <node-id>",
	Short: "Demote a voter to non-voter",
	Long: `Demote a voting node to a non-voting member of the cluster.
This requires the cluster to be in maintenance mode.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := logging.GetLogger(ctx)
		printer := ui.GetPrinter(ctx)

		nodeID := args[0]

		// Create agent client
		client := agent.NewClient(agentPort)

		// Demote node
		_, err := client.DoRequest("POST", fmt.Sprintf("/api/v1/raft/nodes/%s/demote", nodeID), nil)
		if err != nil {
			logger.Error("failed to demote node", "error", err, "node_id", nodeID)
			return fmt.Errorf("failed to demote node: %w (hint: cluster must be in maintenance mode)", err)
		}

		printer.PrintSuccess(fmt.Sprintf("Successfully demoted node %s to non-voter", nodeID))

		return nil
	},
}

var maintenanceCmd = &cobra.Command{
	Use:   "maintenance <enable|disable>",
	Short: "Control cluster maintenance mode",
	Long: `Enable or disable cluster maintenance mode.
Maintenance mode is required for certain operations like promoting/demoting nodes.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := logging.GetLogger(ctx)
		printer := ui.GetPrinter(ctx)

		action := args[0]
		if action != "enable" && action != "disable" {
			return fmt.Errorf("invalid action: %s (must be 'enable' or 'disable')", action)
		}

		// Create agent client
		client := agent.NewClient(agentPort)

		// Toggle maintenance mode
		_, err := client.DoRequest("POST", fmt.Sprintf("/api/v1/raft/maintenance/%s", action), nil)
		if err != nil {
			logger.Error("failed to change maintenance mode", "error", err, "action", action)
			return fmt.Errorf("failed to %s maintenance mode: %w", action, err)
		}

		printer.PrintSuccess(fmt.Sprintf("Successfully %sd maintenance mode", action))

		return nil
	},
}

func init() {
	// Add common flags
	statusCmd.Flags().IntVarP(&agentPort, "port", "p", 8081, "Agent port")
	joinCmd.Flags().IntVarP(&agentPort, "port", "p", 8081, "Agent port")
	leaveCmd.Flags().IntVarP(&agentPort, "port", "p", 8081, "Agent port")
	promoteCmd.Flags().IntVarP(&agentPort, "port", "p", 8081, "Agent port")
	demoteCmd.Flags().IntVarP(&agentPort, "port", "p", 8081, "Agent port")
	maintenanceCmd.Flags().IntVarP(&agentPort, "port", "p", 8081, "Agent port")
}
