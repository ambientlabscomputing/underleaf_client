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
  ufctl jobs status abc-123-xyz --watch
  ufctl jobs status abc-123-xyz --output    # Show command output`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)
		jobID := args[0]

		watch, _ := cmd.Flags().GetBool("watch")
		showOutput, _ := cmd.Flags().GetBool("output")

		if watch {
			return watchJobStatus(ctx, deps, jobID, showOutput)
		}

		return showJobStatus(ctx, deps, jobID, showOutput)
	},
}

func showJobStatus(ctx context.Context, deps *utils.DependencyManager, jobID string, showOutput bool) error {
	// Get job status from API
	job, err := getJob(ctx, deps, jobID)
	if err != nil {
		deps.Printer.PrintError("Failed to get job: " + err.Error())
		return err
	}

	displayJobStatus(deps.Printer, job, showOutput)
	return nil
}

func watchJobStatus(ctx context.Context, deps *utils.DependencyManager, jobID string, showOutput bool) error {
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
			displayJobStatus(deps.Printer, job, showOutput)

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

func displayJobStatus(printer ui.Printer, job map[string]interface{}, showOutput bool) {
	boldStyle := lipgloss.NewStyle().Bold(true)
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	labelStyle := lipgloss.NewStyle().Width(20)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	jobID := getStringField(job, "id")
	jobType := getStringField(job, "type")
	status := getStringField(job, "status")
	createdAt := getStringField(job, "createdat")
	if createdAt == "" {
		createdAt = getStringField(job, "created_at")
	}

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

	// Display type-specific content
	switch jobType {
	case "run_command":
		displayRunCommandJob(printer, job, showOutput, headerStyle, labelStyle, dimStyle)
	default:
		displayGenericJob(printer, job, headerStyle, labelStyle)
	}

	// Events timeline
	displayEvents(printer, job, headerStyle, dimStyle)

	// Error information
	if errors, ok := job["errors"].([]interface{}); ok && len(errors) > 0 {
		printer.Print("")
		printer.Print(headerStyle.Render("Errors"))
		for _, err := range errors {
			if errStr, ok := err.(string); ok {
				printer.PrintError("  " + errStr)
			}
		}
	}

	printer.Print("")
}

// displayRunCommandJob shows rich output for run_command job type
func displayRunCommandJob(printer ui.Printer, job map[string]interface{}, showOutput bool, headerStyle, labelStyle, dimStyle lipgloss.Style) {
	payload, ok := job["payload"].(map[string]interface{})
	if !ok {
		return
	}

	printer.Print("")
	printer.Print(headerStyle.Render("Command Details"))

	if command, ok := payload["command"].(string); ok {
		printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("Command:"), command))
	}
	if workDir, ok := payload["work_dir"].(string); ok && workDir != "" {
		printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("Work Directory:"), workDir))
	}
	if timeout, ok := payload["timeout"].(float64); ok && timeout > 0 {
		printer.Print(fmt.Sprintf("%s%ds", labelStyle.Render("Timeout:"), int(timeout)))
	}

	// Server targeting
	if allServers, ok := payload["all_servers"].(bool); ok && allServers {
		printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("Target:"), "All Servers"))
	} else if serverIDs, ok := payload["server_ids"].([]interface{}); ok && len(serverIDs) > 0 {
		printer.Print(fmt.Sprintf("%s%d server(s)", labelStyle.Render("Target:"), len(serverIDs)))
		for _, id := range serverIDs {
			if idStr, ok := id.(string); ok {
				printer.Print(fmt.Sprintf("%s  • %s", labelStyle.Render(""), dimStyle.Render(idStr)))
			}
		}
	}

	// Server requests with results
	serverRequests, ok := payload["server_requests"].(map[string]interface{})
	if !ok || len(serverRequests) == 0 {
		return
	}

	printer.Print("")
	printer.Print(headerStyle.Render("Server Results"))

	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	outputStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	outputHeaderStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4"))

	for serverID, reqData := range serverRequests {
		req, ok := reqData.(map[string]interface{})
		if !ok {
			continue
		}

		reqStatus := getStringField(req, "status")

		// Display server header
		printer.Print("")
		printer.Print(fmt.Sprintf("  %s %s", formatStatus(reqStatus), dimStyle.Render(serverID)))

		// Get result data
		result, hasResult := req["result"].(map[string]interface{})
		if hasResult {
			// Exit code
			if exitCode, ok := result["exitcode"].(float64); ok {
				exitCodeInt := int(exitCode)
				exitCodeStr := fmt.Sprintf("%d", exitCodeInt)
				if exitCodeInt == 0 {
					exitCodeStr = successStyle.Render(exitCodeStr)
				} else {
					exitCodeStr = errorStyle.Render(exitCodeStr)
				}
				printer.Print(fmt.Sprintf("      Exit Code: %s", exitCodeStr))
			}

			// Timestamp
			if timestamp, ok := result["timestamp"].(string); ok {
				printer.Print(fmt.Sprintf("      Completed: %s", dimStyle.Render(timestamp)))
			}

			// Show output if requested or if there's stderr
			stdout := getStringField(result, "stdout")
			stderr := getStringField(result, "stderr")

			if showOutput || stderr != "" {
				if stdout != "" {
					printer.Print("")
					printer.Print(fmt.Sprintf("      %s", outputHeaderStyle.Render("stdout:")))
					printIndentedOutput(printer, stdout, "        ", outputStyle)
				}

				if stderr != "" {
					printer.Print("")
					printer.Print(fmt.Sprintf("      %s", errorStyle.Render("stderr:")))
					printIndentedOutput(printer, stderr, "        ", errorStyle)
				}
			} else if stdout != "" {
				// Show truncated output hint
				lines := strings.Split(strings.TrimSpace(stdout), "\n")
				printer.Print(fmt.Sprintf("      Output: %s", dimStyle.Render(fmt.Sprintf("%d lines (use --output to show)", len(lines)))))
			}
		}
	}
}

