package link

import (
	"encoding/json"
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

var (
	listKind       string
	listVisibility string
	listServerID   string
	listDeployment string
	listStatus     string
	listLimit      int
	listOffset     int
)

var ListCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List Rhizo links",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		resp, err := deps.CPlaneClient.Links.QueryLinks(ctx, listKind, listVisibility, listServerID, listDeployment, listStatus, listLimit, listOffset)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to list links: %v", err))
			return err
		}

		if deps.Printer.Format() == ui.FormatJSON {
			jsonBytes, _ := json.MarshalIndent(resp.Links, "", "  ")
			deps.Printer.Print(string(jsonBytes))
			return nil
		}

		if len(resp.Links) == 0 {
			deps.Printer.PrintInfo("No links found")
			return nil
		}

		deps.Printer.Print(fmt.Sprintf("%-20s %-9s %-7s %-12s %-8s %s", "ID", "KIND", "VIS", "TARGET", "STATUS", "PUBLIC URL"))
		deps.Printer.Print("────────────────────────────────────────────────────────────────────────────────────────")
		for _, link := range resp.Links {
			deps.Printer.Print(fmt.Sprintf("%-20s %-9s %-7s %-12s %-8s %s",
				truncate(link.ID, 18),
				link.Kind,
				link.Visibility,
				truncate(linkTarget(link), 10),
				link.Status,
				emptyDash(link.PublicURL),
			))
		}
		deps.Printer.Print(fmt.Sprintf("\nTotal: %d", resp.Total))
		return nil
	},
}

func init() {
	ListCmd.Flags().StringVar(&listKind, "kind", "", "Filter by kind: exposure, tunnel, channel")
	ListCmd.Flags().StringVar(&listVisibility, "visibility", "", "Filter by visibility: public, token, peer")
	ListCmd.Flags().StringVar(&listServerID, "server", "", "Filter by server ID")
	ListCmd.Flags().StringVar(&listDeployment, "deployment", "", "Filter by deployment ID")
	ListCmd.Flags().StringVar(&listStatus, "status", "", "Filter by status")
	ListCmd.Flags().IntVar(&listLimit, "limit", 20, "Max number of results")
	ListCmd.Flags().IntVar(&listOffset, "offset", 0, "Results offset")
}

func linkTarget(link *controlplane.LinkRecord) string {
	switch link.Kind {
	case "exposure":
		if link.Spec.ServiceName != "" {
			return fmt.Sprintf("%s:%d", link.Spec.ServiceName, link.Spec.TargetPort)
		}
	case "tunnel":
		return link.Spec.Target
	case "channel":
		return link.Spec.DestServerID
	}
	return link.ServerID
}

func emptyDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 2 {
		return s[:n]
	}
	return s[:n-2] + ".."
}
