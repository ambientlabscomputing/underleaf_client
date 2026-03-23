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
	addValue   string
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

		// Use --value flag if provided, otherwise prompt interactively
		value := addValue
		if value == "" {
			var err error
			value, err = ui.PromptSecret("Secret value:")
			if err != nil {
				return fmt.Errorf("failed to read secret: %w", err)
			}
			if value == "" {
				return fmt.Errorf("secret value cannot be empty")
			}
		}

		// Store on local agent
		port := getAgentPort(ctx)
		client := agent.NewClient(port)

		// Check if the secret already exists — add must not silently overwrite.
		_, getErr := client.DoRequest("GET", fmt.Sprintf("/api/v1/secrets/get/%s", url.PathEscape(name)), nil)
		if getErr == nil {
			return fmt.Errorf("secret %q already exists; use 'ufctl secrets rotate' to update it", name)
		}

		// Determine origin cluster ID
		originCluster := addCluster
		if originCluster == "" {
			deps := utils.NewDependencyManager(ctx)
			if clusterID, ok := deps.ConfigClient.Get("local.cluster_id"); ok && clusterID != nil {
				originCluster = fmt.Sprintf("%v", clusterID)
			}
		}

		// Register metadata with control plane FIRST so we can store the
		// server_api ID in the local secret's custom metadata. The replication
		// loop uses this ID to correlate local secrets with server_api records.
		deps := utils.NewDependencyManager(ctx)
		req := controlplane.CreateSecretMetadataRequest{
			Name:            name,
			Scope:           addScope,
			OriginClusterID: originCluster,
		}

		customMeta := map[string]string{}
		meta, regErr := deps.CPlaneClient.Secrets.CreateSecretMetadata(ctx, req)
		if regErr != nil {
			printer.PrintWarning(fmt.Sprintf("Failed to sync metadata to control plane: %v", regErr))
			printer.PrintWarning("Secret will be stored locally without a control plane link.")
		} else {
			customMeta["server_api_id"] = meta.ID
		}

		payload, _ := json.Marshal(map[string]interface{}{
			"data":            map[string]string{"value": value},
			"custom_metadata": customMeta,
		})
		_, err := client.DoRequest("PUT", fmt.Sprintf("/api/v1/secrets/put/%s", url.PathEscape(name)), payload)
		if err != nil {
			return fmt.Errorf("failed to store secret on local agent: %w", err)
		}

		printer.PrintSuccess("Secret stored locally (version 1)")

		if regErr == nil {
			printer.PrintSuccess(fmt.Sprintf("Metadata synced to control plane (id: %s)", meta.ID))
			if meta.Scope == "org" {
				printer.Print("Replication to org clusters will be scheduled.")
			}
		}

		return nil
	},
}

func init() {
	addCmd.Flags().StringVar(&addScope, "scope", "cluster", "Secret scope: 'org' (replicate to all clusters) or 'cluster' (local only)")
	addCmd.Flags().StringVar(&addCluster, "cluster", "", "Origin cluster ID (defaults to local cluster from config)")
	addCmd.Flags().StringVar(&addValue, "value", "", "Secret value (skips interactive prompt)")
	addCmd.Flags().IntVarP(&agentPort, "port", "p", 0, "Agent port (default: 2240)")
}
