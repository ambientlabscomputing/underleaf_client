package update

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/logging"
	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/ambientlabscomputing/underleaf_client/internal/updater"
	"github.com/ambientlabscomputing/underleaf_client/pkg/version"
	"github.com/spf13/cobra"
)

var UpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Check for and apply client updates",
	Long: `Check for available updates to ufctl and underleaf_agent binaries.
Downloads and applies updates based on the configured desired version.`,
}

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Check for available updates",
	Long: `Check if updates are available for ufctl and underleaf_agent.
Compares current version against the desired version from server configuration.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := logging.GetLogger(ctx)
		printer := ui.GetPrinter(ctx)

		// Get desired version from server config snapshot
		desiredVersion := getDesiredVersion()

		printer.Print(fmt.Sprintf("Current version: %s", version.Version))
		printer.Print(fmt.Sprintf("Desired version: %s", desiredVersion))

		// Initialize update manager
		// Try agent path first (where the agent stores its state)
		agentBasePath := filepath.Join(policy_manager.GetBasePath(true), "updates")
		store := updater.NewStore(agentBasePath)

		// Load current state to get stored SHA
		state, stateErr := store.LoadState()
		if stateErr != nil {
			// Fall back to CLI path if agent state doesn't exist
			cliBasePath := filepath.Join(os.Getenv("HOME"), ".underleaf", "updates")
			store = updater.NewStore(cliBasePath)
			state, stateErr = store.LoadState()
		}

		if err := store.EnsureDirectories(); err != nil {
			return fmt.Errorf("failed to initialize update storage: %w", err)
		}

		checker := updater.NewGitHubReleaseChecker("ambientlabscomputing/underleaf_client")
		release, err := checker.FetchRelease(ctx, desiredVersion)
		if err != nil {
			return fmt.Errorf("failed to check for updates: %w", err)
		}

		printer.PrintSuccess(fmt.Sprintf("Found release: %s", release.Version))
		printer.Print(fmt.Sprintf("Published: %s", release.PublishedAt.Format("2006-01-02")))
		printer.Print(fmt.Sprintf("SHA256: %s", release.SHA256))

		// Compare SHA to detect new builds
		currentSHA := ""
		if stateErr == nil && state != nil {
			currentSHA = state.ReleaseSHA
		}

		if release.SHA256 == currentSHA && release.Version == version.Version {
			printer.PrintSuccess("You are running the latest version")
		} else {
			if release.Version != version.Version {
				printer.Print(fmt.Sprintf("\nVersion update available: %s → %s", version.Version, release.Version))
			} else {
				printer.Print("\nNew build available (SHA changed)")
				printer.Print(fmt.Sprintf("Current SHA: %s", currentSHA[:16]+"..."))
				printer.Print(fmt.Sprintf("Latest SHA:  %s", release.SHA256[:16]+"..."))
			}
			printer.Print("Run 'ufctl update apply' to install the update")
		}

		logger.Info("update check completed",
			"current", version.Version,
			"desired", desiredVersion,
			"available", release.Version)

		return nil
	},
}

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Download and apply available updates",
	Long: `Download and install updates for ufctl and underleaf_agent.
