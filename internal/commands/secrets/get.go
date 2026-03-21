package secrets

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/ambientlabscomputing/underleaf_client/internal/agent"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

var (
	getVersion      int
	getOutputFormat string
)

var getCmd = &cobra.Command{
	Use:   "get <name>",
	Short: "Retrieve a secret value from the local agent",
	Long: `Retrieve and display a decrypted secret value from the local cluster agent.
The value is fetched directly from the agent — it never transits the cloud.

  ufctl secrets get DATABASE_PASSWORD
  ufctl secrets get DATABASE_PASSWORD --version 2`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		printer := ui.GetPrinter(ctx)
		name := args[0]

		port := getAgentPort(ctx)
		client := agent.NewClient(port)

		path := fmt.Sprintf("/api/v1/secrets/get/%s", url.PathEscape(name))
		if getVersion > 0 {
			path = fmt.Sprintf("%s?version=%d", path, getVersion)
		}

		resp, err := client.DoRequest("GET", path, nil)
		if err != nil {
			return fmt.Errorf("failed to retrieve secret: %w", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(resp, &result); err != nil {
			return fmt.Errorf("failed to parse agent response: %w", err)
		}

		if getOutputFormat == "json" {
			fmt.Println(string(resp))
			return nil
		}

		printer.PrintSuccess(fmt.Sprintf("Name:    %s", name))
		if v, ok := result["version"]; ok {
			printer.Print(fmt.Sprintf("Version: %v", v))
		}
		if data, ok := result["data"]; ok {
			printer.Print(fmt.Sprintf("Value:   %v", data))
		} else if value, ok := result["value"]; ok {
			printer.Print(fmt.Sprintf("Value:   %v", value))
		}

		return nil
	},
}

func init() {
	getCmd.Flags().IntVar(&getVersion, "version", 0, "Secret version to retrieve (default: latest)")
	getCmd.Flags().StringVarP(&getOutputFormat, "output", "o", "", "Output format: 'json' for machine-readable output")
	getCmd.Flags().IntVarP(&agentPort, "port", "p", 0, "Agent port (default: 2240)")
}
