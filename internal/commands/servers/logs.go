package servers

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ambientlabscomputing/underleaf_client/internal/agent"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
	"github.com/spf13/cobra"
)

var LogsCmd = &cobra.Command{
	Use:   "logs [server-id]",
	Short: "View logs from a server",
	Long: `View logs from a server agent.

If no server ID is provided, shows logs from the local agent.

Examples:
  ufctl servers logs              # View local agent logs
  ufctl servers logs -f           # Follow local agent logs
  ufctl servers logs -n 100       # Show last 100 lines`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		// Get flags
		follow, _ := cmd.Flags().GetBool("follow")
		lines, _ := cmd.Flags().GetInt("lines")

		if len(args) == 0 {
			// Show local agent logs
			return showLocalLogs(deps, follow, lines)
		}

		// Show remote server logs (not implemented yet)
		serverID := args[0]
		deps.Printer.PrintWarning(fmt.Sprintf("Remote log viewing for server '%s' is not yet implemented", serverID))
		deps.Printer.Print("  Logs are stored on each server and can be viewed locally.")
		return nil
	},
}

func showLocalLogs(deps *utils.DependencyManager, follow bool, lines int) error {
	// Get log file path
	basePath := policy_manager.GetBasePath(true) // agent path
	logFile := filepath.Join(basePath, "agent.log")

	// Check if log file exists
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		// Check if agent is running
		launcher := agent.NewLauncher(agent.LauncherConfig{})
		if !launcher.IsRunning() {
			deps.Printer.PrintWarning("Agent is not running and no log file found")
			deps.Printer.Print(fmt.Sprintf("  Expected log file: %s", logFile))
			deps.Printer.Print("  Start agent with: ufctl agent start")
			return nil
		}

		deps.Printer.PrintWarning("Log file not found")
		deps.Printer.Print(fmt.Sprintf("  Expected: %s", logFile))
		return nil
	}

	if follow {
		return tailLogFile(deps, logFile)
	}

	return showLastNLines(deps, logFile, lines)
}

func showLastNLines(deps *utils.DependencyManager, logFile string, n int) error {
	file, err := os.Open(logFile)
	if err != nil {
		deps.Printer.PrintError("Failed to open log file: " + err.Error())
		return err
	}
	defer file.Close()

	// Read all lines
	var allLines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		allLines = append(allLines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		deps.Printer.PrintError("Error reading log file: " + err.Error())
		return err
	}

	// Get last N lines
	start := 0
	if len(allLines) > n {
		start = len(allLines) - n
	}

	deps.Printer.Print(fmt.Sprintf("=== Last %d lines of %s ===", n, logFile))
	deps.Printer.Print("")

	for _, line := range allLines[start:] {
		deps.Printer.Print(line)
	}

	return nil
}

func tailLogFile(deps *utils.DependencyManager, logFile string) error {
	deps.Printer.Print(fmt.Sprintf("=== Following %s (Ctrl+C to exit) ===", logFile))
	deps.Printer.Print("")

	file, err := os.Open(logFile)
	if err != nil {
		deps.Printer.PrintError("Failed to open log file: " + err.Error())
		return err
	}
	defer file.Close()

	// Seek to end of file
	_, err = file.Seek(0, io.SeekEnd)
	if err != nil {
		deps.Printer.PrintError("Failed to seek to end of file: " + err.Error())
		return err
	}

	reader := bufio.NewReader(file)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				// No more data, wait a bit and try again
				continue
			}
			return err
		}
		fmt.Print(line)
	}
}

func init() {
	LogsCmd.Flags().BoolP("follow", "f", false, "Follow log output")
	LogsCmd.Flags().IntP("lines", "n", 50, "Number of lines to show")
	ServersCmd.AddCommand(LogsCmd)
}
