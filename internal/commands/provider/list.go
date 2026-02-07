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

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed providers",
	Long:  `List all providers currently installed on the Underleaf Agent`,
	RunE:  runList,
}

func runList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	logger := logging.GetLogger(ctx)
	printer := ui.GetPrinter(ctx)

	// Get agent port from context or use default
	agentPort := getAgentPort(ctx)

	// Call agent API
	url := fmt.Sprintf("http://localhost:%d/api/v1/providers/installed", agentPort)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		logger.Error("failed to connect to agent", "error", err, "port", agentPort)
		return fmt.Errorf("failed to connect to agent on port %d: %w", agentPort, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		logger.Error("agent returned error", "status", resp.StatusCode, "body", string(body))
		return fmt.Errorf("agent returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var response struct {
		Providers []ProviderInfo `json:"providers"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		logger.Error("failed to parse response", "error", err)
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Display results
	if len(response.Providers) == 0 {
		printer.PrintWarning("No providers installed")
		printer.Print("\nUse 'ufctl provider install <provider-id>' to install a provider")
		return nil
	}

	// Print provider list
	printer.Print(fmt.Sprintf("\nInstalled Providers (%d):\n", len(response.Providers)))

	for _, p := range response.Providers {
		printer.Print(fmt.Sprintf("  • %s", p.ProviderID))
		printer.Print(fmt.Sprintf("    Version:      %s", p.Version))
		printer.Print(fmt.Sprintf("    State:        %s", p.State))

		if len(p.Capabilities) > 0 {
			capList := ""
			for i, cap := range p.Capabilities {
				if i > 0 {
					capList += ", "
				}
				capList += cap
			}
			printer.Print(fmt.Sprintf("    Capabilities: %s", capList))
		}

		printer.Print(fmt.Sprintf("    Installed:    %s", p.InstalledAt.Format("2006-01-02 15:04")))
		printer.Print("")
	}

	printer.Print("Use 'ufctl provider status <provider-id>' for detailed information")

	return nil
}
