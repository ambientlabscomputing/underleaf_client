package secrets

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/ambientlabscomputing/underleaf_client/internal/agent"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

var (
	addScope   string
	addCluster string
)

var addCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Store a new secret on the local cluster",
	Long: `Store an encrypted secret on the local cluster and register its metadata
with the control plane.

For org-scoped secrets, replication to other clusters in the organization will
be scheduled automatically. For cluster-scoped secrets, the value stays local.

  ufctl secrets add DATABASE_PASSWORD --scope org
  ufctl secrets add API_KEY --scope cluster`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		printer := ui.GetPrinter(ctx)
		name := args[0]

		if addScope != "org" && addScope != "cluster" {
			return fmt.Errorf("--scope must be 'org' or 'cluster'")
		}

		printer.Print(fmt.Sprintf("Creating secret %q with scope: %s", name, addScope))

		// Prompt for secret value (masked input)
		value, err := ui.PromptSecret("Secret value:")
		if err != nil {
			return fmt.Errorf("failed to read secret: %w", err)
		}
		if value == "" {
			return fmt.Errorf("secret value cannot be empty")
		}

		// Store on local agent
		port := getAgentPort(ctx)
		client := agent.NewClient(port)

		payload, _ := json.Marshal(map[string]string{"value": value})
		_, err = client.DoRequest("PUT", fmt.Sprintf("/api/v1/secrets/%s", url.PathEscape(name)), payload)
		if err != nil {
			return fmt.Errorf("failed to store secret on local agent: %w", err)
		}

		printer.PrintSuccess("Secret stored locally (version 1)")

		// Determine origin cluster ID
		originCluster := addCluster
		if originCluster == "" {
			// Try to read from config
			deps := utils.NewDependencyManager(ctx)
			if clusterID, ok := deps.ConfigClient.Get("local.cluster_id"); ok && clusterID != nil {
				originCluster = fmt.Sprintf("%v", clusterID)
			}
		}

		// Register metadata with control plane
		deps := utils.NewDependencyManager(ctx)
		req := controlplane.CreateSecretMetadataRequest{
			Name:            name,
			Scope:           addScope,
			OriginClusterID: originCluster,
		}

		meta, err := deps.CPlaneClient.Secrets.CreateSecretMetadata(ctx, req)
		if err != nil {
			printer.PrintWarning(fmt.Sprintf("Secret stored locally but failed to sync metadata: %v", err))
			return nil
		}

		printer.PrintSuccess(fmt.Sprintf("Metadata synced to control plane (id: %s)", meta.ID))
		if meta.Scope == "org" {
			printer.Print("Replication to org clusters will be scheduled.")
		}

		return nil
	},
}

func init() {
	addCmd.Flags().StringVar(&addScope, "scope", "cluster", "Secret scope: 'org' (replicate to all clusters) or 'cluster' (local only)")
	addCmd.Flags().StringVar(&addCluster, "cluster", "", "Origin cluster ID (defaults to local cluster from config)")
	addCmd.Flags().IntVarP(&agentPort, "port", "p", 0, "Agent port (default: 2240)")
}
