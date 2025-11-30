package jobs

import (
	"context"
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

var ListCmd = &cobra.Command{
	Use:   "list",
	Short: "List recent jobs",
	Long: `List recent command execution jobs.

Examples:
  ufctl jobs list
  ufctl jobs list --limit 20
  ufctl jobs list --status running`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		limit, _ := cmd.Flags().GetInt("limit")
		status, _ := cmd.Flags().GetString("status")

		return listJobs(ctx, deps, limit, status)
	},
}

func listJobs(ctx context.Context, deps *utils.DependencyManager, limit int, status string) error {
	// Build query params
	path := fmt.Sprintf("/jobs?limit=%d", limit)
	if status != "" {
		path += fmt.Sprintf("&status=%s", status)
	}

	var response struct {
		Results    []map[string]interface{} `json:"results"`
		TotalCount int                      `json:"total_count"`
	}

	err := deps.CPlaneClient.API().GET(path, &response)
	if err != nil {
		deps.Printer.PrintError("Failed to list jobs: " + err.Error())
		return err
	}

	if len(response.Results) == 0 {
		deps.Printer.PrintInfo("No jobs found")
		return nil
	}

	// Build table
	table := ui.NewTableBuilder().
		WithTitle(fmt.Sprintf("Jobs (showing %d of %d)", len(response.Results), response.TotalCount)).
		WithHeaders("ID", "Type", "Status", "Created", "Command")

	for _, job := range response.Results {
		jobID := getStringField(job, "id")
		jobType := getStringField(job, "type")
		jobStatus := getStringField(job, "status")
		createdAt := getStringField(job, "created_at")

		// Truncate ID for display
		displayID := jobID
		// if len(displayID) > 12 {
		// 	displayID = displayID[:8] + "..."
		// }

		// Extract command from payload
		command := "-"
		if payload, ok := job["payload"].(map[string]interface{}); ok {
			if cmd, ok := payload["command"].(string); ok {
				command = cmd
				if len(command) > 40 {
					command = command[:37] + "..."
				}
			}
		}

		// Format created time
		displayTime := createdAt
		if len(displayTime) > 19 {
			displayTime = displayTime[:19] // YYYY-MM-DDTHH:MM:SS
		}

		table.AddRow(displayID, jobType, formatStatus(jobStatus), displayTime, command)
	}

	deps.Printer.Print(table.Render())
	deps.Printer.Print("")
	deps.Printer.PrintInfo("Use 'ufctl jobs status <job-id>' to view details")

	return nil
}

func init() {
	ListCmd.Flags().IntP("limit", "l", 10, "Number of jobs to show")
	ListCmd.Flags().StringP("status", "s", "", "Filter by status (pending, running, completed, failed)")
	JobsCmd.AddCommand(ListCmd)
}
