package channel

import (
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
)

var (
	listSource string
	listDest   string
	listStatus string
	listLimit  int
	listOffset int
	listOutput string
)

// ListCmd lists channels, optionally filtered by server or status.
var ListCmd = &cobra.Command{
	Use:   "list",
	Short: "List relay channels",
	Long:  `List relay channels for the authenticated organization.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		if listOutput != "json" {
			deps.Printer.Print(lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("86")).
				Render("\n🔗 Listing channels...\n"))
		}

		resp, err := deps.CPlaneClient.Channels.QueryChannels(ctx, listSource, listDest, listStatus, listLimit, listOffset)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to list channels: %v", err))
			return err
		}

		if len(resp.Channels) == 0 {
			deps.Printer.PrintInfo("No channels found")
			return nil
		}

		if listOutput == "json" {
			jsonBytes, _ := json.MarshalIndent(resp.Channels, "", "  ")
			deps.Printer.Print(string(jsonBytes))
			return nil
		}

		deps.Printer.Print(fmt.Sprintf("%-20s %-20s %-20s %-8s %s", "ID", "SOURCE", "DEST", "STATUS", "PURPOSE"))
		deps.Printer.Print("─────────────────────────────────────────────────────────────────────────────────────────")
		for _, ch := range resp.Channels {
			purpose := ch.Purpose
			if purpose == "" {
				purpose = "-"
			}
			deps.Printer.Print(fmt.Sprintf("%-20s %-20s %-20s %-8s %s",
				truncate(ch.ID, 18),
				truncate(ch.SourceServerID, 18),
				truncate(ch.DestServerID, 18),
				ch.Status,
				purpose,
			))
		}
		deps.Printer.Print(fmt.Sprintf("\nTotal: %d", resp.TotalCount))
		return nil
	},
}

func init() {
	ListCmd.Flags().StringVar(&listSource, "source", "", "Filter by source server ID")
	ListCmd.Flags().StringVar(&listDest, "dest", "", "Filter by destination server ID")
	ListCmd.Flags().StringVar(&listStatus, "status", "", "Filter by status (pending/ready/active/closed/error)")
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
