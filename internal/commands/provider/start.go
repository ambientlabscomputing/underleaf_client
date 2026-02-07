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

var startCmd = &cobra.Command{
	Use:   "start <provider-id>",
	Short: "Start a provider",
	Long:  `Start an installed provider. The provider must be installed first.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runStart,
}

func runStart(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	logger := logging.GetLogger(ctx)
	printer := ui.GetPrinter(ctx)

	providerID := args[0]
	agentPort := getAgentPort(ctx)

	printer.Print(fmt.Sprintf("\nStarting provider: %s", providerID))

	// For now, we use the ensure endpoint which will start the provider if installed
	url := fmt.Sprintf("http://localhost:%d/api/v1/capabilities/ensure", agentPort)

	payload := map[string]interface{}{
		"capability_id": providerID,
		"version_range": "*",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		logger.Error("failed to connect to agent", "error", err)
		return fmt.Errorf("failed to connect to agent: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		logger.Error("start failed", "status", resp.StatusCode, "body", string(respBody))
		return fmt.Errorf("start failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	printer.PrintSuccess(fmt.Sprintf("✓ Provider '%s' started successfully", providerID))
	printer.Print(fmt.Sprintf("\nUse 'ufctl provider status %s' to check its status", providerID))

	return nil
}
