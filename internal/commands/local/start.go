package local

import (
	"fmt"
	"io"
	"os"

	"github.com/ambientlabscomputing/underleaf_client/internal/agent"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	startSuccessStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#00FF00"))

	startInfoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7D56F4"))

	startInstructionStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#999999"))
)

// StartCmd represents the unified start command
// `ufctl start` combines register + mTLS setup + agent start -d in one command
var StartCmd = &cobra.Command{
	Use:   "start",
	Short: "Register server and start agent (simplified onboarding)",
	Long: `One-command setup: Registers your server, sets up mTLS certificates, and starts the agent daemon.

This is the recommended way to get started with Underleaf. It combines:
  1. Server registration (ufctl local register)
  2. mTLS certificate setup (ufctl local generate-csr && ufctl local submit-csr)
  3. Agent daemon start (ufctl local agent start -d)

Prerequisites:
  - You must be authenticated (run 'ufctl local auth login' first)
  - The 'underleaf_agent' binary must be available

Examples:
  # Standard usage - interactive setup
  ufctl start

  # Provide server name directly
  ufctl start --name my-server

  # Use custom agent port
  ufctl start --port 9090`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		serverName, _ := cmd.Flags().GetString("name")
		port, _ := cmd.Flags().GetInt("port")

		// Step 1: Check if already registered, if not register silently
		existingID, hasID := deps.ConfigClient.Get("local.server_id")
		existingName, hasName := deps.ConfigClient.Get("local.server_name")

		if !hasID || existingID == nil || !hasName || existingName == nil {
			// Get server name
			if serverName == "" {
				hostname, _ := os.Hostname()
				defaultName := fmt.Sprintf("server-%s", hostname)
				var err error
				serverName, err = ui.PromptInput("Enter a name for this server:", defaultName)
				if err != nil {
					return fmt.Errorf("failed to get server name: %w", err)
				}
			}

			if serverName == "" {
				return fmt.Errorf("server name is required")
			}

			// Register the server silently
			err := deps.ServerSvc.RegisterServer(ctx, serverName)
			if err != nil {
				return fmt.Errorf("failed to register server: %w", err)
			}

			// Get the server ID from config after registration
			serverIDRaw, _ := deps.ConfigClient.Get("local.server_id")
			serverID, ok := serverIDRaw.(string)
			if !ok || serverID == "" {
				return fmt.Errorf("failed to get server ID after registration")
			}

			// Download config snapshot silently
			if err := downloadAndSaveConfigSnapshot(ctx, &deps.Printer, serverID); err != nil {
				// Silently ignore config snapshot errors
			}
		}

		// Step 2: Setup mTLS certificate silently
		serverIDRaw, _ := deps.ConfigClient.Get("local.server_id")
		orgIDRaw, _ := deps.ConfigClient.Get("local.organization_id")
		orgNameRaw, _ := deps.ConfigClient.Get("local.organization_name")

		serverID := ""
		orgID := ""
		orgName := "Underleaf"

		if serverIDRaw != nil {
			if id, ok := serverIDRaw.(string); ok {
				serverID = id
			}
		}
		if orgIDRaw != nil {
			if id, ok := orgIDRaw.(string); ok {
				orgID = id
			}
		}
		if orgNameRaw != nil {
			if name, ok := orgNameRaw.(string); ok {
				orgName = name
			}
		}

		// Create a silent printer for mTLS setup
		silentPrinter := ui.NewPrinter(ui.FormatTable).WithWriter(io.Discard)
		if err := SetupMTLSCertificate(ctx, silentPrinter, deps.ConfigClient, serverID, orgID, orgName); err != nil {
			return fmt.Errorf("failed to setup mTLS: %w", err)
		}

		// Step 3: Start agent daemon silently
		launcher := agent.NewLauncher(agent.LauncherConfig{
			Mode: agent.ModeDaemon,
			Port: port,
		})

		// Check if already running
		if !launcher.IsRunning() {
			if err := launcher.Start(ctx); err != nil {
				return fmt.Errorf("failed to start agent: %w", err)
			}

			// Save port to config
			if err := deps.ConfigClient.Set("agent.port", port); err != nil {
				// Silently ignore config save errors
			}
		}

		// Get server name for display
		serverNameDisplay := "unknown"
		if serverNameRaw, hasName := deps.ConfigClient.Get("local.server_name"); hasName && serverNameRaw != nil {
			if name, ok := serverNameRaw.(string); ok && name != "" {
				serverNameDisplay = name
			}
		}

		// Get org slug for dashboard link
		orgSlug := ""
		if orgSlugRaw, hasSlug := deps.ConfigClient.Get("local.organization_slug"); hasSlug && orgSlugRaw != nil {
			if slug, ok := orgSlugRaw.(string); ok {
				orgSlug = slug
			}
		}

		// Get dashboard URL from config
		uiBaseURL := ""
		if baseURLRaw, hasBaseURL := deps.ConfigClient.Get("api.base_url"); hasBaseURL && baseURLRaw != nil {
			apiBaseURL := baseURLRaw.(string)
			// Convert API URL to UI base URL
			// Remove /api/v1/servers suffix if present
			if len(apiBaseURL) >= 8 && apiBaseURL[len(apiBaseURL)-8:] == "/servers" {
				apiBaseURL = apiBaseURL[:len(apiBaseURL)-8]
			}
			if len(apiBaseURL) >= 7 && apiBaseURL[len(apiBaseURL)-7:] == "/api/v1" {
				uiBaseURL = apiBaseURL[:len(apiBaseURL)-7]
			}
		}

		fmt.Println()
		fmt.Println("Underleaf agent is running.")
		fmt.Println()
		fmt.Println(startSuccessStyle.Render("✔ Server registered: " + serverNameDisplay))
		fmt.Println(startSuccessStyle.Render("✔ Secure connection established"))
		fmt.Println(startSuccessStyle.Render("✔ Agent started"))
		fmt.Println()
		fmt.Println("You're ready to go.")
		fmt.Println()
		fmt.Println("Try running a command on this server:")
		fmt.Println()
		fmt.Println(startInstructionStyle.Render(fmt.Sprintf("  ufctl servers exec %s -- 'uname -a'", serverID)))
		fmt.Println()
		fmt.Println("This will execute a job and show it in the dashboard.")
		fmt.Println()

		if uiBaseURL != "" && orgSlug != "" {
			fmt.Println("View results in the UI:")
			fmt.Println(startInstructionStyle.Render(fmt.Sprintf("  %s/org/%s/jobs", uiBaseURL, orgSlug)))
			fmt.Println()
		}

		fmt.Println("Other useful commands:")
		fmt.Println(startInstructionStyle.Render("  ufctl local agent status"))
		fmt.Println(startInstructionStyle.Render("  ufctl jobs list"))
		fmt.Println()

		return nil
	},
}

func init() {
	StartCmd.Flags().StringP("name", "n", "", "Server name (auto-generated if not provided)")
	StartCmd.Flags().IntP("port", "p", 8080, "Agent port")
}
