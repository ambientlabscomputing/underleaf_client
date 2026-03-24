package local

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/agent"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/devmode"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

// StartCmd represents the unified start command
// `ufctl start` combines register + mTLS setup + agent start -d in one command
var StartCmd = &cobra.Command{
	Use:   "start",
	Short: "Register server and start agent (simplified onboarding)",
	Long: `One-command setup: Registers your server, sets up mTLS certificates, and starts the agent daemon.

This is the recommended way to get started with Underleaf. It combines:
  1. Server registration
  2. mTLS certificate setup (ufctl csr generate && ufctl csr submit)
  3. Agent daemon start (ufctl agent start -d)

Prerequisites:
  - You must be authenticated (run 'ufctl auth login' first)
  - The 'underleaf_agent' binary must be available

Examples:
  # Standard usage - interactive setup
  ufctl start

  # Provide server name directly
  ufctl start --name my-server

  # Use custom agent port
  ufctl start --port 9090

  # Claim an existing server identity (e.g. after wiping local config)
  ufctl start --existing`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)
		printer := ui.GetPrinter(ctx)

		serverName, _ := cmd.Flags().GetString("name")
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

		// Step 1: Check if already registered, if not register silently
		existingID, hasID := deps.ConfigClient.Get("local.server_id")
		existingName, hasName := deps.ConfigClient.Get("local.server_name")

		useExisting, _ := cmd.Flags().GetBool("existing")

		if !hasID || existingID == nil || !hasName || existingName == nil {
			if useExisting {
				// --existing: prompt for server name/ID, validate staleness, claim identity
				if err := claimExistingServer(ctx, deps, nil); err != nil {
					return err
				}
			} else {
				// Normal registration path with name-collision detection
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

				// Check for a name collision before creating a new server record
				registrationHandled := false
				for {
					existing, findErr := deps.ServerSvc.FindExistingServer(ctx, serverName)

					if findErr == nil {
						// A stale server with this name exists — offer to claim it
						printer.Print("")
						deps.Printer.PrintWarning(fmt.Sprintf("A server named '%s' already exists.", serverName))
						deps.Printer.PrintInfo("  ID:   " + existing.ID)
						if existing.LastCheckIn != nil && existing.LastCheckIn.Valid {
							deps.Printer.PrintInfo("  Last check-in: " + existing.LastCheckIn.Time.Format("2006-01-02 15:04:05"))
						} else {
							deps.Printer.PrintInfo("  Last check-in: Never")
						}
						printer.Print("")

						choices := []string{"Claim this existing server", "Register with a different name"}
						selected, selErr := ui.RunSelection("What would you like to do?", choices)
						if selErr != nil || len(selected) == 0 {
							return fmt.Errorf("selection cancelled")
						}

						if selected[0] == "Claim this existing server" {
							if claimErr := claimExistingServer(ctx, deps, existing); claimErr != nil {
								return claimErr
							}
							registrationHandled = true
							break
						}

						// User wants a different name — re-prompt
						var repromptErr error
						serverName, repromptErr = ui.PromptInput("Enter a different name for this server:", "")
						if repromptErr != nil {
							return fmt.Errorf("failed to get server name: %w", repromptErr)
						}
						if serverName == "" {
							return fmt.Errorf("server name is required")
						}
						continue
					}

					// FindExistingServer returned an error
					if strings.Contains(findErr.Error(), "within last 5 minutes") {
						// Server is actively running — cannot claim, must use a different name
						printer.Print("")
						deps.Printer.PrintWarning(fmt.Sprintf("Server '%s' is currently active (checked in recently).", serverName))
						deps.Printer.PrintInfo("Decommission the running server first, or choose a different name.")
						printer.Print("")
						var repromptErr error
						serverName, repromptErr = ui.PromptInput("Enter a different name for this server:", "")
						if repromptErr != nil {
							return fmt.Errorf("failed to get server name: %w", repromptErr)
						}
						if serverName == "" {
							return fmt.Errorf("server name is required")
						}
						continue
					}

					// "no server found" — name is clean, proceed with normal registration
					break
				}

				if !registrationHandled {
					if err := deps.ServerSvc.RegisterServer(ctx, serverName); err != nil {
						return fmt.Errorf("failed to register server: %w", err)
					}
					serverIDRaw, _ := deps.ConfigClient.Get("local.server_id")
					serverID, ok := serverIDRaw.(string)
					if !ok || serverID == "" {
						return fmt.Errorf("failed to get server ID after registration")
					}
					_ = downloadAndSaveConfigSnapshot(ctx, &deps.Printer, serverID)
				}
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
		// Resolve the build config path to absolute so the daemon child can find it
		var absBuildConfigPath string
		if devConfig != nil && buildConfigPath != "" {
			absBuildConfigPath, _ = filepath.Abs(buildConfigPath)
		}

		// --health-timeout 0 means "disable" — we map that to a negative sentinel
		// so NewLauncher's zero-default (120s) does not override the user's intent.
		launcherHealthTimeout := healthTimeout
		if healthTimeout == 0 {
			launcherHealthTimeout = -1 * time.Nanosecond
		}

		launcher := agent.NewLauncher(agent.LauncherConfig{
			Mode:            agent.ModeDaemon,
			Port:            port,
			HealthTimeout:   launcherHealthTimeout,
			DevConfig:       devConfig,
			BuildConfigPath: absBuildConfigPath,
		})

		// Save port to config (do this before checking if running, so port is always updated)
		_ = deps.ConfigClient.Set("agent.port", port)
		// Silently ignore config save errors

		// Check if already running
		if !launcher.IsRunning() {
			if err := launcher.Start(ctx); err != nil {
				return fmt.Errorf("failed to start agent: %w", err)
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

		printer.Print("")
		printer.PrintSuccess("Underleaf agent is running.")
		printer.Print("")
		printer.PrintSuccess("Server registered: " + serverNameDisplay)
		printer.PrintSuccess("Secure connection established")
		printer.PrintSuccess("Agent started")
		printer.Print("")
		printer.Print("You're ready to go.")
		printer.Print("")
		printer.Print("Try running a command on this server:")
		printer.Print("")
		printer.Print(fmt.Sprintf("  ufctl servers exec %s -- 'uname -a'", serverID))
		printer.Print("")
		printer.Print("This will execute a job and show it in the dashboard.")
		printer.Print("")

		if uiBaseURL != "" && orgSlug != "" {
			printer.Print("View results in the UI:")
			printer.Print(fmt.Sprintf("  %s/org/%s/jobs", uiBaseURL, orgSlug))
			printer.Print("")
		}

		printer.Print("Other useful commands:")
		printer.Print("  ufctl agent status")
		printer.Print("  ufctl jobs list")
		printer.Print("")

		return nil
	},
}

func init() {
	StartCmd.Flags().StringP("name", "n", "", "Server name (auto-generated if not provided)")
	StartCmd.Flags().IntP("port", "p", 2240, "Agent port")
	StartCmd.Flags().Bool("existing", false, "Claim an existing server identity instead of creating a new one")
	StartCmd.Flags().Duration("health-timeout", 120*time.Second, "Max time to wait for agent HTTP health check (0 = disable timeout)")
}
