package tunnel

import (
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
)

var (
	listServerID string
	listStatus   string
	listLimit    int
	listOffset   int
	listOutput   string
)

// ListCmd lists tunnels, optionally filtered by server or status.
var ListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tunnels",
	Long:  `List tunnels for the authenticated organization.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		if listOutput != "json" {
			deps.Printer.Print(lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("86")).
				Render("\n🔗 Listing tunnels...\n"))
		}

		resp, err := deps.CPlaneClient.Tunnels.QueryTunnels(ctx, listServerID, listStatus, listLimit, listOffset)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to list tunnels: %v", err))
			return err
		}

		if len(resp.Tunnels) == 0 {
			deps.Printer.PrintInfo("No tunnels found")
			return nil
		}

		if listOutput == "json" {
			jsonBytes, _ := json.MarshalIndent(resp.Tunnels, "", "  ")
			deps.Printer.Print(string(jsonBytes))
			return nil
		}

		deps.Printer.Print(fmt.Sprintf("%-20s %-6s %-22s %-8s %s", "ID", "TYPE", "TARGET", "STATUS", "PUBLIC URL"))
		deps.Printer.Print("────────────────────────────────────────────────────────────────────────────────────────────")
		for _, t := range resp.Tunnels {
			publicURL := t.PublicURL
			if publicURL == "" {
				publicURL = "-"
			}
			deps.Printer.Print(fmt.Sprintf("%-20s %-6s %-22s %-8s %s",
				truncate(t.ID, 18),
				t.TargetType,
				truncate(t.Target, 20),
				t.Status,
				publicURL,
			))
		}
		deps.Printer.Print(fmt.Sprintf("\nTotal: %d", resp.Total))
		return nil
	},
}

func init() {
	ListCmd.Flags().StringVar(&listServerID, "server", "", "Filter by server ID")
	ListCmd.Flags().StringVar(&listStatus, "status", "", "Filter by status (pending/bound/error/closed)")
	ListCmd.Flags().IntVar(&listLimit, "limit", 20, "Max number of results")
	ListCmd.Flags().IntVar(&listOffset, "offset", 0, "Results offset")
	ListCmd.Flags().StringVarP(&listOutput, "output", "o", "table", "Output format: table|json")
}

// truncate shortens s to at most n runes, appending ".." if truncated.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-2] + ".."
}
