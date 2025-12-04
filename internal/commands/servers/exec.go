package servers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var ExecCmd = &cobra.Command{
	Use:   "exec [selector] -- [command...]",
	Short: "Execute a command on one or more servers",
	Long: `Execute a command on one or more servers via the control plane.

By default, the command waits for execution to complete and displays results.
Use --detach (-d) to run in the background without waiting.

The selector can be:
  - A server ID (e.g., "server-abc123")
  - A tag selector (e.g., "env=production")
  - "all" to target all servers

Examples:
  ufctl servers exec server-abc123 -- echo "Hello World"
  ufctl servers exec env=production -- npm run deploy
  ufctl servers exec all -- uptime
  ufctl servers exec all -d -- long-running-task    # Detached mode`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		// Parse selector and command
		selector, command, err := parseExecArgs(args)
		if err != nil {
			deps.Printer.PrintError(err.Error())
			return err
		}

		// Get flags
		timeout, _ := cmd.Flags().GetInt("timeout")
		envVars, _ := cmd.Flags().GetStringSlice("env")
		detach, _ := cmd.Flags().GetBool("detach")

		// Build environment map
		envMap := make(map[string]string)
		for _, e := range envVars {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) == 2 {
				envMap[parts[0]] = parts[1]
			}
		}

		// Build full command string
		fullCommand := strings.Join(command, " ")

		// Build dispatch request
		req := controlplane.DispatchCommandRequest{
			Command: fullCommand,
			Timeout: timeout,
			EnvVars: envMap,
		}

		// Apply selector
		applyCommandSelector(&req, selector)

		// Show what we're about to do
		showExecPlan(deps.Printer, selector, command, timeout, detach)

		// Dispatch the command
		response, err := deps.CPlaneClient.Commands.DispatchCommand(ctx, req)
		if err != nil {
			deps.Printer.PrintError("Failed to dispatch command: " + err.Error())
			return err
		}

		traceID := response.TraceID
		if traceID == "" {
			traceID = response.JobID
		}

		// If detached, just show the job ID and exit
		if detach {
			showDetachedResult(deps.Printer, response, traceID)
			return nil
		}

		// Wait for completion
		return waitForCompletion(ctx, deps, traceID, len(response.TargetServers))
	},
}

// parseExecArgs parses the exec command arguments
// Expected format: [selector] -- [command...]
func parseExecArgs(args []string) (string, []string, error) {
	// Find the "--" separator
	separatorIdx := -1
	for i, arg := range args {
		if arg == "--" {
			separatorIdx = i
			break
		}
	}

	if separatorIdx == -1 {
		// No separator - assume first arg is selector, rest is command
		if len(args) < 2 {
			return "", nil, fmt.Errorf("usage: ufctl servers exec [selector] -- [command...]")
		}
		return args[0], args[1:], nil
	}

	if separatorIdx == 0 {
		return "", nil, fmt.Errorf("missing selector before '--'")
	}

	if separatorIdx == len(args)-1 {
		return "", nil, fmt.Errorf("missing command after '--'")
	}

	selector := args[0]
	command := args[separatorIdx+1:]

	return selector, command, nil
}

// applyCommandSelector applies the selector to the dispatch request
func applyCommandSelector(req *controlplane.DispatchCommandRequest, selector string) {
	switch {
	case selector == "all":
		req.AllServers = true
	case strings.Contains(selector, "="):
		// Tag selector (e.g., "env=production")
		parts := strings.SplitN(selector, "=", 2)
		req.Tags = map[string]string{parts[0]: parts[1]}
	default:
		// Assume it's a server ID
		req.ServerIDs = []string{selector}
	}
}

// showExecPlan displays what will be executed
func showExecPlan(printer ui.Printer, selector string, command []string, timeout int, detach bool) {
	boldStyle := lipgloss.NewStyle().Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	printer.Print("")
	printer.Print(boldStyle.Render("Command Execution"))
	printer.Print(fmt.Sprintf("  Selector: %s", selector))
	printer.Print(fmt.Sprintf("  Command:  %s", strings.Join(command, " ")))
	if timeout > 0 {
		printer.Print(fmt.Sprintf("  Timeout:  %ds", timeout))
	}
	if detach {
		printer.Print(fmt.Sprintf("  Mode:     %s", dimStyle.Render("detached (fire-and-forget)")))
	}
	printer.Print("")
}

// showDetachedResult displays the result for detached mode
func showDetachedResult(printer ui.Printer, response *controlplane.DispatchCommandResponse, traceID string) {
	printer.PrintSuccess("Command dispatched successfully")
	printer.Print("")

	// Show trace ID prominently for copy-paste
	traceStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	printer.Print("  Job ID:")
	printer.Print("  " + traceStyle.Render(traceID))
	printer.Print("")

	printer.Print(fmt.Sprintf("  Target servers: %d", len(response.TargetServers)))

	if len(response.TargetServers) > 0 && len(response.TargetServers) <= 5 {
		printer.Print("  Servers:")
		for _, s := range response.TargetServers {
			printer.Print(fmt.Sprintf("    - %s", s))
		}
	}

	printer.Print("")
	printer.PrintInfo(fmt.Sprintf("Check status: ufctl jobs status %s", traceID))
	printer.PrintInfo(fmt.Sprintf("Watch live:   ufctl jobs status %s --watch", traceID))
}

