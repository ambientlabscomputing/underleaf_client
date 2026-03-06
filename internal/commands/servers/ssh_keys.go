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
	sshKeyLabel      string
	sshKeyPublicPath string
)

// SSHKeysCmd is the parent command for SSH key management
var SSHKeysCmd = &cobra.Command{
	Use:   "ssh-keys",
	Short: "Manage SSH public keys for a server",
	Long:  "Add, list, and remove SSH public keys for SSH-based server authentication.",
}

// SSHKeysAddCmd adds an SSH public key to a server
var SSHKeysAddCmd = &cobra.Command{
	Use:   "add <server-id>",
	Short: "Add an SSH public key to a server",
	Long: `Add an SSH public key to a server for SSH-based authentication.

Examples:
  ufctl servers ssh-keys add <server-id> -k ~/.ssh/id_ed25519.pub
  ufctl servers ssh-keys add <server-id> -k ~/.ssh/id_ed25519.pub --label "prod-node"`,
	Args: cobra.ExactArgs(1),
	RunE: runSSHKeysAdd,
}

// SSHKeysListCmd lists SSH public keys for a server
var SSHKeysListCmd = &cobra.Command{
	Use:   "list <server-id>",
	Short: "List SSH public keys for a server",
	Long: `List all SSH public keys registered for a server.

Example:
  ufctl servers ssh-keys list <server-id>`,
	Args: cobra.ExactArgs(1),
	RunE: runSSHKeysList,
}

// SSHKeysRemoveCmd removes an SSH public key from a server
var SSHKeysRemoveCmd = &cobra.Command{
	Use:   "remove <server-id> <key-id>",
	Short: "Remove an SSH public key from a server",
	Long: `Remove an SSH public key from a server by its key ID.
Use 'ufctl servers ssh-keys list <server-id>' to find the key ID.

Example:
  ufctl servers ssh-keys remove <server-id> <key-id>`,
	Args: cobra.ExactArgs(2),
	RunE: runSSHKeysRemove,
}

func init() {
	SSHKeysAddCmd.Flags().StringVarP(&sshKeyPublicPath, "public-key", "k", "", "Path to SSH public key file (required)")
	SSHKeysAddCmd.Flags().StringVar(&sshKeyLabel, "label", "", "Optional label for this key")
	SSHKeysAddCmd.MarkFlagRequired("public-key")

	SSHKeysCmd.AddCommand(SSHKeysAddCmd)
	SSHKeysCmd.AddCommand(SSHKeysListCmd)
	SSHKeysCmd.AddCommand(SSHKeysRemoveCmd)
}

func runSSHKeysAdd(cmd *cobra.Command, args []string) error {
	serverID := args[0]
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	deps := utils.NewDependencyManager(ctx)

	// Read and validate the SSH public key file
	keyBytes, err := os.ReadFile(sshKeyPublicPath)
	if err != nil {
		return fmt.Errorf("failed to read SSH public key file: %w", err)
	}

	// Validate the format
	if _, _, _, _, err := ssh.ParseAuthorizedKey(keyBytes); err != nil {
		return fmt.Errorf("invalid SSH public key format: %w", err)
	}

	deps.Printer.PrintInfo(fmt.Sprintf("Adding SSH key to server %s...", serverID))

	key, err := deps.ServerSvc.AddSSHKey(ctx, serverID, string(keyBytes), sshKeyLabel)
	if err != nil {
		deps.Printer.PrintError(fmt.Sprintf("Failed to add SSH key: %s", err.Error()))
		return err
	}

	deps.Printer.PrintSuccess("SSH public key added successfully!")
	deps.Printer.PrintInfo(fmt.Sprintf("\nKey ID:      %s", key.ID))
	deps.Printer.PrintInfo(fmt.Sprintf("Fingerprint: %s", key.Fingerprint))
	if key.Label != "" {
		deps.Printer.PrintInfo(fmt.Sprintf("Label:       %s", key.Label))
	}
	deps.Printer.PrintInfo(fmt.Sprintf("Added:       %s", key.AddedAt))

	return nil
}

func runSSHKeysList(cmd *cobra.Command, args []string) error {
	serverID := args[0]
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	deps := utils.NewDependencyManager(ctx)

	keys, err := deps.ServerSvc.ListSSHKeys(ctx, serverID)
	if err != nil {
		deps.Printer.PrintError(fmt.Sprintf("Failed to list SSH keys: %s", err.Error()))
		return err
	}

	if len(keys) == 0 {
		deps.Printer.PrintInfo("No SSH public keys registered for this server.")
		deps.Printer.PrintInfo("\nTo add a key:")
		deps.Printer.PrintInfo("  ufctl servers ssh-keys add " + serverID + " -k ~/.ssh/id_ed25519.pub")
		return nil
	}

	deps.Printer.PrintInfo(fmt.Sprintf("SSH keys for server %s:\n", serverID))
	for i, key := range keys {
		deps.Printer.PrintInfo(fmt.Sprintf("[%d] Key ID:      %s", i+1, key.ID))
		deps.Printer.PrintInfo(fmt.Sprintf("    Fingerprint: %s", key.Fingerprint))
		if key.Label != "" {
			deps.Printer.PrintInfo(fmt.Sprintf("    Label:       %s", key.Label))
		}
		deps.Printer.PrintInfo(fmt.Sprintf("    Added:       %s", key.AddedAt))
		if i < len(keys)-1 {
			fmt.Println()
		}
	}

	return nil
}

func runSSHKeysRemove(cmd *cobra.Command, args []string) error {
	serverID := args[0]
	keyID := args[1]
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	deps := utils.NewDependencyManager(ctx)

	deps.Printer.PrintInfo(fmt.Sprintf("Removing SSH key %s from server %s...", keyID, serverID))

	if err := deps.ServerSvc.RemoveSSHKey(ctx, serverID, keyID); err != nil {
		deps.Printer.PrintError(fmt.Sprintf("Failed to remove SSH key: %s", err.Error()))
		return err
	}

	deps.Printer.PrintSuccess("SSH public key removed successfully.")

	return nil
}
