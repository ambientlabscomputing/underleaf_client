package templates

import (
	"context"
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var CronjobEnableCmd = &cobra.Command{
	Use:   "enable <cronjob-id>",
	Short: "Enable a cron job",
	Long: `Enable a disabled cron job to resume scheduled execution.

Examples:
  ufctl templates cronjobs enable abc123`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		cronjobID := args[0]
		return toggleCronJob(deps, cronjobID, true)
	},
}

var CronjobDisableCmd = &cobra.Command{
	Use:   "disable <cronjob-id>",
	Short: "Disable a cron job",
	Long: `Disable a cron job to stop scheduled execution.

Examples:
  ufctl templates cronjobs disable abc123`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		cronjobID := args[0]
		return toggleCronJob(deps, cronjobID, false)
	},
}

func toggleCronJob(deps *utils.DependencyManager, cronjobID string, enabled bool) error {
	payload := map[string]interface{}{
		"enabled": enabled,
	}

	var response CronJob
	err := deps.CPlaneClient.API().PATCH(context.Background(), "/cron-jobs/"+cronjobID, payload, &response)
	if err != nil {
		deps.Printer.PrintError("Failed to update cron job: " + err.Error())
		return err
	}

	successStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))

	deps.Printer.Print("")
	if enabled {
		deps.Printer.Print(successStyle.Render("✓ Cron job enabled"))
		if response.NextRun != "" {
			deps.Printer.Print(fmt.Sprintf("  Next run: %s", response.NextRun))
		}
	} else {
		deps.Printer.Print(successStyle.Render("✓ Cron job disabled"))
	}
	deps.Printer.Print("")

	return nil
}

func init() {
	CronjobsCmd.AddCommand(CronjobEnableCmd)
	CronjobsCmd.AddCommand(CronjobDisableCmd)
}
