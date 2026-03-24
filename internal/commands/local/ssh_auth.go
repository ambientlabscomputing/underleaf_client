package local

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net"
	"os"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/logging"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// runSSHAuth handles SSH-based authentication
func runSSHAuth(cmd *cobra.Command, serverID string) error {
	ctx := cmd.Context()
	logger := logging.GetLogger(ctx)
	printer := ui.GetPrinter(ctx)

	if serverID == "" {
		return fmt.Errorf("--server-id is required when using --use-ssh")
	}

	// Get dependencies
	deps := utils.NewDependencyManager(ctx)
	if deps == nil {
		return fmt.Errorf("failed to create dependencies")
	}

	config := deps.ConfigClient
	authClient := deps.CPlaneClient.Auth

	printer.Print(titleStyle.Render("🔐 Underleaf SSH Authentication"))
	printer.Print("")

	// Step 1: Connect to SSH agent
	printer.Print("Step 1: Locating SSH agent...")
	sshAgentConn, err := getSSHAgent()
	if err != nil {
		return formatSSHError(err)
	}
	defer func() {
		if sshAgentConn != nil {
			sshAgentConn.Close()
		}
	}()

	agentClient := agent.NewClient(sshAgentConn)

	// Step 2: List available keys
	printer.Print("Step 2: Retrieving SSH keys from agent...")
	keys, err := agentClient.List()
	if err != nil {
		return fmt.Errorf("failed to list SSH keys: %w", err)
	}

	if len(keys) == 0 {
		return formatNoKeysError()
	}

	printer.PrintSuccess(fmt.Sprintf("Found %d SSH key(s) in agent", len(keys)))

	// Step 3: Request challenge from server
	printer.Print("Step 3: Requesting challenge from server...")
	challengeResp, err := authClient.RequestSSHChallenge(ctx, serverID)
	if err != nil {
		return fmt.Errorf("failed to request SSH challenge: %w", err)
	}

	printer.PrintSuccess("Received challenge from server")

	// Decode the challenge nonce
	challengeBytes, err := base64.StdEncoding.DecodeString(challengeResp.Challenge)
	if err != nil {
		return fmt.Errorf("failed to decode challenge: %w", err)
	}

	// Step 4: Try signing with available keys
	printer.Print("Step 4: Signing challenge with SSH key...")
	var signature string
	var usedKeyFingerprint string

	for _, key := range keys {
		// Parse the public key from the blob to compute fingerprint
		pubKey, err := ssh.ParsePublicKey(key.Blob)
		if err != nil {
			logger.Debug("failed to parse public key", "key_comment", key.Comment, "error", err)
			continue
		}

		// Try to sign with this key
		sig, err := agentClient.Sign(pubKey, challengeBytes)
		if err != nil {
			logger.Debug("failed to sign with key", "key_comment", key.Comment, "error", err)
			continue
		}

		// Successfully signed
		signature = base64.StdEncoding.EncodeToString(sig.Blob)
		usedKeyFingerprint = computeSSHFingerprint(pubKey)

		printer.PrintSuccess(fmt.Sprintf("Successfully signed with key: %s", key.Comment))
		break
	}

	if signature == "" {
		return fmt.Errorf("failed to sign challenge with any available SSH key")
	}

	// Step 5: Verify signature with server
	printer.Print("Step 5: Verifying signature with server...")
	verifyResp, err := authClient.VerifySSHSignature(ctx, serverID, challengeResp.Challenge, signature, usedKeyFingerprint)
	if err != nil {
		return fmt.Errorf("SSH authentication failed: %w", err)
	}

	printer.PrintSuccess("SSH signature verified!")

	// Step 6: Save token
	if verifyResp.Token == "" {
		return fmt.Errorf("server returned empty token")
	}

	logger.Info("Saving SSH auth token to config")
	if err := config.Set("auth.token", verifyResp.Token); err != nil {
		return fmt.Errorf("failed to save authentication token: %w", err)
	}

	// Try to save server ID if not already set
	if serverID != "" {
		if err := config.Set("local.server_id", serverID); err != nil {
			logger.Warn("failed to save server ID", "error", err)
		}
	}

	// Save organization ID from the verify response.
	// This is critical: the Spine SDK requires a non-empty OrgID and the
	// mTLS certificate embeds it.  Without this the agent cannot connect
	// to Mycelium Spine and org-scoped API queries return "no documents".
	if verifyResp.OrgID != "" {
		logger.Info("Saving organization ID from SSH auth", "org_id", verifyResp.OrgID)
		if err := config.Set("local.organization_id", verifyResp.OrgID); err != nil {
			logger.Warn("failed to save organization ID", "error", err)
		}
	} else {
		logger.Warn("SSH verify response did not include org_id — Spine connectivity may be degraded")
	}

	printer.PrintSuccess("SSH Authentication successful!")
	printer.PrintSuccess(fmt.Sprintf("Short-lived API token obtained (expires in %d seconds)", verifyResp.ExpiresIn))

	return nil
}

// getSSHAgent attempts to connect to the SSH agent
func getSSHAgent() (net.Conn, error) {
	sshAuthSock := os.Getenv("SSH_AUTH_SOCK")
	if sshAuthSock == "" {
		return nil, fmt.Errorf("SSH_AUTH_SOCK environment variable not set")
	}

	conn, err := net.Dial("unix", sshAuthSock)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SSH agent at %s: %w", sshAuthSock, err)
	}

	return conn, nil
}

// computeSSHFingerprint computes the SHA256 fingerprint of an SSH public key
func computeSSHFingerprint(pubKey ssh.PublicKey) string {
	hash := pubKey.Marshal()
	h := sha256.Sum256(hash)
	return "SHA256:" + base64.StdEncoding.EncodeToString(h[:])
}

// formatSSHError formats an SSH connection error with helpful guidance
func formatSSHError(err error) error {
	return fmt.Errorf(`
❌ SSH Agent Error: %w

The --use-ssh flag requires a running SSH agent with your server's private key loaded.

To set up an SSH agent:

  macOS:   eval "$(ssh-agent -s)" && ssh-add ~/.ssh/id_ed25519 (or id_rsa)
  Linux:   eval "$(ssh-agent -s)" && ssh-add ~/.ssh/id_ed25519 (or id_rsa)
  Windows: ssh-add (in PowerShell or Git Bash with ssh-agent running)

Ensure the corresponding public key was registered with:
  ufctl servers create -n <name> -k ~/.ssh/id_ed25519.pub

Then try again:
  ufctl auth login --use-ssh --server-id <server-id>

Alternatively, authenticate without SSH:
  ufctl auth login
`, err)
}

// formatNoKeysError returns a helpful error when no SSH keys are found
func formatNoKeysError() error {
	return fmt.Errorf(`
❌ No SSH Keys Found in Agent

The SSH agent is running but has no keys loaded.

To add your SSH key:
  ssh-add ~/.ssh/id_ed25519    (or ~/.ssh/id_rsa)

To list currently loaded keys:
  ssh-add -l

If you don't have an SSH key, create one:
  ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519

Then authenticate:
  ufctl auth login --use-ssh --server-id <server-id>
`)
}
