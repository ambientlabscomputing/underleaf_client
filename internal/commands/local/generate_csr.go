package local

import (
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
	"github.com/ambientlabscomputing/underleaf_client/internal/crypto"
	"github.com/spf13/cobra"
)

// GenerateCSRCmd generates a Certificate Signing Request for mTLS
var GenerateCSRCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a Certificate Signing Request (CSR) for mTLS authentication",
	Long: `Generates a private key and Certificate Signing Request (CSR) for mTLS authentication.

This command:
1. Checks if the server is registered (requires 'ufctl start' first)
2. Generates an ECDSA P-256 private key
3. Creates a CSR with the server ID as the Common Name
4. Saves the private key and CSR to disk

The private key is saved with restricted permissions (0600) and should never be shared.
The CSR will be sent to the control plane CA for signing in the next step.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		// Verify server is registered
		serverIDRaw, hasID := deps.ConfigClient.Get("local.server_id")
		if !hasID || serverIDRaw == nil {
			deps.Printer.PrintError("Server not registered. Please run 'ufctl start' first.")
			return fmt.Errorf("server not registered")
		}

		serverID, ok := serverIDRaw.(string)
		if !ok || serverID == "" {
			deps.Printer.PrintError("Invalid server ID in config")
			return fmt.Errorf("invalid server ID")
		}

		// Get organization info
		orgIDRaw, _ := deps.ConfigClient.Get("local.organization_id")
		orgNameRaw, _ := deps.ConfigClient.Get("local.organization_name")

		orgID := ""
		orgName := "Underleaf"

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

		deps.Printer.PrintInfo(fmt.Sprintf("Generating CSR for server: %s", serverID))
		deps.Printer.PrintInfo(fmt.Sprintf("Organization: %s (%s)", orgName, orgID))

		// Get cert paths
		basePath := policy_manager.GetBasePath(false) // false = CLI mode
		keyPath, csrPath, _ := crypto.GetCertPaths(basePath, serverID)

		// Check if key already exists
		if _, err := crypto.LoadPrivateKey(keyPath); err == nil {
			deps.Printer.PrintWarning(fmt.Sprintf("Private key already exists at: %s", keyPath))
			deps.Printer.PrintWarning("Continuing will overwrite the existing key.")
			// TODO: Add confirmation prompt
		}

		// Generate private key
		deps.Printer.PrintInfo("\n[1/3] Generating ECDSA P-256 private key...")
		privateKey, privateKeyPEM, err := crypto.GeneratePrivateKey()
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to generate private key: %s", err))
			return err
		}

		// Save private key
		deps.Printer.PrintInfo("[2/3] Saving private key...")
		if err := crypto.SavePrivateKey(privateKeyPEM, keyPath); err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to save private key: %s", err))
			return err
		}
		deps.Printer.PrintSuccess(fmt.Sprintf("  → %s (permissions: 0600)", keyPath))

		// Generate CSR
		deps.Printer.PrintInfo("[3/3] Generating Certificate Signing Request...")
		csrPEM, err := crypto.GenerateCSR(privateKey, serverID, orgID, orgName)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to generate CSR: %s", err))
			return err
		}

		// Save CSR
		if err := crypto.SaveCSR(csrPEM, csrPath); err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to save CSR: %s", err))
			return err
		}
		deps.Printer.PrintSuccess(fmt.Sprintf("  → %s", csrPath))

		// Verify CSR
		if err := crypto.VerifyCSR(csrPEM, privateKey); err != nil {
			deps.Printer.PrintWarning(fmt.Sprintf("CSR verification warning: %s", err))
		}

		// Save paths to config for later use
		if err := deps.ConfigClient.Set("local.mtls.private_key_path", keyPath); err != nil {
			deps.Printer.PrintWarning(fmt.Sprintf("Failed to save key path to config: %s", err))
		}
		if err := deps.ConfigClient.Set("local.mtls.csr_path", csrPath); err != nil {
			deps.Printer.PrintWarning(fmt.Sprintf("Failed to save CSR path to config: %s", err))
		}

		deps.Printer.PrintSuccess("\n✓ CSR generation complete!")
		deps.Printer.PrintInfo("\nNext steps:")
		deps.Printer.PrintInfo("  1. Submit the CSR to the control plane CA for signing:")
		deps.Printer.PrintInfo("     ufctl csr submit")
		deps.Printer.PrintInfo("  2. The CA will verify your identity and sign the certificate")
		deps.Printer.PrintInfo("  3. Download the signed certificate and configure mTLS")

		return nil
	},
}
