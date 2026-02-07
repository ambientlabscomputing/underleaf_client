package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/logging"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install <provider-id>",
	Short: "Install a provider",
	Long: `Install a provider from the capability registry.
	
The provider will be downloaded and installed but not started automatically.
Use 'ufctl provider start <provider-id>' to start it after installation.`,
	Args: cobra.ExactArgs(1),
	RunE: runInstall,
}

func runInstall(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	logger := logging.GetLogger(ctx)
	printer := ui.GetPrinter(ctx)

	providerID := args[0]
	agentPort := getAgentPort(ctx)

	printer.Print(fmt.Sprintf("\nInstalling provider: %s", providerID))

	// Call agent API to install provider by provider ID
	url := fmt.Sprintf("http://localhost:%d/api/v1/providers/install", agentPort)

	payload := map[string]interface{}{
		"provider_id": providerID,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	client := &http.Client{Timeout: 60 * time.Second} // Longer timeout for installation
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		logger.Error("failed to connect to agent", "error", err)
		return fmt.Errorf("failed to connect to agent: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		logger.Error("installation failed", "status", resp.StatusCode, "body", string(respBody))
		return fmt.Errorf("installation failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var response map[string]interface{}
	if err := json.Unmarshal(respBody, &response); err != nil {
		logger.Warn("failed to parse response", "error", err)
	}

	printer.PrintSuccess(fmt.Sprintf("✓ Provider '%s' installed successfully", providerID))
	printer.Print(fmt.Sprintf("\nUse 'ufctl provider start %s' to start the provider", providerID))

	return nil
}
