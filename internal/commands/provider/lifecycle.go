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

var stopCmd = &cobra.Command{
	Use:   "stop <provider-id>",
	Short: "Stop a running provider",
	Long:  `Stop a running provider. The provider will remain installed.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runStop,
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall <provider-id>",
	Short: "Uninstall a provider",
	Long:  `Uninstall a provider, removing it from the system. The provider will be stopped first if running.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runUninstall,
}

var restartCmd = &cobra.Command{
	Use:   "restart <provider-id>",
	Short: "Restart a provider",
	Long:  `Restart a provider by stopping and starting it again.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runRestart,
}

var logsCmd = &cobra.Command{
	Use:   "logs <provider-id>",
	Short: "Show provider logs",
	Long:  `Display logs from a provider.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runLogs,
}

func runStop(cmd *cobra.Command, args []string) error {
	printer := ui.GetPrinter(cmd.Context())
	providerID := args[0]

	printer.PrintWarning(fmt.Sprintf("\nStop functionality for provider '%s' not yet implemented", providerID))
	printer.Print("\nThis will be available in a future update.")
	printer.Print("For now, you can restart the Underleaf Agent to stop all providers.")

	return nil
}

func runUninstall(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	logger := logging.GetLogger(ctx)
	printer := ui.GetPrinter(ctx)
	providerID := args[0]
	agentPort := getAgentPort(ctx)

	printer.Print(fmt.Sprintf("\nUninstalling provider: %s", providerID))

	url := fmt.Sprintf("http://localhost:%d/api/v1/providers/uninstall", agentPort)
	payload := map[string]interface{}{
		"provider_id": providerID,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		logger.Error("failed to connect to agent", "error", err)
		return fmt.Errorf("failed to connect to agent: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		logger.Error("uninstall failed", "status", resp.StatusCode, "body", string(respBody))
		return fmt.Errorf("uninstall failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	printer.PrintSuccess(fmt.Sprintf("✓ Provider '%s' uninstalled successfully", providerID))
	printer.Print("\nProvider data is stored in ~/.underleaf/providers/")

	return nil
}

func runRestart(cmd *cobra.Command, args []string) error {
	printer := ui.GetPrinter(cmd.Context())
	providerID := args[0]

	printer.PrintWarning(fmt.Sprintf("\nRestart functionality for provider '%s' not yet implemented", providerID))
	printer.Print("\nThis will be available in a future update.")
	printer.Print("For now, you can restart the Underleaf Agent to restart all providers.")

	return nil
}

func runLogs(cmd *cobra.Command, args []string) error {
	printer := ui.GetPrinter(cmd.Context())
	providerID := args[0]

	printer.PrintWarning(fmt.Sprintf("\nLogs functionality for provider '%s' not yet implemented", providerID))
	printer.Print("\nThis will be available in a future update.")
	printer.Print("Provider logs may be available in the Underleaf Agent logs.")

	return nil
}