// printIndentedOutput prints multi-line output with indentation
func printIndentedOutput(printer ui.Printer, output string, indent string, style lipgloss.Style) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	maxLines := 50 // Limit output display

	for i, line := range lines {
		if i >= maxLines {
			printer.Print(fmt.Sprintf("%s%s", indent, style.Render(fmt.Sprintf("... (%d more lines)", len(lines)-maxLines))))
			break
		}
		printer.Print(fmt.Sprintf("%s%s", indent, style.Render(line)))
	}
}

// displayGenericJob shows basic payload info for unknown job types
func displayGenericJob(printer ui.Printer, job map[string]interface{}, headerStyle, labelStyle lipgloss.Style) {
	payload, ok := job["payload"].(map[string]interface{})
	if !ok {
		return
	}

	printer.Print("")
	printer.Print(headerStyle.Render("Payload"))

	for key, value := range payload {
		printer.Print(fmt.Sprintf("%s%v", labelStyle.Render(key+":"), value))
	}
}

// displayEvents shows the job events timeline
func displayEvents(printer ui.Printer, job map[string]interface{}, headerStyle, dimStyle lipgloss.Style) {
	events, ok := job["events"].([]interface{})
	if !ok || len(events) == 0 {
		return
	}

	printer.Print("")
	printer.Print(headerStyle.Render("Events Timeline"))

	for _, event := range events {
		eventMap, ok := event.(map[string]interface{})
		if !ok {
			continue
		}

		timestamp := getStringField(eventMap, "timestamp")
		message := getStringField(eventMap, "message")

		// Format timestamp to show just time if same day
		timeStr := timestamp
		if t, err := time.Parse(time.RFC3339, timestamp); err == nil {
			timeStr = t.Format("15:04:05")
		}

		printer.Print(fmt.Sprintf("  %s  %s", dimStyle.Render(timeStr), message))
	}
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
	StatusCmd.Flags().BoolP("output", "o", false, "Show full command output (stdout/stderr)")
	JobsCmd.AddCommand(StatusCmd)
}
