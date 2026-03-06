package servers

import (
	"context"
	"fmt"
	"os"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
)

var (
	createName          string
	createPublicKeyPath string
)

var CreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Register and pre-configure a new server",
	Long: `Create a new server registration with optional SSH public key setup for SSH-based authentication.

Examples:
  ufctl servers create -n my-server
  ufctl servers create -n my-server -k ~/.ssh/id_ed25519.pub`,
	RunE: runCreateServer,
}

func init() {
	CreateCmd.Flags().StringVarP(&createName, "name", "n", "", "Server name (required)")
	CreateCmd.Flags().StringVarP(&createPublicKeyPath, "public-key", "k", "", "Path to SSH public key file (optional)")

	CreateCmd.MarkFlagRequired("name")
}

func runCreateServer(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	deps := utils.NewDependencyManager(ctx)

	// Validate inputs
	if createName == "" {
		return fmt.Errorf("server name is required")
	}

	// Load SSH public key if provided
	var sshPublicKey *string
	if createPublicKeyPath != "" {
		// Read and validate the SSH public key file
		keyBytes, err := os.ReadFile(createPublicKeyPath)
		if err != nil {
			return fmt.Errorf("failed to read SSH public key file: %w", err)
		}

		keyStr := string(keyBytes)

		// Validate it's a proper SSH public key
		if _, _, _, _, err := ssh.ParseAuthorizedKey(keyBytes); err != nil {
			return fmt.Errorf("invalid SSH public key: %w", err)
		}

		sshPublicKey = &keyStr
	}

	// Create the server
	deps.Printer.PrintInfo("Creating server...")

	server, err := deps.ServerSvc.CreateServer(ctx, createName, sshPublicKey)
	if err != nil {
		deps.Printer.PrintError(fmt.Sprintf("Failed to create server: %s", err.Error()))
		return err
	}

	// Display the created server
	deps.Printer.PrintSuccess("Server created successfully!")
	deps.Printer.PrintInfo(fmt.Sprintf("\nServer ID:   %s", server.ID))
	deps.Printer.PrintInfo(fmt.Sprintf("Name:        %s", server.Name))
	deps.Printer.PrintInfo(fmt.Sprintf("Status:      %s", server.Status))

	if createPublicKeyPath != "" && len(server.SSHPublicKeys) > 0 {
		deps.Printer.PrintInfo("\nSSH Public Keys registered:")
		for _, key := range server.SSHPublicKeys {
			deps.Printer.PrintInfo(fmt.Sprintf("  - Fingerprint: %s", key.Fingerprint))
			if key.Label != "" {
				deps.Printer.PrintInfo(fmt.Sprintf("    Label:       %s", key.Label))
			}
		}
	}

	deps.Printer.PrintInfo(fmt.Sprintf("\nTo authenticate this server and bootstrap mTLS via SSH:"))
	deps.Printer.PrintInfo(fmt.Sprintf("  ufctl auth login --use-ssh --server-id %s\n", server.ID))

	return nil
}
