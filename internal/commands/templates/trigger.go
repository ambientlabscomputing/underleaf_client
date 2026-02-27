package templates

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

var TriggerCmd = &cobra.Command{
	Use:   "trigger <template-id>",
	Short: "Trigger execution of a command template",
	Long: `Trigger execution of a command template with variable values.

By default, waits for the job to complete. Use --detach to run in background.

Examples:
  # Trigger with input values
  ufctl templates trigger abc123 --input environment=production --input replicas=3

  # Trigger in background
  ufctl templates trigger abc123 --input env=prod --detach

  # Override target servers
  ufctl templates trigger abc123 --server-ids server-1,server-2`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		templateID := args[0]
		inputSpecs, _ := cmd.Flags().GetStringArray("input")
		serverIDs, _ := cmd.Flags().GetStringSlice("server-ids")
		tagSpecs, _ := cmd.Flags().GetStringSlice("tag")
		detach, _ := cmd.Flags().GetBool("detach")

		// Parse inputs
		inputs := parseKeyValueSpecs(inputSpecs)
		tags := parseKeyValueSpecs(tagSpecs)

		req := TriggerTemplateRequest{
			Inputs:    inputs,
			ServerIDs: serverIDs,
			Tags:      tags,
		}

		return triggerTemplate(ctx, deps, templateID, req, detach)
	},
}

type TriggerTemplateRequest struct {
	Inputs    map[string]string `json:"inputs,omitempty"`
	ServerIDs []string          `json:"server_ids,omitempty"`
	Tags      map[string]string `json:"tags,omitempty"`
}

type TriggerTemplateResponse struct {
	JobID         string `json:"job_id"`
	RenderedCmd   string `json:"rendered_command"`
	TargetServers int    `json:"target_servers"`
}

func triggerTemplate(ctx context.Context, deps *utils.DependencyManager, templateID string, req TriggerTemplateRequest, detach bool) error {
	boldStyle := lipgloss.NewStyle().Bold(true)

	// First, get the template to show what we're about to do
	var template Template
	err := deps.CPlaneClient.API().GET(context.Background(), "/templates/"+templateID, &template)
	if err != nil {
		deps.Printer.PrintError("Failed to get template: " + err.Error())
		return err
	}

	deps.Printer.Print("")
	deps.Printer.Print(boldStyle.Render("Triggering Template: ") + template.Name)
	if len(req.Inputs) > 0 {
		deps.Printer.Print("")
		deps.Printer.Print("  Inputs:")
		for k, v := range req.Inputs {
			deps.Printer.Print(fmt.Sprintf("    %s = %s", k, v))
		}
	}
	deps.Printer.Print("")

	// Trigger the template
	var response TriggerTemplateResponse
	err = deps.CPlaneClient.API().POST(context.Background(), "/templates/"+templateID+"/trigger", req, &response)
	if err != nil {
		deps.Printer.PrintError("Failed to trigger template: " + err.Error())
		return err
	}

	// Show rendered command
	deps.Printer.Print(fmt.Sprintf("  %s", boldStyle.Render("Rendered Command:")))
	deps.Printer.Print(fmt.Sprintf("    %s", response.RenderedCmd))
	deps.Printer.Print("")
	deps.Printer.Print(fmt.Sprintf("  Job ID: %s", response.JobID))
	deps.Printer.Print(fmt.Sprintf("  Target Servers: %d", response.TargetServers))
	deps.Printer.Print("")

	if detach {
		deps.Printer.PrintSuccess("Template triggered successfully (detached mode)")
		deps.Printer.Print("")
		deps.Printer.PrintInfo("Use 'ufctl jobs status " + response.JobID + "' to check status")
		return nil
	}

	// Wait for job completion
	deps.Printer.Print("Waiting for job completion...")
	deps.Printer.Print("")

	return waitForJobCompletion(ctx, deps, response.JobID)
}

func waitForJobCompletion(ctx context.Context, deps *utils.DependencyManager, jobID string) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	timeout := time.After(5 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			deps.Printer.PrintWarning("Timeout waiting for job completion")
			deps.Printer.PrintInfo("Use 'ufctl jobs status " + jobID + "' to check status")
			return nil
		case <-ticker.C:
			var job struct {
				ID        string                 `json:"id"`
				Status    string                 `json:"status"`
				Results   []JobResult            `json:"results,omitempty"`
				CreatedAt string                 `json:"created_at"`
				UpdatedAt string                 `json:"updated_at"`
				Payload   map[string]interface{} `json:"payload"`
			}

			if err := deps.CPlaneClient.API().GET(context.Background(), "/jobs/"+jobID, &job); err != nil {
				continue // Retry on error
			}
			switch job.Status {
			case "completed":
				showJobResults(deps, job.ID, job.Results)
				return nil
			case "failed":
				deps.Printer.PrintError("Job failed")
				showJobResults(deps, job.ID, job.Results)
				return fmt.Errorf("job failed")
			case "pending", "running":
				// Continue waiting
				continue
			default:
				deps.Printer.PrintWarning(fmt.Sprintf("Unknown job status: %s", job.Status))
			}
		}
	}
}

type JobResult struct {
	ServerID   string `json:"server_id"`
	ServerName string `json:"server_name"`
	Status     string `json:"status"`
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	Duration   int    `json:"duration_ms"`
}

func showJobResults(deps *utils.DependencyManager, jobID string, results []JobResult) {
	if len(results) == 0 {
		deps.Printer.PrintInfo("No results available yet")
		return
	}

	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	boldStyle := lipgloss.NewStyle().Bold(true)

	// Summary
	var succeeded, failed int
	for _, r := range results {
		if r.Status == "completed" && r.ExitCode == 0 {
			succeeded++
		} else {
			failed++
		}
	}

	deps.Printer.Print("")
	deps.Printer.Print(boldStyle.Render("Job Results"))
	deps.Printer.Print(fmt.Sprintf("  %s  %s",
		successStyle.Render(fmt.Sprintf("%d succeeded", succeeded)),
		errorStyle.Render(fmt.Sprintf("%d failed", failed)),
	))
	deps.Printer.Print("")

	// Build table
	table := ui.NewTableBuilder().
		WithHeaders("Server", "Status", "Exit", "Duration", "Output")

	for _, r := range results {
		serverDisplay := r.ServerName
		if serverDisplay == "" {
			serverDisplay = r.ServerID
		}

		var statusDisplay string

		duration := fmt.Sprintf("%dms", r.Duration)

		// Truncate output
		output := strings.TrimSpace(r.Stdout)
		if output == "" {
			output = strings.TrimSpace(r.Stderr)
		}
		if len(output) > 50 {
			output = output[:47] + "..."
		}
		if output == "" {
			output = "-"
		}

		table.AddRow(serverDisplay, statusDisplay, fmt.Sprintf("%d", r.ExitCode), duration, output)
	}

	deps.Printer.Print(table.Render())
	deps.Printer.Print("")
	deps.Printer.PrintInfo("Use 'ufctl jobs status " + jobID + "' for full output")
}

func init() {
	TriggerCmd.Flags().StringArrayP("input", "i", []string{}, "Input value (key=value)")
	TriggerCmd.Flags().StringSlice("server-ids", []string{}, "Override target server IDs")
	TriggerCmd.Flags().StringSlice("tag", []string{}, "Override target tags (key=value)")
	TriggerCmd.Flags().BoolP("detach", "d", false, "Run in background without waiting")
	TemplatesCmd.AddCommand(TriggerCmd)
}
