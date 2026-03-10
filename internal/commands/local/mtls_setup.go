package local

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/crypto"
	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
)

// SetupMTLSCertificate performs the complete mTLS setup flow:
// 1. Generate private key
// 2. Generate CSR
// 3. Submit CSR to server API
// 4. Save signed certificate
// 5. Update config with certificate paths
func SetupMTLSCertificate(ctx context.Context, printer *ui.Printer, configClient policy_manager.ConfigClient, serverID, orgID, orgName string) error {
	printer.PrintInfo("\n=== Setting up mTLS Certificate ===\n")

	// Get cert paths
	basePath := policy_manager.GetBasePath(false)
	keyPath, csrPath, certPath := crypto.GetCertPaths(basePath, serverID)

	// Check if certificate already exists
	if _, err := os.Stat(certPath); err == nil {
		printer.PrintSuccess("✓ mTLS certificate already exists at: " + certPath)
		printer.PrintInfo("✓ mTLS authentication is ready to use")
		return nil
	}

	// Step 1: Generate private key
	printer.PrintInfo("[1/4] Generating ECDSA P-256 private key...")
	privateKey, privateKeyPEM, err := crypto.GeneratePrivateKey()
	if err != nil {
		return fmt.Errorf("failed to generate private key: %w", err)
	}

	// Save private key
	if err := crypto.SavePrivateKey(privateKeyPEM, keyPath); err != nil {
		return fmt.Errorf("failed to save private key: %w", err)
	}
	printer.PrintSuccess(fmt.Sprintf("  → Saved to: %s", keyPath))

	// Step 2: Generate CSR
	printer.PrintInfo("[2/4] Generating Certificate Signing Request...")
	csrPEM, err := crypto.GenerateCSR(privateKey, serverID, orgID, orgName)
	if err != nil {
		return fmt.Errorf("failed to generate CSR: %w", err)
	}

	if err := crypto.SaveCSR(csrPEM, csrPath); err != nil {
		return fmt.Errorf("failed to save CSR: %w", err)
	}
	printer.PrintSuccess(fmt.Sprintf("  → Saved to: %s", csrPath))

	// Step 3: Submit CSR to server API
	printer.PrintInfo("[3/4] Submitting CSR to server API for signing...")

	// Get API base URL
	baseURL, ok := configClient.Get("api.base_url")
	if !ok || baseURL == nil {
		return fmt.Errorf("api.base_url not configured")
	}

	// Get auth token
	token, ok := configClient.Get("auth.token")
	if !ok || token == nil {
		return fmt.Errorf("auth token not found - please login first")
	}

	// Submit CSR to API
	endpoint := fmt.Sprintf("%s/servers/%s/csr", baseURL.(string), serverID)
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(csrPEM))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token.(string))
	req.Header.Set("Content-Type", "text/plain")

	// Add organization context if available
	if orgID != "" {
		req.Header.Set("X-Organization-ID", orgID)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to submit CSR: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Step 4: Read and parse JSON response, then save signed certificate
	printer.PrintInfo("[4/4] Saving signed certificate...")
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read certificate: %w", err)
	}
	var csrResponse struct {
		Certificate string `json:"certificate"`
	}
	if err := json.Unmarshal(bodyBytes, &csrResponse); err != nil {
		return fmt.Errorf("failed to parse CSR response: %w", err)
	}
	certPEM := []byte(csrResponse.Certificate)

	if err := crypto.SaveCertificate(certPEM, certPath); err != nil {
		return fmt.Errorf("failed to save certificate: %w", err)
	}
	printer.PrintSuccess(fmt.Sprintf("  → Saved to: %s", certPath))

	// Step 5: Update config with certificate paths
	configClient.Set("local.mtls.certificate_path", certPath)
	configClient.Set("local.mtls.private_key_path", keyPath)

	printer.PrintSuccess("\n✓ mTLS certificate setup complete!")
	printer.PrintInfo("✓ The agent will automatically use mTLS authentication")

	return nil
}
