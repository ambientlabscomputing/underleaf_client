package templates

import (
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var CronjobGetCmd = &cobra.Command{
	Use:   "get <cronjob-id>",
	Short: "Get details of a cron job",
	Long: `Get detailed information about a cron job.

Examples:
  ufctl templates cronjobs get abc123`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		cronjobID := args[0]
		return getCronJob(deps, cronjobID)
	},
}

func getCronJob(deps *utils.DependencyManager, cronjobID string) error {
	var cj CronJob
	err := deps.CPlaneClient.API().GET("/cron-jobs/"+cronjobID, &cj)
	if err != nil {
		deps.Printer.PrintError("Failed to get cron job: " + err.Error())
		return err
	}

	// Styles
	boldStyle := lipgloss.NewStyle().Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1"))

	deps.Printer.Print("")
	deps.Printer.Print(headerStyle.Render("Cron Job Details"))
	deps.Printer.Print("")

	// Basic info
	deps.Printer.Print(fmt.Sprintf("  %s %s", boldStyle.Render("ID:"), cj.ID))
	deps.Printer.Print(fmt.Sprintf("  %s %s", boldStyle.Render("Name:"), cj.Name))

	// Status
	enabledStr := errorStyle.Render("Disabled")
	if cj.Enabled {
		enabledStr = successStyle.Render("Enabled")
	}
	deps.Printer.Print(fmt.Sprintf("  %s %s", boldStyle.Render("Status:"), enabledStr))
	deps.Printer.Print("")

	// Template
	templateName := cj.TemplateName
	if templateName == "" {
		templateName = cj.TemplateID
	}
	deps.Printer.Print(fmt.Sprintf("  %s %s", boldStyle.Render("Template:"), templateName))
	deps.Printer.Print(fmt.Sprintf("  %s %s", boldStyle.Render("Template ID:"), cj.TemplateID))
	deps.Printer.Print("")

	// Schedule
	deps.Printer.Print(fmt.Sprintf("  %s %s", boldStyle.Render("Schedule:"), formatSchedule(cj.Schedule)))
	deps.Printer.Print(fmt.Sprintf("    %s", dimStyle.Render("(minute hour day-of-month month day-of-week)")))
	deps.Printer.Print("")

	// Preset inputs
	if len(cj.SetInputs) > 0 {
		deps.Printer.Print(fmt.Sprintf("  %s", boldStyle.Render("Preset Inputs:")))
		for k, v := range cj.SetInputs {
			deps.Printer.Print(fmt.Sprintf("    %s = %s", k, v))
		}
		deps.Printer.Print("")
	}

	// Execution info
	deps.Printer.Print(fmt.Sprintf("  %s", boldStyle.Render("Execution:")))
	if cj.NextRun != "" && cj.Enabled {
		deps.Printer.Print(fmt.Sprintf("    Next Run: %s", cj.NextRun))
	}
	if cj.LastRun != "" {
		deps.Printer.Print(fmt.Sprintf("    Last Run: %s", cj.LastRun))
	}
	if cj.LastJobID != "" {
		deps.Printer.Print(fmt.Sprintf("    Last Job: %s", cj.LastJobID))
	}
	if cj.LastError != "" {
		deps.Printer.Print(fmt.Sprintf("    Last Error: %s", errorStyle.Render(cj.LastError)))
	}
	if cj.ConsecutiveFailures > 0 {
		deps.Printer.Print(fmt.Sprintf("    Consecutive Failures: %s", errorStyle.Render(fmt.Sprintf("%d", cj.ConsecutiveFailures))))
	}
	deps.Printer.Print("")

	// Timestamps
	deps.Printer.Print(fmt.Sprintf("  %s %s", dimStyle.Render("Created:"), cj.CreatedAt))
	deps.Printer.Print(fmt.Sprintf("  %s %s", dimStyle.Render("Updated:"), cj.UpdatedAt))
	deps.Printer.Print("")

	// Actions hint
	if cj.Enabled {
		deps.Printer.PrintInfo("Use 'ufctl templates cronjobs disable " + cj.ID + "' to disable")
	} else {
		deps.Printer.PrintInfo("Use 'ufctl templates cronjobs enable " + cj.ID + "' to enable")
	}

	return nil
}

func init() {
	CronjobsCmd.AddCommand(CronjobGetCmd)
}
