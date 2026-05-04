package deploy

import (
	"fmt"
	"strings"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

var (
	psLimit  int
	psOffset int
)

var PsCmd = &cobra.Command{
	Use:   "ps",
	Short: "List live deployments",
	Long: `List all deployed applications managed by the control plane.

Shows deployment slug, state, source, and age. Use --exposures to also
print the public URLs for each deployment.

Examples:
  ufctl deploy ps
  ufctl deploy ps --exposures
  ufctl deploy ps --limit 50`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		showExposures, _ := cmd.Flags().GetBool("exposures")

		resp, err := deps.CPlaneClient.Deployments.ListDeployments(ctx, psLimit, psOffset)
		if err != nil {
			deps.Printer.PrintError("Failed to list deployments: " + err.Error())
			return err
		}

		if len(resp.Results) == 0 {
			deps.Printer.PrintInfo("No deployments found")
			return nil
		}

		title := fmt.Sprintf("Deployments (%d of %d)", len(resp.Results), resp.TotalCount)
		table := ui.NewTableBuilder().
			WithTitle(title).
			WithHeaders("SLUG", "STATE", "STATUS", "SOURCE", "UPDATED")

		for _, dep := range resp.Results {
			source := "-"
			if dep.Source != nil {
				source = fmt.Sprintf("%s/%s", dep.Source.Owner, dep.Source.Repo)
				if dep.Source.Ref != "" {
					source += "@" + dep.Source.Ref
				}
			}

			updated := dep.UpdatedAt
			if len(updated) > 19 {
				updated = updated[:19]
			}

			table.AddRow(dep.Slug, dep.State, dep.Status, source, updated)
		}

		if err := deps.Printer.PrintTable(table); err != nil {
			return err
		}

		if showExposures {
			for _, dep := range resp.Results {
				links, expErr := deps.CPlaneClient.Links.QueryLinks(ctx, "exposure", "", "", dep.ID, "", 50, 0)
				if expErr != nil || links == nil || len(links.Links) == 0 {
					continue
				}
				deps.Printer.Print(fmt.Sprintf("\n  %s URLs:", dep.Slug))
				printLinkList(deps, links.Links)
			}
		}

		return nil
	},
}

func init() {
	PsCmd.Flags().IntVar(&psLimit, "limit", 50, "Maximum number of deployments to return")
	PsCmd.Flags().IntVar(&psOffset, "offset", 0, "Offset for pagination")
	PsCmd.Flags().Bool("exposures", false, "Also show public URLs for each deployment")
}

func printLinkList(deps *utils.DependencyManager, links []*controlplane.LinkRecord) {
	for _, link := range links {
		state := strings.ToLower(link.State + "/" + link.Status)
		if link.PublicURL != "" {
			deps.Printer.Print(fmt.Sprintf("    %-12s %s  [%s]", link.Spec.ServiceName, link.PublicURL, state))
		} else {
			deps.Printer.Print(fmt.Sprintf("    %-12s (no URL — %s)", link.Spec.ServiceName, state))
		}
	}
}
