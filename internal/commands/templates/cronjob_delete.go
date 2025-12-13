package templates

import (
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var CronjobDeleteCmd = &cobra.Command{
	Use:   "delete <cronjob-id>",
	Short: "Delete a cron job",
	Long: `Delete a cron job.

Examples:
  ufctl templates cronjobs delete abc123
  ufctl templates cronjobs delete abc123 --force`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		cronjobID := args[0]
		force, _ := cmd.Flags().GetBool("force")

		return deleteCronJob(deps, cronjobID, force)
	},
}

func deleteCronJob(deps *utils.DependencyManager, cronjobID string, force bool) error {
	// If not force, confirm
	if !force {
		deps.Printer.Print("")
		deps.Printer.PrintWarning(fmt.Sprintf("This will permanently delete cron job '%s'", cronjobID))
		deps.Printer.Print("")
		deps.Printer.Print("Use --force to skip confirmation")
		return fmt.Errorf("deletion cancelled - use --force to confirm")
	}

	var response map[string]interface{}
	err := deps.CPlaneClient.API().DELETE("/cron-jobs/"+cronjobID, &response)
	if err != nil {
		deps.Printer.PrintError("Failed to delete cron job: " + err.Error())
		return err
	}

	successStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))
	deps.Printer.Print("")
	deps.Printer.Print(successStyle.Render("✓ Cron job deleted successfully"))
	deps.Printer.Print("")

	return nil
}

func init() {
	CronjobDeleteCmd.Flags().BoolP("force", "f", false, "Skip confirmation prompt")
	CronjobsCmd.AddCommand(CronjobDeleteCmd)
}
