package link

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
)

var (
	listKind       string
	listVisibility string
	listStatus     string
	listServerID   string
	listLimit      int
	listOffset     int
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

		if deps.Printer.Format() == ui.FormatHuman {
			deps.Printer.PrintInfo("Listing links...")
		}

		resp, err := deps.CPlaneClient.Links.QueryLinks(
			ctx, listKind, listVisibility, listStatus, listServerID, listLimit, listOffset,
		)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to list links: %v", err))
			return err
		}

		if deps.Printer.Format() == ui.FormatJSON {
			return deps.Printer.Print(resp)
		}

		if len(resp.Links) == 0 {
			if deps.Printer.Format() == ui.FormatShell {
				return nil
			}
			deps.Printer.PrintInfo("No links found")
			return nil
		}

		table := ui.NewTableBuilder().
			WithTitle("Links").
			WithHeaders("ID", "Kind", "Visibility", "Target", "State", "Status", "URL/Peer")
		for _, l := range resp.Links {
			urlOrPeer := l.URLOrPeer
			if urlOrPeer == "" {
				urlOrPeer = "-"
			}
			state := l.State
			if state == "" {
				state = "-"
			}
			status := l.Status
			if status == "" {
				status = "-"
			}
			table.AddRow(
				truncate(l.ID, 18),
				l.Kind,
				l.Visibility,
				truncate(l.Target, 20),
				state,
				status,
				urlOrPeer,
			)
		}

		if err := deps.Printer.PrintTable(table); err != nil {
			return err
		}
		if deps.Printer.Format() == ui.FormatHuman {
			deps.Printer.Print(fmt.Sprintf("\nTotal: %d  (exposure=%d  tunnel=%d  channel=%d)",
				resp.Total,
				resp.CountsByKind["exposure"],
				resp.CountsByKind["tunnel"],
				resp.CountsByKind["channel"],
			))
		}
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
}

// truncate shortens s to at most n runes, appending ".." if truncated.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-2] + ".."
}
