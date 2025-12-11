package local

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/config_manager"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

// RegisterCmd represents the register command
// `ufctl local register` prompts the user for a name and registers a new edge server
// This command is idempotent - running it multiple times will reuse the existing registration
var RegisterCmd = &cobra.Command{
	Use:   "register",
	Short: "Register a new Underleaf edge server",
	Long: `Registers a new Underleaf edge server with the control plane.

This command is idempotent - if a server is already registered in the local config,
it will verify the registration is still valid on the backend and reuse it.
This makes it safe to run repeatedly and is the foundation for mTLS certificate generation.

Use the --existing flag to claim an existing server registration that hasn't checked in 
for at least 5 minutes, instead of creating a new server.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		// Get the --existing flag
		useExisting, _ := cmd.Flags().GetBool("existing")

		// If using --existing flag, handle the existing server flow
		if useExisting {
			deps.Printer.PrintInfo("Looking up existing server...")

			// Prompt for server name or ID to lookup
			nameOrID, err := ui.PromptInput("Enter server name or ID to claim:", "")
			if err != nil {
				deps.Printer.PrintError("Failed to get input: " + err.Error())
				return err
			}

			if nameOrID == "" {
				deps.Printer.PrintWarning("Registration cancelled - no server name or ID provided")
				return nil
			}

			// Find the existing server (validates it hasn't checked in within 5 minutes)
			server, err := deps.ServerSvc.FindExistingServer(ctx, nameOrID)
			if err != nil {
				deps.Printer.PrintError("Failed to find server: " + err.Error())
				return err
			}

			// Confirm before claiming
			deps.Printer.PrintInfo("\nFound server:")
			deps.Printer.PrintInfo("  ID: " + server.ID)
			deps.Printer.PrintInfo("  Name: " + server.Name)
			if server.LastCheckIn != nil && server.LastCheckIn.Valid {
				deps.Printer.PrintInfo("  Last Check-In: " + server.LastCheckIn.Time.Format("2006-01-02 15:04:05"))
			} else {
				deps.Printer.PrintInfo("  Last Check-In: Never")
			}

			confirm, err := ui.PromptInput("\nClaim this server? (yes/no):", "no")
			if err != nil {
				deps.Printer.PrintError("Failed to get confirmation: " + err.Error())
				return err
			}

			if confirm != "yes" && confirm != "y" {
				deps.Printer.PrintWarning("Registration cancelled")
				return nil
			}

			// Download the server configuration
			if err := deps.ServerSvc.DownloadServerConfig(ctx, server); err != nil {
				deps.Printer.PrintError("Failed to download server configuration: " + err.Error())
				return err
			}

			deps.Printer.PrintSuccess("Successfully claimed existing server '" + server.Name + "'!")

			// Download and save config snapshot
			if err := downloadAndSaveConfigSnapshot(ctx, &deps.Printer, server.ID); err != nil {
				deps.Printer.PrintWarning("Failed to download config snapshot: " + err.Error())
				deps.Printer.PrintInfo("Config will be downloaded when agent starts")
			}

			// Setup mTLS certificate after claiming
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

			// Setup mTLS certificate
			if err := SetupMTLSCertificate(ctx, &deps.Printer, deps.ConfigClient, serverID, orgID, orgName); err != nil {
				deps.Printer.PrintWarning("Failed to setup mTLS certificate: " + err.Error())
				deps.Printer.PrintInfo("You can manually setup mTLS later with: ufctl local generate-csr && ufctl local submit-csr")
			}

			deps.Printer.PrintSuccess("\n✓ Server claimed successfully! Your edge server is ready to use.")
			return nil
		}

		// Standard registration flow (creating a new server)
		// Check if already registered
		existingID, hasID := deps.ConfigClient.Get("local.server_id")
		existingName, hasName := deps.ConfigClient.Get("local.server_name")

		if hasID && existingID != nil && hasName && existingName != nil {
			deps.Printer.PrintInfo("Server already registered:")
			deps.Printer.PrintInfo("  ID: " + existingID.(string))
			deps.Printer.PrintInfo("  Name: " + existingName.(string))
			deps.Printer.PrintInfo("\nVerifying registration with control plane...")
		}

		// Prompt for server name
		defaultName := "my-edge-server"
		if hasName && existingName != nil {
			if name, ok := existingName.(string); ok && name != "" {
				defaultName = name
			}
		}

		name, err := ui.PromptInput("Enter a name for this edge server:", defaultName)
		if err != nil {
			deps.Printer.PrintError("Failed to get input: " + err.Error())
			return err
		}

		if name == "" {
			deps.Printer.PrintWarning("Registration cancelled - no name provided")
			return nil
		}

		// RegisterServer is now idempotent
		if err := deps.ServerSvc.RegisterServer(ctx, name); err != nil {
			deps.Printer.PrintError("Failed to register edge server: " + err.Error())
			return err
		}

		deps.Printer.PrintSuccess("Edge server '" + name + "' registered successfully!")

		// Download and save config snapshot
		serverIDRaw, _ := deps.ConfigClient.Get("local.server_id")
		if serverIDRaw != nil {
			if serverID, ok := serverIDRaw.(string); ok && serverID != "" {
				if err := downloadAndSaveConfigSnapshot(ctx, &deps.Printer, serverID); err != nil {
					deps.Printer.PrintWarning("Failed to download config snapshot: " + err.Error())
					deps.Printer.PrintInfo("Config will be downloaded when agent starts")
				}
			}
		}

		// Automatically setup mTLS certificate after registration
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

		// Setup mTLS certificate (generates key, CSR, and gets it signed)
		if err := SetupMTLSCertificate(ctx, &deps.Printer, deps.ConfigClient, serverID, orgID, orgName); err != nil {
			deps.Printer.PrintWarning("Failed to setup mTLS certificate: " + err.Error())
			deps.Printer.PrintInfo("You can manually setup mTLS later with: ufctl local generate-csr && ufctl local submit-csr")
		}

		deps.Printer.PrintSuccess("\n✓ Registration complete! Your edge server is ready to use.")

		return nil
	},
}

func init() {
	RegisterCmd.Flags().Bool("existing", false, "Claim an existing server registration instead of creating a new one")
}

// downloadAndSaveConfigSnapshot fetches the server config from control plane and saves it locally
func downloadAndSaveConfigSnapshot(ctx context.Context, printer *ui.Printer, serverID string) error {
	printer.PrintInfo("Downloading server configuration...")

	// Initialize dependencies
	deps := utils.NewDependencyManager(ctx)

	// Get server config from control plane
	config, version, err := deps.CPlaneClient.Config.GetServerConfig(ctx, serverID)
	if err != nil {
		return err
	}

	// Create config store (use home directory for CLI)
	basePath := filepath.Join(os.Getenv("HOME"), ".underleaf")
	store := config_manager.NewStore(basePath, false) // false = CLI mode

	// Create and save snapshot
	snapshot := config_manager.NewConfigSnapshot(serverID, version, config)
	if err := snapshot.Validate(); err != nil {
		return err
	}

	if err := store.SaveSnapshot(snapshot); err != nil {
		return err
	}

	printer.PrintSuccess(fmt.Sprintf("Server configuration downloaded (version %d)", version))
	return nil
}
