package servers

import (
	"fmt"
	"strings"

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

The selector can be:
  - A server ID (e.g., "server-abc123")
  - A tag selector (e.g., "env=production")
  - "all" to target all servers

Examples:
  ufctl servers exec server-abc123 -- echo "Hello World"
  ufctl servers exec env=production -- npm run deploy
  ufctl servers exec all -- uptime`,
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
		showExecPlan(deps.Printer, selector, command, timeout)

		// Dispatch the command
		response, err := deps.CPlaneClient.Commands.DispatchCommand(ctx, req)
		if err != nil {
			deps.Printer.PrintError("Failed to dispatch command: " + err.Error())
			return err
		}

		// Show result
		deps.Printer.PrintSuccess("Command dispatched successfully")
		deps.Printer.Print("")

		// Show trace ID prominently for copy-paste
		traceStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
		deps.Printer.Print("  Trace ID:")
		deps.Printer.Print("  " + traceStyle.Render(response.TraceID))
		deps.Printer.Print("")

		deps.Printer.Print(fmt.Sprintf("  Target servers: %d", len(response.TargetServers)))

		if len(response.TargetServers) > 0 && len(response.TargetServers) <= 5 {
			deps.Printer.Print("  Servers:")
			for _, s := range response.TargetServers {
				deps.Printer.Print(fmt.Sprintf("    - %s", s))
			}
		}

		deps.Printer.Print("")
		deps.Printer.PrintInfo(fmt.Sprintf("Check status: ufctl jobs status %s", response.TraceID))
		deps.Printer.PrintInfo(fmt.Sprintf("Watch live:   ufctl jobs status %s --watch", response.TraceID))

		return nil
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
func showExecPlan(printer ui.Printer, selector string, command []string, timeout int) {
	boldStyle := lipgloss.NewStyle().Bold(true)
	printer.Print("")
	printer.Print(boldStyle.Render("Command Execution"))
	printer.Print(fmt.Sprintf("  Selector: %s", selector))
	printer.Print(fmt.Sprintf("  Command:  %s", strings.Join(command, " ")))
	if timeout > 0 {
		printer.Print(fmt.Sprintf("  Timeout:  %ds", timeout))
	}
	printer.Print("")
}

func init() {
	ExecCmd.Flags().IntP("timeout", "t", 0, "Command timeout in seconds (0 = use server default)")
	ExecCmd.Flags().StringSliceP("env", "e", []string{}, "Environment variables (KEY=VALUE)")
	ServersCmd.AddCommand(ExecCmd)
}
