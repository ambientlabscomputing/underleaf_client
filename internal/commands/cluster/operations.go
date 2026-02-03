package cluster

import (
	"encoding/json"
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/agent"
	"github.com/ambientlabscomputing/underleaf_client/internal/cluster"
	"github.com/ambientlabscomputing/underleaf_client/internal/logging"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

type configAgentPortKey struct{}

var (
	agentPort int
	joinCode  string
	joinToken string
)

// getEffectiveAgentPort returns the agent port to use, prioritizing global flag from context
func getEffectiveAgentPort(cmd *cobra.Command, localPort int) int {
	// First try to get global flag value directly from the root command
	if globalPort, err := cmd.Root().PersistentFlags().GetInt("config-agent-port"); err == nil && globalPort != 0 {
		return globalPort
	}

	// Fallback to context (for backward compatibility)
	ctx := cmd.Context()
	if globalPort, ok := ctx.Value(configAgentPortKey{}).(int); ok && globalPort != 0 {
		return globalPort
	}
	return localPort
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Raft cluster status",
	Long:  `Display the current status of the Raft cluster including role, leader, and cluster members.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := logging.GetLogger(ctx)
		printer := ui.GetPrinter(ctx)

		// Create agent client with effective port
		effectivePort := getEffectiveAgentPort(cmd, agentPort)
		client := agent.NewClient(effectivePort)

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
	Use:   "join <node-id> [address]",
	Short: "Join this node to an existing cluster with trust verification",
	Long: `Join this node to an existing cluster using the trust ceremony.

This command initiates a secure join process:
1. Connect to the target node (cluster member)
2. Present the join code for verification
3. Exchange CA fingerprints for mutual trust
4. Complete the join if verification succeeds

You can provide either a short code (--code) or full token (--token).

Examples:
  # Join using short code (recommended for manual entry)
  ufctl cluster join node-1 --code A7K2-N9P4

  # Join using full token
  ufctl cluster join node-1 --token A7K2N9P4-abc123...

  # Legacy: Direct join with address (bypasses trust ceremony)
  ufctl cluster join <node-id> <address>`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := logging.GetLogger(ctx)
		printer := ui.GetPrinter(ctx)

		nodeID := args[0]

		// Check if using trust ceremony (--code or --token) vs legacy mode
		if joinCode == "" && joinToken == "" && len(args) < 2 {
			return fmt.Errorf("either --code/--token or <address> is required")
		}

		// Legacy mode: direct join with address (bypasses trust ceremony)
		if len(args) >= 2 && joinCode == "" && joinToken == "" {
			address := args[1]
			return legacyJoin(cmd, ctx, logger, printer, nodeID, address)
		}

		// Trust ceremony mode
		if joinCode != "" {
			// Validate code format
			if !cluster.ValidateJoinCode(joinCode) {
				return fmt.Errorf("invalid join code format (expected: XXXX-XXXX)")
			}
			printer.Print(fmt.Sprintf("🔐 Using join code: %s\n", joinCode))
		} else if joinToken != "" {
			printer.Print("🔐 Using full join token\n")
		}

		printer.Print("🤝 Initiating trust ceremony with cluster...\n")
		printer.PrintWarning("⏳ Trust ceremony implementation pending")
		printer.Print("   This will:")
		printer.Print("   1. Contact the target node")
		printer.Print("   2. Exchange CA fingerprints")
		printer.Print("   3. Verify mutual trust")
		printer.Print("   4. Complete cluster join")

		// TODO: Implement trust ceremony HTTP requests
		// POST /api/v1/cluster/join-request with code/token
		// Wait for confirmation
		// POST /api/v1/cluster/join-confirm with fingerprint verification

		return fmt.Errorf("trust ceremony not yet implemented - use legacy join")
	},
}

// legacyJoin performs a direct join without trust ceremony (for backward compatibility)
func legacyJoin(cmd *cobra.Command, ctx interface{}, logger interface{}, printer *ui.Printer, nodeID, address string) error {
	// Create agent client with effective port
	effectivePort := getEffectiveAgentPort(cmd, agentPort)
	client := agent.NewClient(effectivePort)

	// Add node
	payload := map[string]string{
		"node_id": nodeID,
		"address": address,
	}

	payloadBytes, _ := json.Marshal(payload)
	_, err := client.DoRequest("POST", "/api/v1/raft/nodes", payloadBytes)
	if err != nil {
		return fmt.Errorf("failed to add node: %w", err)
	}

	printer.PrintSuccess(fmt.Sprintf("Successfully added node %s to cluster", nodeID))
	printer.Print("Note: New nodes are added as non-voters. Use 'cluster promote' to make them voters.")

	return nil
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

		// Create agent client with effective port
		effectivePort := getEffectiveAgentPort(cmd, agentPort)
		client := agent.NewClient(effectivePort)

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

		// Create agent client with effective port
		effectivePort := getEffectiveAgentPort(cmd, agentPort)
		client := agent.NewClient(effectivePort)

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

		// Create agent client with effective port
		effectivePort := getEffectiveAgentPort(cmd, agentPort)
		client := agent.NewClient(effectivePort)

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

		// Create agent client with effective port
		effectivePort := getEffectiveAgentPort(cmd, agentPort)
		client := agent.NewClient(effectivePort)

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
	joinCmd.Flags().StringVarP(&joinCode, "code", "c", "", "Join code for trust ceremony (e.g., A7K2-N9P4)")
	joinCmd.Flags().StringVarP(&joinToken, "token", "t", "", "Full join token")
	leaveCmd.Flags().IntVarP(&agentPort, "port", "p", 8081, "Agent port")
	promoteCmd.Flags().IntVarP(&agentPort, "port", "p", 8081, "Agent port")
	demoteCmd.Flags().IntVarP(&agentPort, "port", "p", 8081, "Agent port")
	maintenanceCmd.Flags().IntVarP(&agentPort, "port", "p", 8081, "Agent port")
}
