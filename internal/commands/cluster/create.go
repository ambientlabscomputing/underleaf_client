package cluster

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/agent"
	"github.com/ambientlabscomputing/underleaf_client/internal/cluster"
	"github.com/ambientlabscomputing/underleaf_client/internal/logging"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

var (
	createName          string
	createNodeID        string
	createCAFingerprint string
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new Raft cluster and generate join credentials",
	Long: `Create a new Raft cluster with this node as the bootstrap (root) node.

This command:
1. Bootstraps a new Raft cluster with this node as the leader
2. Generates join credentials (short code + token) for other nodes
3. Outputs the credentials for distribution to other nodes

The generated join token allows other nodes to request membership in this cluster.
Nodes must complete the trust ceremony before being added as members.

Examples:
  # Create a cluster with auto-generated ID
  ufctl cluster create --name production-cluster

  # Create with specific node ID
  ufctl cluster create --name my-cluster --node-id node-01

  # Create with CA fingerprint for additional verification
  ufctl cluster create --name secure-cluster --ca-fingerprint "A7:B3:2F:..."

Requirements:
  • Raft must be enabled in the agent configuration
  • Agent must be running locally or accessible via --host flag
  • Node must not already be part of a cluster`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := logging.GetLogger(ctx)
		printer := ui.GetPrinter(ctx)

		// Validate inputs
		if createName == "" {
			return fmt.Errorf("cluster name is required (--name)")
		}

		printer.Print("🚀 Verifying cluster state...\n")

		// Create agent client with effective port
		effectivePort := getEffectiveAgentPort(cmd, agentPort)
		client := agent.NewClient(effectivePort)

		// Call agent API to verify cluster is bootstrapped
		resp, err := client.DoRequest("GET", "/api/v1/raft/status", nil)
		if err != nil {
			logger.Error("failed to get cluster status", "error", err)
			return fmt.Errorf("failed to verify cluster status: %w\n\nEnsure the agent is running and Raft is enabled in config", err)
		}

		var status map[string]interface{}
		if err := json.Unmarshal(resp, &status); err != nil {
			return fmt.Errorf("failed to parse cluster status: %w", err)
		}

		// Check leader status; if not leader, determine whether we can bootstrap.
		isLeader, _ := status["is_leader"].(bool)
		if !isLeader {
			// Un-bootstrapped state: nodes is an empty map ({}). In this case we can
			// bootstrap this node as the single-node cluster leader.
			nodes, _ := status["nodes"].(map[string]interface{})
			if len(nodes) != 0 {
				// There ARE peers but this node isn't the leader — user must run create
				// from the leader node.
				role, _ := status["role"].(string)
				return fmt.Errorf("this node is not the cluster leader (role: %s)\n\nJoin tokens must be generated from the leader node", role)
			}

			// nodes == {} → cluster has never been bootstrapped. Bootstrap now.
			printer.Print("⚙️  Cluster not yet bootstrapped — bootstrapping this node as leader...")
			if _, err := client.DoRequest("POST", "/api/v1/raft/bootstrap", nil); err != nil {
				return fmt.Errorf("failed to bootstrap cluster: %w", err)
			}

			// Wait for leader election (single-node typically takes ~500ms)
			printer.Print("⏳ Waiting for leader election...")
			deadline := time.Now().Add(15 * time.Second)
			for time.Now().Before(deadline) {
				time.Sleep(500 * time.Millisecond)
				resp2, err2 := client.DoRequest("GET", "/api/v1/raft/status", nil)
				if err2 != nil {
					continue
				}
				var s2 map[string]interface{}
				if err2 = json.Unmarshal(resp2, &s2); err2 != nil {
					continue
				}
				if ok2, _ := s2["is_leader"].(bool); ok2 {
					isLeader = true
					printer.PrintSuccess("✓ This node is now the cluster leader")
					break
				}
			}
			if !isLeader {
				return fmt.Errorf("timed out waiting for this node to become cluster leader after bootstrap")
			}
		}

		// Extract cluster/node information from status
		var clusterID, nodeID string
		if nodes, ok := status["nodes"].([]interface{}); ok && len(nodes) > 0 {
			if nodeMap, ok := nodes[0].(map[string]interface{}); ok {
				nodeID, _ = nodeMap["id"].(string)
			}
		}

		// Fallback: generate cluster ID from name if not available
		if clusterID == "" {
			clusterID = strings.ReplaceAll(createName, " ", "-")
			clusterID = strings.ToLower(clusterID)
		}

		// Fallback: use provided or default node ID if not extracted from status
		if nodeID == "" {
			if createNodeID != "" {
				nodeID = createNodeID
			} else {
				nodeID = "node-1"
			}
		}

		// Generate join token
		token, err := cluster.GenerateJoinToken(clusterID, nodeID, createCAFingerprint)
		if err != nil {
			return fmt.Errorf("failed to generate join token: %w", err)
		}

		logger.Info("cluster join token generated", "cluster_id", clusterID, "root_node", nodeID)

		printer.PrintSuccess("✓ Cluster verified and join token generated\n")

		// Display cluster information
		printer.Print("📋 Cluster Information:")
		printer.Print(fmt.Sprintf("   Name:       %s", createName))
		printer.Print(fmt.Sprintf("   Cluster ID: %s", clusterID))
		printer.Print(fmt.Sprintf("   Root Node:  %s", createNodeID))
		printer.Print(fmt.Sprintf("   Created:    %s", token.CreatedAt.Format("2006-01-02 15:04:05")))
		if createCAFingerprint != "" {
			printer.Print(fmt.Sprintf("   CA Fingerprint: %s", cluster.FormatFingerprint(createCAFingerprint)))
		}

		// Display join credentials prominently
		printer.Print("\n🔑 Join Credentials (share with other nodes):")
		printer.Print("   ┌────────────────────────────────────────┐")
		printer.Print(fmt.Sprintf("   │  Join Code:  %s             │", token.Code))
		printer.Print("   └────────────────────────────────────────┘")
		printer.Print("")
		printer.Print(fmt.Sprintf("   Full Token: %s", token.FullToken))
		printer.Print(fmt.Sprintf("   Expires:    %s (%s remaining)",
			token.ExpiresAt.Format("15:04:05"),
			token.ExpiresAt.Sub(token.CreatedAt)))

		// Display next steps for other nodes
		printer.Print("\n💡 Adding Other Nodes:")
		printer.Print("   On each node you want to add to this cluster, run:")
		printer.Print("")
		printer.Print(fmt.Sprintf("   ufctl cluster join %s --code %s", createNodeID, token.Code))
		printer.Print("")
		printer.Print("   Or with the full token:")
		printer.Print(fmt.Sprintf("   ufctl cluster join %s --token %s", createNodeID, token.FullToken))

		// Security reminder
		printer.Print("\n⚠️  Security Reminder:")
		printer.Print("   • Join credentials grant cluster access - keep them secure")
		printer.Print("   • Tokens expire after 1 hour for security")
		printer.Print("   • Verify node identity during the trust ceremony")
		printer.Print("   • Both nodes must confirm fingerprints match")

		// Report token to server_api if configured
		printer.Print("\n📡 Reporting cluster to control plane...")
		// TODO: Call server_api to create cluster record
		printer.PrintWarning("   (server_api integration pending)")

		return nil
	},
}

func init() {
	createCmd.Flags().StringVarP(&createName, "name", "n", "", "Cluster name (required)")
	createCmd.Flags().StringVar(&createNodeID, "node-id", "", "Node ID for this root node (default: auto-generated)")
	createCmd.Flags().StringVar(&createCAFingerprint, "ca-fingerprint", "", "CA certificate fingerprint for verification")
	createCmd.MarkFlagRequired("name")
	ClusterCmd.AddCommand(createCmd)
}
