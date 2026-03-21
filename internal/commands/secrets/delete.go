package secrets

import (
	"fmt"
	"net/url"

	"github.com/ambientlabscomputing/underleaf_client/internal/agent"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

var deleteForce bool

var deleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Revoke a secret",
	Long: `Revoke a secret: delete it from the local agent and mark it as revoked
in the control plane. The revocation is broadcast to all clusters that hold a copy.

  ufctl secrets delete DATABASE_PASSWORD`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		printer := ui.GetPrinter(ctx)
		name := args[0]

		if !deleteForce {
			confirmed, err := ui.Confirm(fmt.Sprintf("This will revoke %q across all clusters. Continue?", name))
			if err != nil {
				return err
			}
			if !confirmed {
				printer.Print("Aborted.")
				return nil
			}
		}

		// Delete from local agent
		port := getAgentPort(ctx)
		client := agent.NewClient(port)

		_, err := client.DoRequest("DELETE", fmt.Sprintf("/api/v1/secrets/delete/%s", url.PathEscape(name)), nil)
		if err != nil {
			return fmt.Errorf("failed to delete secret from local agent: %w", err)
		}
		printer.PrintSuccess("Secret revoked locally")

		// Find secret ID in control plane by name
		deps := utils.NewDependencyManager(ctx)
		resp, err := deps.CPlaneClient.Secrets.ListSecretMetadata(ctx, name, "", "", 10, 0)
		if err != nil {
			printer.PrintWarning(fmt.Sprintf("Secret deleted locally but could not update control plane: %v", err))
			return nil
		}

		var secretID string
		for _, s := range resp.Results {
			if s.Name == name {
				secretID = s.ID
				break
			}
		}

		if secretID == "" {
			printer.PrintWarning("Secret not found in control plane (may already be deleted).")
			return nil
		}

		revoked := "revoked"
		_, err = deps.CPlaneClient.Secrets.PatchSecretMetadata(ctx, secretID, controlplane.PatchSecretMetadataRequest{State: &revoked})
		if err != nil {
			printer.PrintWarning(fmt.Sprintf("Secret deleted locally but failed to update control plane state: %v", err))
			return nil
		}

		printer.PrintSuccess("Control plane state updated to 'revoked'")
		return nil
	},
}

func init() {
	deleteCmd.Flags().BoolVar(&deleteForce, "force", false, "Skip confirmation prompt")
	deleteCmd.Flags().IntVarP(&agentPort, "port", "p", 0, "Agent port (default: 2240)")
}
