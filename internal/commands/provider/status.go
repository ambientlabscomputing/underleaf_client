package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/logging"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status <provider-id>",
	Short: "Show provider status",
	Long:  `Display detailed status information for a specific provider`,
	Args:  cobra.ExactArgs(1),
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	logger := logging.GetLogger(ctx)
	printer := ui.GetPrinter(ctx)

	providerID := args[0]
	agentPort := getAgentPort(ctx)

	// Call agent API to get all providers
	url := fmt.Sprintf("http://localhost:%d/api/v1/providers/installed", agentPort)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		logger.Error("failed to connect to agent", "error", err)
		return fmt.Errorf("failed to connect to agent: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("agent returned status %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Providers []ProviderInfo `json:"providers"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Find matching provider
	var provider *ProviderInfo
	for i := range response.Providers {
		if response.Providers[i].ProviderID == providerID {
			provider = &response.Providers[i]
			break
		}
	}

	if provider == nil {
		printer.PrintError(fmt.Sprintf("Provider '%s' not found", providerID))
		printer.Print("\nUse 'ufctl provider list' to see installed providers")
		return nil
	}

	// Display provider details
	printer.PrintSuccess(fmt.Sprintf("\nProvider: %s", provider.ProviderID))
	printer.Print(fmt.Sprintf("  Version:      %s", provider.Version))
	printer.Print(fmt.Sprintf("  State:        %s", provider.State))
	printer.Print(fmt.Sprintf("  Endpoint:     %s", provider.Endpoint))
	printer.Print(fmt.Sprintf("  Runtime ID:   %s", provider.RuntimeID))
	printer.Print(fmt.Sprintf("  Installed:    %s", provider.InstalledAt.Format(time.RFC3339)))

	if len(provider.Capabilities) > 0 {
		printer.Print("\n  Capabilities:")
		for _, cap := range provider.Capabilities {
			printer.Print(fmt.Sprintf("    - %s", cap))
		}
	}

	if len(provider.Metadata) > 0 {
		printer.Print("\n  Metadata:")
		for key, value := range provider.Metadata {
			printer.Print(fmt.Sprintf("    %s: %s", key, value))
		}
	}

	printer.Print("")

	return nil
}
