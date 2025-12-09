package local

import (
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
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
This makes it safe to run repeatedly and is the foundation for mTLS certificate generation.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

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

		// Automatically setup mTLS certificate after registration
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

		// Setup mTLS certificate (generates key, CSR, and gets it signed)
		if err := SetupMTLSCertificate(ctx, &deps.Printer, deps.ConfigClient, serverID, orgID, orgName); err != nil {
			deps.Printer.PrintWarning("Failed to setup mTLS certificate: " + err.Error())
			deps.Printer.PrintInfo("You can manually setup mTLS later with: ufctl local generate-csr && ufctl local submit-csr")
		}

		deps.Printer.PrintSuccess("\n✓ Registration complete! Your edge server is ready to use.")

		return nil
	},
}
