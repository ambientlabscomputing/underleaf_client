package secrets

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	listScope        string
	listState        string
	listName         string
	listOutputFormat string
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List secrets from the control plane",
	Long:  `Display a table of secret metadata records registered with the control plane.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		printer := ui.GetPrinter(ctx)
		deps := utils.NewDependencyManager(ctx)

		resp, err := deps.CPlaneClient.Secrets.ListSecretMetadata(ctx, listName, listScope, listState, 100, 0)
		if err != nil {
			return fmt.Errorf("failed to list secrets: %w", err)
		}

		if len(resp.Results) == 0 {
			if listOutputFormat == "json" {
				fmt.Println("[]")
				return nil
			}
			printer.Print("No secrets found.")
			return nil
		}

		if listOutputFormat == "json" {
			out, err := json.Marshal(resp.Results)
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(out))
			return nil
		}

		headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
		colWidths := []int{24, 9, 9, 9, 20, 8}

		header := fmt.Sprintf("%-*s %-*s %-*s %-*s %-*s %-*s",
			colWidths[0], "NAME",
			colWidths[1], "SCOPE",
			colWidths[2], "VERSION",
			colWidths[3], "STATE",
			colWidths[4], "ORIGIN CLUSTER",
			colWidths[5], "SYNCED",
		)
		printer.Print(headerStyle.Render(header))
		printer.Print(strings.Repeat("─", 85))

		for _, s := range resp.Results {
			synced := "—"
			if s.Scope == "org" && len(s.ReplicationTargets) > 0 {
				acked := 0
				for _, t := range s.ReplicationTargets {
					if t.Status == "acknowledged" {
						acked++
					}
				}
				synced = fmt.Sprintf("%d/%d", acked, len(s.ReplicationTargets))
			}

			row := fmt.Sprintf("%-*s %-*s %-*d %-*s %-*s %-*s",
				colWidths[0], truncate(s.Name, colWidths[0]),
				colWidths[1], s.Scope,
				colWidths[2], s.CurrentVersion,
				colWidths[3], s.State,
				colWidths[4], truncate(s.OriginClusterID, colWidths[4]),
				colWidths[5], synced,
			)
			printer.Print(row)
		}

		return nil
	},
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

func init() {
	listCmd.Flags().StringVar(&listScope, "scope", "", "Filter by scope (org|cluster)")
	listCmd.Flags().StringVar(&listState, "state", "", "Filter by state (active|rotating|revoked)")
	listCmd.Flags().StringVar(&listName, "name", "", "Filter by name (partial match)")
	listCmd.Flags().StringVarP(&listOutputFormat, "output", "o", "", "Output format: 'json' for machine-readable output")
}
