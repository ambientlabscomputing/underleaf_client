package channel

import (
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
)

var (
	createSource  string
	createDest    string
	createPurpose string
	createTTL     int
	createOutput  string
)

// CreateCmd creates a relay channel between two servers.
var CreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a relay channel between two servers",
	Long: `Create a server-to-server relay channel via the Hyphae gateway.

Both --source and --dest are required. The source server initiates traffic;
the destination server listens. The response includes a one-time grant JWT
that is passed to the initiator — it is never stored and cannot be retrieved later.

The channel bind is asynchronous: both agents receive a Spine event and connect
to Hyphae in the background. Use 'ufctl channel get <id>' to check status.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if createSource == "" || createDest == "" {
			return fmt.Errorf("both --source and --dest are required")
		}

		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		if createOutput != "json" {
			deps.Printer.Print(lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("86")).
				Render("\n🔗 Creating channel...\n"))
		}

		req := controlplane.CreateChannelRequest{
			SourceServerID: createSource,
			DestServerID:   createDest,
			Purpose:        createPurpose,
			TTLSeconds:     createTTL,
		}

		resp, err := deps.CPlaneClient.Channels.CreateChannel(ctx, req)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to create channel: %v", err))
			return err
		}

		if createOutput == "json" {
			// Flatten for CLI output: promote channel fields to top level.
			flat := map[string]interface{}{}
			if resp.Channel != nil {
				flat["id"] = resp.Channel.ID
				flat["org_id"] = resp.Channel.OrgID
				flat["source_server_id"] = resp.Channel.SourceServerID
				flat["dest_server_id"] = resp.Channel.DestServerID
				flat["purpose"] = resp.Channel.Purpose
				flat["status"] = resp.Channel.Status
				flat["error_message"] = resp.Channel.ErrorMessage
				flat["created_at"] = resp.Channel.CreatedAt
				flat["updated_at"] = resp.Channel.UpdatedAt
				flat["expires_at"] = resp.Channel.ExpiresAt
			}
			flat["grant"] = resp.Grant
			jsonBytes, _ := json.MarshalIndent(flat, "", "  ")
			deps.Printer.Print(string(jsonBytes))
			return nil
		}

		ch := resp.Channel
		if ch == nil {
			deps.Printer.PrintError("Channel created but no record returned")
			return fmt.Errorf("channel record is nil")
		}
		deps.Printer.PrintSuccess("✅ Channel created (agents are binding in background):")
		deps.Printer.Print(fmt.Sprintf("   ID:      %s", ch.ID))
		deps.Printer.Print(fmt.Sprintf("   Source:  %s", ch.SourceServerID))
		deps.Printer.Print(fmt.Sprintf("   Dest:    %s", ch.DestServerID))
		if ch.Purpose != "" {
			deps.Printer.Print(fmt.Sprintf("   Purpose: %s", ch.Purpose))
		}
		deps.Printer.Print(fmt.Sprintf("   Status:  %s", ch.Status))
		deps.Printer.Print(fmt.Sprintf("   Expires: %s", ch.ExpiresAt))
		if resp.Grant != "" {
			deps.Printer.Print(fmt.Sprintf("\n   Grant:   %s", resp.Grant))
			deps.Printer.Print("   ⚠️  Save the grant JWT — it will not be shown again.")
		}
		deps.Printer.Print(fmt.Sprintf("\nCheck status with: ufctl channel get %s", ch.ID))
		return nil
	},
}

func init() {
	CreateCmd.Flags().StringVar(&createSource, "source", "", "Source server ID (initiator)")
	CreateCmd.Flags().StringVar(&createDest, "dest", "", "Destination server ID (listener)")
	CreateCmd.Flags().StringVar(&createPurpose, "purpose", "", "Human-readable purpose label (optional)")
	CreateCmd.Flags().IntVar(&createTTL, "ttl", 0, "TTL in seconds (default: 300, max: 3600)")
	CreateCmd.Flags().StringVarP(&createOutput, "output", "o", "table", "Output format: table|json")
}
