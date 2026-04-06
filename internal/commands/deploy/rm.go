package deploy

import (
	"fmt"
	"strings"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

var rmForce bool

var RmCmd = &cobra.Command{
	Use:   "rm <id|slug> [<id|slug>...]",
	Short: "Delete a deployment",
	Long: `Delete one or more deployments by ID or slug. Cascades to exposures and
Hyphae leases — public URLs are immediately revoked.

Examples:
  ufctl deploy rm n8n
  ufctl deploy rm n8n hello-world
  ufctl deploy rm f503ebcf-539b-47ef-a08b-61837d6225c0 --force`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)
		printer := ui.GetPrinter(ctx)

		// Resolve each arg to a deployment ID (GET /deployments/<id> works for UUIDs;
		// for slugs we fall back to listing and matching).
		type resolved struct {
			id   string
			slug string
		}
		var targets []resolved

		for _, arg := range args {
			dep, err := deps.CPlaneClient.Deployments.GetDeployment(ctx, arg)
			if err != nil {
				// Try listing to match by slug
				resp, listErr := deps.CPlaneClient.Deployments.ListDeployments(ctx, 200, 0)
				if listErr != nil {
					printer.PrintError(fmt.Sprintf("Cannot resolve %q: %v", arg, err))
					return err
				}
				found := false
				for _, d := range resp.Results {
					if strings.EqualFold(d.Slug, arg) {
						targets = append(targets, resolved{id: d.ID, slug: d.Slug})
						found = true
						break
					}
				}
				if !found {
					printer.PrintError(fmt.Sprintf("Deployment %q not found", arg))
					return fmt.Errorf("deployment %q not found", arg)
				}
			} else {
				targets = append(targets, resolved{id: dep.ID, slug: dep.Slug})
			}
		}

		if !rmForce {
			names := make([]string, len(targets))
			for i, t := range targets {
				names[i] = t.slug
			}
			confirmed, err := ui.Confirm(fmt.Sprintf(
				"Delete %s? This will revoke all public URLs.", strings.Join(names, ", ")))
			if err != nil {
				return err
			}
			if !confirmed {
				printer.Print("Aborted.")
				return nil
			}
		}

		anyErr := false
		for _, t := range targets {
			if err := deps.CPlaneClient.Deployments.DeleteDeployment(ctx, t.id); err != nil {
				printer.PrintError(fmt.Sprintf("Failed to delete %s: %v", t.slug, err))
				anyErr = true
				continue
			}
			printer.PrintSuccess(fmt.Sprintf("Deleted deployment: %s", t.slug))
		}

		if anyErr {
			return fmt.Errorf("one or more deletions failed")
		}
		return nil
	},
}

func init() {
	RmCmd.Flags().BoolVar(&rmForce, "force", false, "Skip confirmation prompt")
}
