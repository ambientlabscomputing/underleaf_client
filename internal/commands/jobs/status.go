package jobs

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var StatusCmd = &cobra.Command{
	Use:   "status [job-id]",
	Short: "Get the status of a job",
	Long: `View the status of a command execution job.

Examples:
  ufctl jobs status abc-123-xyz
  ufctl jobs status abc-123-xyz --watch`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)
		jobID := args[0]

		watch, _ := cmd.Flags().GetBool("watch")

		if watch {
			return watchJobStatus(ctx, deps, jobID)
		}

		return showJobStatus(ctx, deps, jobID)
	},
}

func showJobStatus(ctx context.Context, deps *utils.DependencyManager, jobID string) error {
	// Get job status from API
	job, err := getJob(ctx, deps, jobID)
	if err != nil {
		deps.Printer.PrintError("Failed to get job: " + err.Error())
		return err
	}

	displayJobStatus(deps.Printer, job)
	return nil
}

func watchJobStatus(ctx context.Context, deps *utils.DependencyManager, jobID string) error {
	deps.Printer.Print(fmt.Sprintf("Watching job: %s (Ctrl+C to exit)\n", jobID))

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			job, err := getJob(ctx, deps, jobID)
			if err != nil {
				deps.Printer.PrintError("Failed to get job: " + err.Error())
				return err
			}

			// Clear screen and show status
			fmt.Print("\033[H\033[2J") // Clear screen
			displayJobStatus(deps.Printer, job)

			// If job is completed or failed, stop watching
			status := getStringField(job, "status")
			if status == "completed" || status == "failed" {
				deps.Printer.Print("\n✓ Job finished. Exiting watch mode.")
				return nil
			}
		}
	}
}

func getJob(ctx context.Context, deps *utils.DependencyManager, jobID string) (map[string]interface{}, error) {
	var job map[string]interface{}
	err := deps.CPlaneClient.API().GET("/jobs/"+jobID, &job)
	if err != nil {
		return nil, err
	}
	return job, nil
}

func displayJobStatus(printer ui.Printer, job map[string]interface{}) {
	boldStyle := lipgloss.NewStyle().Bold(true)
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	labelStyle := lipgloss.NewStyle().Width(20)

	jobID := getStringField(job, "id")
	jobType := getStringField(job, "type")
	status := getStringField(job, "status")
	createdAt := getStringField(job, "created_at")

	printer.Print("")
	printer.Print(boldStyle.Render(fmt.Sprintf("Job: %s", jobID)))
	printer.Print(strings.Repeat("─", 70))
	printer.Print("")

	// Basic info
	printer.Print(headerStyle.Render("Job Information"))
	printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("ID:"), jobID))
	printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("Type:"), jobType))
	printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("Status:"), formatStatus(status)))
	printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("Created:"), createdAt))

	// Payload (command details)
	if payload, ok := job["payload"].(map[string]interface{}); ok {
		printer.Print("")
		printer.Print(headerStyle.Render("Command Details"))

		if command, ok := payload["command"].(string); ok {
			printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("Command:"), command))
		}
		if timeout, ok := payload["timeout"].(float64); ok && timeout > 0 {
			printer.Print(fmt.Sprintf("%s%ds", labelStyle.Render("Timeout:"), int(timeout)))
		}
		if envVars, ok := payload["env_vars"].(map[string]interface{}); ok && len(envVars) > 0 {
			printer.Print(fmt.Sprintf("%s%d vars", labelStyle.Render("Environment:"), len(envVars)))
		}

		// Server targeting
		if allServers, ok := payload["all_servers"].(bool); ok && allServers {
			printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("Target:"), "All Servers"))
		} else if serverIDs, ok := payload["server_ids"].([]interface{}); ok && len(serverIDs) > 0 {
			printer.Print(fmt.Sprintf("%s%d server(s)", labelStyle.Render("Target:"), len(serverIDs)))
		} else if tags, ok := payload["tags"].(map[string]interface{}); ok && len(tags) > 0 {
			tagStrs := []string{}
			for k, v := range tags {
				tagStrs = append(tagStrs, fmt.Sprintf("%s=%v", k, v))
			}
			printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("Target:"), strings.Join(tagStrs, ", ")))
		}
	}

	// Server requests (individual server results)
	if serverRequests, ok := job["server_requests"].(map[string]interface{}); ok && len(serverRequests) > 0 {
		printer.Print("")
		printer.Print(headerStyle.Render("Server Results"))

		table := ui.NewTableBuilder().
			WithHeaders("Server ID", "Status", "Exit Code", "Duration")

		for serverID, reqData := range serverRequests {
			if req, ok := reqData.(map[string]interface{}); ok {
				reqStatus := getStringField(req, "status")
				exitCode := "-"
				if ec, ok := req["exit_code"].(float64); ok {
					exitCode = fmt.Sprintf("%d", int(ec))
				}
				duration := "-"
				if dur, ok := req["duration"].(float64); ok {
					duration = fmt.Sprintf("%.2fs", dur/1000)
				}

				// Truncate server ID for display
				displayID := serverID
				if len(displayID) > 12 {
					displayID = displayID[:12] + "..."
				}

				table.AddRow(displayID, formatStatus(reqStatus), exitCode, duration)
			}
		}

		printer.Print(table.Render())
	}

	// Error information
	if errors, ok := job["errors"].([]interface{}); ok && len(errors) > 0 {
		printer.Print("")
		printer.Print(headerStyle.Render("Errors"))
		for _, err := range errors {
			if errStr, ok := err.(string); ok {
				printer.PrintError(errStr)
			}
		}
	}

	printer.Print("")
}

func formatStatus(status string) string {
	statusStyle := lipgloss.NewStyle().Bold(true)

	switch status {
	case "completed":
		return statusStyle.Foreground(lipgloss.Color("2")).Render("✓ " + status)
	case "failed":
		return statusStyle.Foreground(lipgloss.Color("1")).Render("✗ " + status)
	case "running":
		return statusStyle.Foreground(lipgloss.Color("3")).Render("⟳ " + status)
	case "pending":
		return statusStyle.Foreground(lipgloss.Color("6")).Render("◷ " + status)
	default:
		return status
	}
}

func getStringField(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func init() {
	StatusCmd.Flags().BoolP("watch", "w", false, "Watch job status until completion")
	JobsCmd.AddCommand(StatusCmd)
}