// waitForCompletion polls the job status until completion
func waitForCompletion(ctx context.Context, deps *utils.DependencyManager, jobID string, serverCount int) error {
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	boldStyle := lipgloss.NewStyle().Bold(true)

	deps.Printer.Print(dimStyle.Render(fmt.Sprintf("Waiting for %d server(s) to complete... (Ctrl+C to detach)", serverCount)))
	deps.Printer.Print("")

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	startTime := time.Now()
	lastEventCount := 0

	for {
		select {
		case <-ctx.Done():
			deps.Printer.Print("")
			deps.Printer.PrintInfo(fmt.Sprintf("Detached. Check status: ufctl jobs status %s", jobID))
			return nil
		case <-ticker.C:
			job, err := getJobStatus(deps, jobID)
			if err != nil {
				deps.Printer.PrintError("Failed to get job status: " + err.Error())
				return err
			}

			status := getStringField(job, "status")
			elapsed := time.Since(startTime).Round(time.Second)

			// Show new events as they come in
			if events, ok := job["events"].([]interface{}); ok {
				for i := lastEventCount; i < len(events); i++ {
					if event, ok := events[i].(map[string]interface{}); ok {
						message := getStringField(event, "message")
						deps.Printer.Print(fmt.Sprintf("  %s %s", dimStyle.Render("→"), message))
					}
				}
				lastEventCount = len(events)
			}

			// Check if completed
			if status == "completed" || status == "failed" {
				deps.Printer.Print("")

				if status == "completed" {
					deps.Printer.Print(successStyle.Render(fmt.Sprintf("✓ Completed in %s", elapsed)))
				} else {
					deps.Printer.Print(errorStyle.Render(fmt.Sprintf("✗ Failed after %s", elapsed)))
				}

				// Show results summary
				showResultsSummary(deps.Printer, job, boldStyle, successStyle, errorStyle, dimStyle)
				return nil
			}
		}
	}
}

// getJobStatus fetches job status from the API
func getJobStatus(deps *utils.DependencyManager, jobID string) (map[string]interface{}, error) {
	var job map[string]interface{}
	err := deps.CPlaneClient.API().GET("/jobs/"+jobID, &job)
	if err != nil {
		return nil, err
	}
	return job, nil
}

// showResultsSummary displays a summary of server results
func showResultsSummary(printer ui.Printer, job map[string]interface{}, boldStyle, successStyle, errorStyle, dimStyle lipgloss.Style) {
	payload, ok := job["payload"].(map[string]interface{})
	if !ok {
		return
	}

	serverRequests, ok := payload["server_requests"].(map[string]interface{})
	if !ok || len(serverRequests) == 0 {
		return
	}

	printer.Print("")
	printer.Print(boldStyle.Render("Results:"))

	for serverID, reqData := range serverRequests {
		req, ok := reqData.(map[string]interface{})
		if !ok {
			continue
		}

		reqStatus := getStringField(req, "status")

		// Get result data
		result, hasResult := req["result"].(map[string]interface{})

		// Format server line
		var statusIcon string
		var exitCodeStr string

		switch reqStatus {
		case "completed":
			statusIcon = successStyle.Render("✓")
		case "failed":
			statusIcon = errorStyle.Render("✗")
		default:
			statusIcon = dimStyle.Render("◷")
		}

		if hasResult {
			if exitCode, ok := result["exitcode"].(float64); ok {
				exitCodeInt := int(exitCode)
				if exitCodeInt == 0 {
					exitCodeStr = successStyle.Render(fmt.Sprintf("exit %d", exitCodeInt))
				} else {
					exitCodeStr = errorStyle.Render(fmt.Sprintf("exit %d", exitCodeInt))
				}
			}
		}

		// Truncate server ID
		displayID := serverID
		if len(displayID) > 20 {
			displayID = displayID[:8] + "..." + displayID[len(displayID)-8:]
		}

		printer.Print(fmt.Sprintf("  %s %s %s", statusIcon, displayID, exitCodeStr))

		// Show stderr if present (always show errors)
		if hasResult {
			if stderr := getStringField(result, "stderr"); stderr != "" {
				lines := strings.Split(strings.TrimSpace(stderr), "\n")
				for i, line := range lines {
					if i >= 3 {
						printer.Print(fmt.Sprintf("      %s", errorStyle.Render(fmt.Sprintf("... (%d more lines)", len(lines)-3))))
						break
					}
					printer.Print(fmt.Sprintf("      %s", errorStyle.Render(line)))
				}
			}
		}
	}

	printer.Print("")
	printer.PrintInfo(fmt.Sprintf("Full output: ufctl jobs status %s --output", getStringField(job, "id")))
}

// getStringField safely extracts a string field from a map
func getStringField(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func init() {
	ExecCmd.Flags().IntP("timeout", "t", 0, "Command timeout in seconds (0 = use server default)")
	ExecCmd.Flags().StringSliceP("env", "e", []string{}, "Environment variables (KEY=VALUE)")
	ExecCmd.Flags().BoolP("detach", "d", false, "Run in background without waiting for completion")
	ServersCmd.AddCommand(ExecCmd)
}
