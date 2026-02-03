package cluster

import (
	"fmt"
	"strings"

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

		printer.Print("🚀 Creating new Raft cluster...\n")

		// TODO: Call local agent API to bootstrap Raft cluster
		// For now, we'll generate the join token and display instructions

		// Generate a cluster ID (in production, this would come from the agent after bootstrap)
		clusterID := strings.ReplaceAll(createName, " ", "-")
		clusterID = strings.ToLower(clusterID)

		// If no node ID provided, use hostname or generate one
		if createNodeID == "" {
			createNodeID = "node-1" // In production, get from agent config
		}

		// Generate join token
		token, err := cluster.GenerateJoinToken(clusterID, createNodeID, createCAFingerprint)
		if err != nil {
			return fmt.Errorf("failed to generate join token: %w", err)
		}

		logger.Info("cluster created", "cluster_id", clusterID, "root_node", createNodeID)

		printer.PrintSuccess("✓ Cluster created successfully\n")

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
