package templates

import (
	"context"
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var CronjobListCmd = &cobra.Command{
	Use:   "list",
	Short: "List cron jobs",
	Long: `List all scheduled cron jobs.

Examples:
  ufctl templates cronjobs list
  ufctl templates cronjobs list --limit 20
  ufctl templates cronjobs list --enabled`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		limit, _ := cmd.Flags().GetInt("limit")
		enabledOnly, _ := cmd.Flags().GetBool("enabled")

		return listCronJobs(deps, limit, enabledOnly)
	},
}

type CronJob struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	TemplateID          string            `json:"template_id"`
	TemplateName        string            `json:"template_name,omitempty"`
	SetInputs           map[string]string `json:"set_inputs,omitempty"`
	Schedule            CronSchedule      `json:"schedule"`
	Enabled             bool              `json:"enabled"`
	NextRun             string            `json:"next_run,omitempty"`
	LastRun             string            `json:"last_run,omitempty"`
	LastJobID           string            `json:"last_job_id,omitempty"`
	LastError           string            `json:"last_error,omitempty"`
	ConsecutiveFailures int               `json:"consecutive_failures"`
	CreatedAt           string            `json:"created_at"`
	UpdatedAt           string            `json:"updated_at"`
}

type CronSchedule struct {
	Minute     string `json:"minute" yaml:"minute"`
	Hour       string `json:"hour" yaml:"hour"`
	DayOfMonth string `json:"day_of_month" yaml:"day_of_month"`
	Month      string `json:"month" yaml:"month"`
	DayOfWeek  string `json:"day_of_week" yaml:"day_of_week"`
}

type QueryCronJobsResponse struct {
	Results    []CronJob `json:"results"`
	TotalCount int       `json:"total_count"`
}

func listCronJobs(deps *utils.DependencyManager, limit int, enabledOnly bool) error {
	// Build query params
	path := fmt.Sprintf("/cron-jobs?limit=%d", limit)
	if enabledOnly {
		path += "&enabled=true"
	}

	var response QueryCronJobsResponse
	err := deps.CPlaneClient.API().GET(context.Background(), path, &response)
	if err != nil {
		deps.Printer.PrintError("Failed to list cron jobs: " + err.Error())
		return err
	}

	if len(response.Results) == 0 {
		deps.Printer.PrintInfo("No cron jobs found")
		return nil
	}

	enabledStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	disabledStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	// Build table
	table := ui.NewTableBuilder().
		WithTitle(fmt.Sprintf("Cron Jobs (showing %d of %d)", len(response.Results), response.TotalCount)).
		WithHeaders("ID", "Name", "Template", "Schedule", "Enabled", "Next Run", "Last Run")

	for _, cj := range response.Results {
		// Format schedule
		schedule := formatSchedule(cj.Schedule)

		// Format enabled status
		enabledStr := disabledStyle.Render("No")
		if cj.Enabled {
			enabledStr = enabledStyle.Render("Yes")
		}

		// Format next run
		nextRun := formatTime(cj.NextRun)
		if !cj.Enabled {
			nextRun = disabledStyle.Render("-")
		}

		// Format last run
		lastRun := formatTime(cj.LastRun)
		if lastRun == "" {
			lastRun = disabledStyle.Render("Never")
		}

		templateName := cj.TemplateName
		if templateName == "" {
			templateName = cj.TemplateID
		}

		table.AddRow(cj.ID, cj.Name, templateName, schedule, enabledStr, nextRun, lastRun)
	}

	deps.Printer.Print(table.Render())
	deps.Printer.Print("")
	deps.Printer.PrintInfo("Use 'ufctl templates cronjobs get <id>' to view details")

	return nil
}

func formatSchedule(s CronSchedule) string {
	// Convert to cron-like string
	return fmt.Sprintf("%s %s %s %s %s",
		s.Minute, s.Hour, s.DayOfMonth, s.Month, s.DayOfWeek)
}

func formatTime(t string) string {
	if t == "" {
		return ""
	}
	if len(t) > 16 {
		return t[:16] // YYYY-MM-DDTHH:MM
	}
	return t
}

func init() {
	CronjobListCmd.Flags().IntP("limit", "l", 20, "Number of cron jobs to show")
	CronjobListCmd.Flags().Bool("enabled", false, "Show only enabled cron jobs")
	CronjobsCmd.AddCommand(CronjobListCmd)
}
