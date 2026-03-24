package secrets

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var statusOutputFormat string

var statusCmd = &cobra.Command{
	Use:   "status <name>",
	Short: "Show replication status for a secret",
	Long: `Show control plane metadata and replication status for a secret.

The output lists every cluster that is registered as a replication target,
along with the sync status and the version each target has acknowledged.

  ufctl secrets status DATABASE_PASSWORD`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		printer := ui.GetPrinter(ctx)
		name := args[0]

		deps := utils.NewDependencyManager(ctx)
		listResp, err := deps.CPlaneClient.Secrets.ListSecretMetadata(ctx, name, "", "", 10, 0)
		if err != nil {
			return fmt.Errorf("failed to query control plane: %w", err)
		}

		var found *controlplane.SecretMetadata
		for i := range listResp.Results {
			if listResp.Results[i].Name == name {
				found = &listResp.Results[i]
				break
			}
		}

		if found == nil {
			return fmt.Errorf("secret %q not found in control plane", name)
		}

		if statusOutputFormat == "json" {
			out, err := json.Marshal(found)
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			printer.Print(string(out))
			return nil
		}

		headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
		labelStyle := lipgloss.NewStyle().Bold(true)
		valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("7"))

		printer.Print(headerStyle.Render(fmt.Sprintf("Secret: %s", found.Name)))
		printer.Print(fmt.Sprintf("%s %s", labelStyle.Render("ID:"), valueStyle.Render(found.ID)))
		printer.Print(fmt.Sprintf("%s %s", labelStyle.Render("Scope:"), valueStyle.Render(found.Scope)))
		printer.Print(fmt.Sprintf("%s %s", labelStyle.Render("State:"), valueStyle.Render(stateColor(found.State))))
		printer.Print(fmt.Sprintf("%s %s", labelStyle.Render("Origin Cluster:"), valueStyle.Render(found.OriginClusterID)))
		printer.Print(fmt.Sprintf("%s %d", labelStyle.Render("Version:"), found.CurrentVersion))
		printer.Print(fmt.Sprintf("%s %s", labelStyle.Render("Created:"), valueStyle.Render(found.CreatedAt)))
		printer.Print(fmt.Sprintf("%s %s", labelStyle.Render("Updated:"), valueStyle.Render(found.UpdatedAt)))

		if len(found.ReplicationTargets) == 0 {
			printer.Print("\nNo replication targets.")
			return nil
		}

		printer.Print("\nReplication Targets:")
		colWidths := []int{40, 12, 8, 26}
		headers := []string{"CLUSTER ID", "STATUS", "VERSION", "LAST SYNCED"}
		headerRow := formatRow(headers, colWidths)
		separator := strings.Repeat("─", 90)
		printer.Print(headerStyle.Render(headerRow))
		printer.Print(separator)

		for _, t := range found.ReplicationTargets {
			lastSync := "—"
			if t.LastSyncedAt != nil {
				lastSync = *t.LastSyncedAt
			}
			row := formatRow([]string{
				t.ClusterID,
				stateColor(t.Status),
				fmt.Sprintf("%d", t.SyncedVersion),
				lastSync,
			}, colWidths)
			printer.Print(row)
		}

		return nil
	},
}

func stateColor(state string) string {
	switch strings.ToLower(state) {
	case "active":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(state)
	case "synced":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(state)
	case "rotating":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(state)
	case "pending":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(state)
	case "revoked", "failed":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(state)
	default:
		return state
	}
}

func init() {
	statusCmd.Flags().StringVarP(&statusOutputFormat, "output", "o", "", "Output format: 'json' for machine-readable output")
}

func formatRow(cols []string, widths []int) string {
	var sb strings.Builder
	for i, col := range cols {
		if i < len(widths) {
			sb.WriteString(fmt.Sprintf("%-*s", widths[i], truncate(col, widths[i]-1)))
		} else {
			sb.WriteString(col)
		}
	}
	return sb.String()
}
