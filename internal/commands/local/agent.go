package local

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/agent"
	"github.com/ambientlabscomputing/underleaf_client/internal/devmode"
	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

var AgentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage the local agent daemon",
	Long:  "Start, stop, and manage the Underleaf agent daemon",
}

var agentStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the agent daemon",
	Long: `Start the Underleaf agent daemon.

💡 TIP: For initial setup, use 'ufctl start' instead, which handles registration + mTLS + agent start.

In daemon mode (-d), requires the 'underleaf_agent' binary to be in PATH or the same directory as ufctl.
In development mode (--dev), runs the agent in the current process (foreground).

Examples:
  # Start in foreground (development mode)
  ufctl agent start --dev

  # Start as background daemon (requires underleaf_agent binary)
  ufctl agent start -d

  # Start on custom port
  ufctl agent start --dev --port 9090`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		printer := ui.GetPrinter(cmd.Context())

		// Show tip for new users
		existingID, hasID := policy_manager.GetConfig(cmd.Context()).Get("local.server_id")
		if !hasID || existingID == nil {
			printer.PrintInfo("💡 TIP: If you haven't registered yet, use 'ufctl start' for complete setup")
			fmt.Println()
		}

		// Get flags
		dev, _ := cmd.Flags().GetBool("dev")
		detach, _ := cmd.Flags().GetBool("detach")
		port, _ := cmd.Flags().GetInt("port")
		healthTimeout, _ := cmd.Flags().GetDuration("health-timeout")

		// Load dev config if dev-mode is enabled
		devMode, _ := cmd.Flags().GetBool("dev-mode")
		buildConfigPath, _ := cmd.Flags().GetString("build-config")

		var devConfig *devmode.DevConfig
		if devMode {
			var err error
			devConfig, err = devmode.LoadBuildConfig(buildConfigPath)
			if err != nil {
				printer.PrintError(fmt.Sprintf("Failed to load build config: %v", err))
				return err
			}
			if err := devConfig.Validate(); err != nil {
				printer.PrintError(fmt.Sprintf("Build config validation failed: %v", err))
				return err
			}
			PrintDevModeInfo(devConfig, printer)
		}

		// Determine launch mode
		mode := agent.ModeDev
		if detach {
			mode = agent.ModeDaemon
		} else if dev {
			mode = agent.ModeDev
		}

		// Create launcher
		// --health-timeout 0 means "disable" — map to negative sentinel so
		// NewLauncher's zero-default (120s) does not override the user's intent.
		launcherHealthTimeout := healthTimeout
		if healthTimeout == 0 {
			launcherHealthTimeout = -1 * time.Nanosecond
		}
		launcher := agent.NewLauncher(agent.LauncherConfig{
			Mode:          mode,
			Port:          port,
			HealthTimeout: launcherHealthTimeout,
			DevConfig:     devConfig,
		})

		// Check if already running
		if launcher.IsRunning() {
			status := launcher.GetStatus()
			printer.PrintError(fmt.Sprintf("Agent is already running (PID: %d, Port: %d)", status.PID, status.Port))
			return nil
		}

		// Start the agent
		if mode == agent.ModeDev {
			printer.PrintInfo(fmt.Sprintf("Starting agent in development mode on port %d...", port))
			printer.PrintInfo("Press Ctrl+C to stop")
			// Save port to config before starting (in case of immediate shutdown)
			config := policy_manager.GetConfig(cmd.Context())
			if err := config.Set("agent.port", port); err != nil {
				printer.PrintWarning(fmt.Sprintf("Failed to save port to config: %v", err))
			}
			// In dev mode, this blocks until shutdown
			return launcher.Start(ctx)
		} else {
			printer.PrintInfo(fmt.Sprintf("Starting agent daemon on port %d...", port))
			printer.PrintInfo("Requires 'underleaf_agent' binary in PATH or same directory")

			if err := launcher.Start(ctx); err != nil {
				printer.PrintError(fmt.Sprintf("Failed to start agent: %v", err))
				printer.PrintInfo("\nTroubleshooting:")
				printer.PrintInfo("- Ensure 'underleaf_agent' binary is installed")
				printer.PrintInfo("- Check if it's in PATH: which underleaf_agent")
				printer.PrintInfo("- Or use development mode: ufctl agent start --dev")
				return err
			}

			// Save the port to config manager so status commands can find it
			config := policy_manager.GetConfig(cmd.Context())
			if err := config.Set("agent.port", port); err != nil {
				printer.PrintWarning(fmt.Sprintf("Failed to save port to config: %v", err))
			}

			status := launcher.GetStatus()
			structuredLogFile := filepath.Join(os.TempDir(), "underleaf-agent-structured.log")
			printer.PrintSuccess(fmt.Sprintf("Agent started successfully (PID: %d)", status.PID))
			printer.Print(fmt.Sprintf("Stdout/Stderr: %s", status.LogFile))
			printer.Print(fmt.Sprintf("Structured logs: %s", structuredLogFile))
			return nil
		}
	},
}

var agentStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the agent daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		printer := ui.GetPrinter(cmd.Context())

		launcher := agent.NewLauncher(agent.LauncherConfig{})

		if !launcher.IsRunning() {
			printer.PrintWarning("Agent is not running")
			return nil
		}

		status := launcher.GetStatus()
		printer.PrintInfo(fmt.Sprintf("Stopping agent (PID: %d)...", status.PID))

		if err := launcher.Stop(); err != nil {
			return fmt.Errorf("failed to stop agent: %w", err)
		}

		printer.PrintSuccess("Agent stopped successfully")
		return nil
	},
}

var agentRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the agent daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		printer := ui.GetPrinter(cmd.Context())

		port, _ := cmd.Flags().GetInt("port")

		launcher := agent.NewLauncher(agent.LauncherConfig{
			Mode: agent.ModeDaemon,
			Port: port,
		})

		printer.PrintInfo("Restarting agent...")

		if err := launcher.Restart(ctx); err != nil {
			return fmt.Errorf("failed to restart agent: %w", err)
		}

		// Save the port to config manager so status commands can find it
		config := policy_manager.GetConfig(cmd.Context())
		if err := config.Set("agent.port", port); err != nil {
			printer.PrintWarning(fmt.Sprintf("Failed to save port to config: %v", err))
		}

		status := launcher.GetStatus()
		printer.PrintSuccess(fmt.Sprintf("Agent restarted successfully (PID: %d)", status.PID))
		return nil
	},
}

var agentStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show agent status",
	RunE: func(cmd *cobra.Command, args []string) error {
		printer := ui.GetPrinter(cmd.Context())

		// Try to get port from config first
		config := policy_manager.GetConfig(cmd.Context())
		port := 2240 // default (matches agent.port in ~/.underleaf/config.yaml)
		if portVal, ok := config.Get("agent.port"); ok {
			if portInt, ok := portVal.(int); ok {
				port = portInt
			}
		}

		launcher := agent.NewLauncher(agent.LauncherConfig{
			Port: port,
		})
		status := launcher.GetStatus()

		if !status.Running {
			printer.PrintWarning(status.Message)
			if status.PID != 0 {
				printer.Print(fmt.Sprintf("Stale PID file: %s", status.PIDFile))
			}
			return nil
		}

		// Try to ping the agent
		client := agent.NewClient(status.Port)
		healthy, err := client.GetHealth()

		// Show structured log file location (where slog writes)
		structuredLogFile := filepath.Join(os.TempDir(), "underleaf-agent-structured.log")

		table := ui.NewTableBuilder().
			WithTitle("Agent Status").
			WithHeaders("Property", "Value").
			AddRow("Status", "Running").
			AddRow("PID", fmt.Sprintf("%d", status.PID)).
			AddRow("Port", fmt.Sprintf("%d", status.Port)).
			AddRow("PID File", status.PIDFile).
			AddRow("Stdout/Stderr Log", status.LogFile).
			AddRow("Structured Log", structuredLogFile)

		if err == nil && healthy {
			table.AddRow("Health", "Healthy")
		} else {
			table.AddRow("Health", "Unreachable")
		}

		printer.Print(table.Render())
		return nil
	},
}

var agentLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Show agent logs",
	RunE: func(cmd *cobra.Command, args []string) error {
		printer := ui.GetPrinter(cmd.Context())

		launcher := agent.NewLauncher(agent.LauncherConfig{})
		status := launcher.GetStatus()

		// check that log file exists
		if _, err := os.Stat(status.LogFile); err != nil {
			printer.PrintError(fmt.Sprintf("Log file not found, expected at %s", status.LogFile))
			return nil
		}

		follow, _ := cmd.Flags().GetBool("follow")
		lines, _ := cmd.Flags().GetInt("lines")

		if status.LogFile == "" {
			printer.PrintError("Log file not found")
			return nil
		}

		// Use tail to show logs
		tailCmd := fmt.Sprintf("tail -n %d", lines)
		if follow {
			tailCmd += " -f"
		}
		tailCmd += fmt.Sprintf(" %s", status.LogFile)

		// Execute shell command
		shellCmd := exec.Command("bash", "-c", tailCmd)
		shellCmd.Stdout = os.Stdout
		shellCmd.Stderr = os.Stderr
		shellCmd.Stdin = os.Stdin
		return shellCmd.Run()
	},
}

func init() {
	// Start flags
	agentStartCmd.Flags().Bool("dev", false, "Run in development mode (foreground)")
	agentStartCmd.Flags().BoolP("detach", "d", false, "Run as background daemon (requires underleaf_agent binary)")
	agentStartCmd.Flags().IntP("port", "p", 2240, "Port to run the agent on")
	agentStartCmd.Flags().Duration("health-timeout", 120*time.Second, "Max time to wait for agent HTTP health check (0 = disable timeout)")

	// Restart flags
	agentRestartCmd.Flags().IntP("port", "p", 2240, "Port to run the agent on")

	// Logs flags
	agentLogsCmd.Flags().BoolP("follow", "f", false, "Follow log output")
	agentLogsCmd.Flags().IntP("lines", "n", 50, "Number of lines to show")

	// Add subcommands
	AgentCmd.AddCommand(agentStartCmd)
	AgentCmd.AddCommand(agentStopCmd)
	AgentCmd.AddCommand(agentRestartCmd)
	AgentCmd.AddCommand(agentStatusCmd)
	AgentCmd.AddCommand(agentLogsCmd)
}
