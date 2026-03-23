package secrets

import (
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/agent"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

var (
	rotateFingerprint string
	rotateValue       string
)

var rotateCmd = &cobra.Command{
	Use:   "rotate <name>",
	Short: "Rotate a secret to a new value",
	Long: `Rotate a secret to a new value. The old value is replaced on the local agent
and the control plane version counter is incremented. All replication targets
will receive the updated value on next sync.

  ufctl secrets rotate DATABASE_PASSWORD`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		printer := ui.GetPrinter(ctx)
		name := args[0]

		// Use --value flag if provided, otherwise prompt interactively
		newValue := rotateValue
		if newValue == "" {
			var err error
			newValue, err = ui.PromptSecret("New secret value:")
			if err != nil {
				return fmt.Errorf("failed to read secret: %w", err)
			}
			if newValue == "" {
				return fmt.Errorf("secret value cannot be empty")
			}
		}

		// Pass the new value to the local agent (agent handles versioning internally)
		port := getAgentPort(ctx)
		client := agent.NewClient(port)

		// Fetch existing secret to preserve custom_metadata (e.g. server_api_id
		// needed by the replication loop).
		var existingCustomMeta map[string]string
		if getResp, getErr := client.DoRequest("GET", fmt.Sprintf("/api/v1/secrets/get/%s", url.PathEscape(name)), nil); getErr == nil {
			var parsed struct {
				Metadata struct {
					CustomMeta map[string]string `json:"custom_metadata"`
				} `json:"metadata"`
			}
			if json.Unmarshal(getResp, &parsed) == nil && len(parsed.Metadata.CustomMeta) > 0 {
				existingCustomMeta = parsed.Metadata.CustomMeta
			}
		}

		payload, _ := json.Marshal(map[string]interface{}{
			"data":            map[string]string{"value": newValue},
			"custom_metadata": existingCustomMeta,
		})
		respBytes, err := client.DoRequest("PUT", fmt.Sprintf("/api/v1/secrets/put/%s", url.PathEscape(name)), payload)
		if err != nil {
			return fmt.Errorf("failed to update secret on local agent: %w", err)
		}

		// Try to parse version from agent response
		var agentResp map[string]interface{}
		newVersion := uint64(1)
		if err2 := json.Unmarshal(respBytes, &agentResp); err2 == nil {
			if v, ok := agentResp["version"]; ok {
				switch val := v.(type) {
				case float64:
					newVersion = uint64(val)
				}
			}
		}

		printer.PrintSuccess(fmt.Sprintf("Secret updated locally (version %d)", newVersion))

		// Look up existing metadata in control plane
		deps := utils.NewDependencyManager(ctx)
		listResp, err := deps.CPlaneClient.Secrets.ListSecretMetadata(ctx, name, "", "", 10, 0)
		if err != nil {
			printer.PrintWarning(fmt.Sprintf("Secret rotated locally but could not update control plane: %v", err))
			return nil
		}

		var meta *controlplane.SecretMetadata
		for i := range listResp.Results {
			if listResp.Results[i].Name == name {
				meta = &listResp.Results[i]
				break
			}
		}

		if meta == nil {
			printer.PrintWarning("Secret rotated locally but not found in control plane.")
			return nil
		}

		// Increment version and record history entry
		entry := controlplane.VersionEntry{
			Version:     newVersion,
			Fingerprint: rotateFingerprint,
			CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		}
		active := "active"

		// Reset replication targets to "pending" so the replication loop
		// re-delivers the new version to all destination clusters.
		var resetTargets *[]controlplane.ReplicationTarget
		if len(meta.ReplicationTargets) > 0 {
			targets := make([]controlplane.ReplicationTarget, len(meta.ReplicationTargets))
			copy(targets, meta.ReplicationTargets)
			for i := range targets {
				targets[i].Status = "pending"
				targets[i].GrantID = ""
			}
			resetTargets = &targets
		}

		_, err = deps.CPlaneClient.Secrets.PatchSecretMetadata(ctx, meta.ID, controlplane.PatchSecretMetadataRequest{
			State:              &active,
			CurrentVersion:     &newVersion,
			VersionEntry:       &entry,
			ReplicationTargets: resetTargets,
		})
		if err != nil {
			printer.PrintWarning(fmt.Sprintf("Secret rotated locally but failed to update control plane version: %v", err))
			return nil
		}

		printer.PrintSuccess(fmt.Sprintf("Control plane updated — secret is now at version %d", newVersion))
		return nil
	},
}

func init() {
	rotateCmd.Flags().IntVarP(&agentPort, "port", "p", 0, "Agent port (default: 2240)")
	rotateCmd.Flags().StringVar(&rotateFingerprint, "fingerprint", "", "Optional fingerprint/hash of the new secret value")
	rotateCmd.Flags().StringVar(&rotateValue, "value", "", "New secret value (skips interactive prompt)")
}