Creates backups of current binaries before applying updates.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := logging.GetLogger(ctx)
		printer := ui.GetPrinter(ctx)

		// Get desired version from server config snapshot
		desiredVersion := getDesiredVersion()

		printer.Print(fmt.Sprintf("Current version: %s", version.Version))
		printer.Print(fmt.Sprintf("Updating to: %s", desiredVersion))

		// Initialize update components - use agent path for state
		agentBasePath := filepath.Join(policy_manager.GetBasePath(true), "updates")
		store := updater.NewStore(agentBasePath)
		if err := store.EnsureDirectories(); err != nil {
			return fmt.Errorf("failed to initialize update storage: %w", err)
		}

		installer := updater.NewInstaller(store, nil, nil) // No agent restart for ufctl
		manager := updater.NewUpdateManager(updater.UpdateManagerConfig{
			Store:     store,
			Installer: installer,
		})

		// Start manager
		if err := manager.Start(ctx); err != nil {
			return fmt.Errorf("failed to start update manager: %w", err)
		}
		defer manager.Stop(ctx)

		// Check and download update
		printer.Print("Checking for updates...")
		if err := manager.CheckAndDownload(desiredVersion); err != nil {
			return fmt.Errorf("failed to download update: %w", err)
		}

		state := manager.GetState()
		if state.Status != updater.UpdateStatusPending {
			printer.PrintSuccess("Already at desired version")
			return nil
		}

		// Apply update
		printer.Print("Applying update...")
		if err := manager.ApplyUpdate(); err != nil {
			return fmt.Errorf("failed to apply update: %w", err)
		}

		printer.PrintSuccess(fmt.Sprintf("Successfully updated to %s", state.DesiredVersion))
		printer.Print("\nNote: The agent will need to be restarted separately to apply its update.")
		printer.Print("Run: ufctl restart")

		logger.Info("update applied successfully", "version", state.DesiredVersion)
		return nil
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current update status",
	Long:  `Display the current update state, including pending updates and last check time.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		printer := ui.GetPrinter(ctx)

		// Try agent path first (where the agent stores its state)
		agentBasePath := filepath.Join(policy_manager.GetBasePath(true), "updates")
		store := updater.NewStore(agentBasePath)

		state, err := store.LoadState()
		if err != nil {
			// Fall back to CLI path if agent state doesn't exist
			cliBasePath := filepath.Join(os.Getenv("HOME"), ".underleaf", "updates")
			store = updater.NewStore(cliBasePath)
			state, err = store.LoadState()
			if err != nil {
				return fmt.Errorf("failed to load update state: %w", err)
			}
		}

		printer.Print(fmt.Sprintf("Current Version:  %s", version.Version))
		printer.Print(fmt.Sprintf("Desired Version:  %s", state.DesiredVersion))
		printer.Print(fmt.Sprintf("Current SHA:      %s", state.ReleaseSHA))
		printer.Print(fmt.Sprintf("Status:           %s", state.Status))
		printer.Print(fmt.Sprintf("Last Check:       %s", state.LastCheck.Format("2006-01-02 15:04:05")))

		if state.Status == updater.UpdateStatusPending {
			printer.Print("\nPending Updates:")
			if state.PendingAgent != "" {
				printer.Print(fmt.Sprintf("  Agent:  %s", state.PendingAgent))
			}
			if state.PendingUfctl != "" {
				printer.Print(fmt.Sprintf("  Ufctl:  %s", state.PendingUfctl))
			}
			printer.Print("\nRun 'ufctl update apply' to install pending updates")
		}

		if state.ErrorMessage != "" {
			printer.PrintError(fmt.Sprintf("Last Error: %s", state.ErrorMessage))
		}

		return nil
	},
}

func init() {
	UpdateCmd.AddCommand(checkCmd)
	UpdateCmd.AddCommand(applyCmd)
	UpdateCmd.AddCommand(statusCmd)
	UpdateCmd.AddCommand(snapshotCmd)
}

var snapshotCmd = &cobra.Command{
	Use:   "snapshot",
	Short: "Show current config snapshot",
	Long:  `Display the current server configuration snapshot from the control plane.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		printer := ui.GetPrinter(ctx)

		basePath := filepath.Join(os.Getenv("HOME"), ".underleaf")
		store := policy_manager.NewStore(basePath, false)

		snapshot, err := store.LoadSnapshot()
		if err != nil {
			return fmt.Errorf("failed to load snapshot: %w", err)
		}

		if snapshot == nil {
			printer.PrintWarning("No config snapshot found")
			printer.Print("Run 'ufctl register' to download server configuration")
			return nil
		}

		printer.Print(fmt.Sprintf("Server ID:    %s", snapshot.ServerID))
		printer.Print(fmt.Sprintf("Version:      %d", snapshot.Version))
		printer.Print(fmt.Sprintf("Hash:         %s", snapshot.Hash[:16]+"..."))
		printer.Print(fmt.Sprintf("Timestamp:    %s", snapshot.Timestamp.Format("2006-01-02 15:04:05")))
		printer.Print(fmt.Sprintf("Age:          %s", snapshot.Age().Round(time.Second)))

		if len(snapshot.Payload) > 0 {
			printer.Print("\nConfiguration:")
			for key, value := range snapshot.Payload {
				printer.Print(fmt.Sprintf("  %s: %v", key, value))
			}
		}

		return nil
	},
}

// getDesiredVersion reads the desired software_version from the server config snapshot
func getDesiredVersion() string {
	// Load the snapshot from disk (contains server config from control plane)
	basePath := filepath.Join(os.Getenv("HOME"), ".underleaf")
	store := policy_manager.NewStore(basePath, false) // false = CLI mode

	snapshot, err := store.LoadSnapshot()
	if err != nil || snapshot == nil {
		return "auto-update" // Default if no snapshot available
	}

	// Extract software_version from server config
	if versionRaw, ok := snapshot.Payload["software_version"]; ok {
		if version, ok := versionRaw.(string); ok && version != "" {
			return version
		}
	}

	return "auto-update" // Default if not set in server config
}
