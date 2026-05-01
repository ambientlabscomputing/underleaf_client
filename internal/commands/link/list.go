package link

import (
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
)

var (
	listKind       string
	listVisibility string
	listStatus     string
	listServerID   string
	listLimit      int
	listOffset     int
	listOutput     string
)

// ListCmd lists links across all kinds with optional filters.
var ListCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List links across exposures, tunnels, and channels",
	Long: `List links — a unified projection over exposures, tunnels, and channels.

Filter by kind (exposure|tunnel|channel), visibility (public|token|peer),
status (in_progress|success|failure), or server ID.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		if listOutput != "json" {
			deps.Printer.Print(lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("86")).
				Render("\n🔗 Listing links...\n"))
		}

		resp, err := deps.CPlaneClient.Links.QueryLinks(
			ctx, listKind, listVisibility, listStatus, listServerID, listLimit, listOffset,
		)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to list links: %v", err))
			return err
		}

		if listOutput == "json" {
			jsonBytes, _ := json.MarshalIndent(resp, "", "  ")
			deps.Printer.Print(string(jsonBytes))
			return nil
		}

		if len(resp.Links) == 0 {
			deps.Printer.PrintInfo("No links found")
			return nil
		}

		deps.Printer.Print(fmt.Sprintf("%-20s %-9s %-10s %-22s %-12s %s",
			"ID", "KIND", "VISIBILITY", "TARGET", "STATUS", "URL/PEER"))
		deps.Printer.Print("──────────────────────────────────────────────────────────────────────────────────────────────────")
		for _, l := range resp.Links {
			urlOrPeer := l.URLOrPeer
			if urlOrPeer == "" {
				urlOrPeer = "-"
			}
			deps.Printer.Print(fmt.Sprintf("%-20s %-9s %-10s %-22s %-12s %s",
				truncate(l.ID, 18),
				l.Kind,
				l.Visibility,
				truncate(l.Target, 20),
				l.Status,
				urlOrPeer,
			))
		}
		deps.Printer.Print(fmt.Sprintf("\nTotal: %d  (exposure=%d  tunnel=%d  channel=%d)",
			resp.Total,
			resp.CountsByKind["exposure"],
			resp.CountsByKind["tunnel"],
			resp.CountsByKind["channel"],
		))
		return nil
	},
}

func init() {
	ListCmd.Flags().StringVar(&listKind, "kind", "", "Filter by kind (exposure|tunnel|channel)")
	ListCmd.Flags().StringVar(&listVisibility, "visibility", "", "Filter by visibility (public|token|peer)")
	ListCmd.Flags().StringVar(&listStatus, "status", "", "Filter by status (in_progress|success|failure)")
	ListCmd.Flags().StringVar(&listServerID, "server", "", "Filter by server ID")
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
