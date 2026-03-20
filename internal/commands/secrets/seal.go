package secrets

import (
	"encoding/json"
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/agent"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

// sealCmd is the root for vault lifecycle subcommands (init, seal, unseal, seal-status).
var sealCmd = &cobra.Command{
	Use:   "vault",
	Short: "Manage the local secret vault lifecycle",
	Long: `Commands for managing the lifecycle of the local secret vault on the agent.

The vault must be initialized before any secrets can be stored. After init,
the vault is sealed by default and must be unsealed with the master key shares
generated during init.

  ufctl secrets vault init
  ufctl secrets vault unseal
  ufctl secrets vault seal
  ufctl secrets vault status`,
}

var (
	sealShares    int
	sealThreshold int
)

var vaultInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize the local secret vault",
	Long: `Initialize the local secret vault. Generates master key shares and returns
them for distribution. The vault is sealed after initialization and must be
unsealed before secrets can be stored.

  ufctl secrets vault init --shares 5 --threshold 3`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		printer := ui.GetPrinter(ctx)

		port := getAgentPort(ctx)
		client := agent.NewClient(port)

		payload, _ := json.Marshal(map[string]int{
			"shares":    sealShares,
			"threshold": sealThreshold,
		})

		respBytes, err := client.DoRequest("POST", "/api/v1/secrets/init", payload)
		if err != nil {
			return fmt.Errorf("vault init failed: %w", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(respBytes, &result); err != nil {
			printer.Print(string(respBytes))
			return nil
		}

		printer.PrintSuccess("Vault initialized.")
		printer.PrintWarning("Save these key shares securely — they cannot be retrieved again:")
		if shares, ok := result["key_shares"]; ok {
			switch v := shares.(type) {
			case []interface{}:
				for i, share := range v {
					printer.Print(fmt.Sprintf("  Share %d: %v", i+1, share))
				}
			}
		}

		return nil
	},
}

var vaultSealCmd = &cobra.Command{
	Use:   "seal",
	Short: "Seal the local secret vault",
	Long: `Seal the local secret vault. All secrets become inaccessible until the vault
is unsealed again.

  ufctl secrets vault seal`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		printer := ui.GetPrinter(ctx)

		port := getAgentPort(ctx)
		client := agent.NewClient(port)

		_, err := client.DoRequest("POST", "/api/v1/secrets/seal", nil)
		if err != nil {
			return fmt.Errorf("vault seal failed: %w", err)
		}

		printer.PrintSuccess("Vault sealed.")
		return nil
	},
}

var vaultUnsealCmd = &cobra.Command{
	Use:   "unseal",
	Short: "Unseal the local secret vault",
	Long: `Unseal the local secret vault. You will be prompted for a key share.
Run this command multiple times (once per share) until the threshold is met.

  ufctl secrets vault unseal`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		printer := ui.GetPrinter(ctx)

		share, err := ui.PromptSecret("Key share:")
		if err != nil {
			return fmt.Errorf("failed to read key share: %w", err)
		}
		if share == "" {
			return fmt.Errorf("key share cannot be empty")
		}

		port := getAgentPort(ctx)
		client := agent.NewClient(port)

		payload, _ := json.Marshal(map[string]string{"key": share})
		respBytes, err := client.DoRequest("POST", "/api/v1/secrets/unseal", payload)
		if err != nil {
			return fmt.Errorf("vault unseal failed: %w", err)
		}

		var result map[string]interface{}
		if err2 := json.Unmarshal(respBytes, &result); err2 == nil {
			if sealed, ok := result["sealed"]; ok {
				if sealed == false {
					printer.PrintSuccess("Vault unsealed.")
				} else {
					if progress, ok2 := result["progress"]; ok2 {
						printer.Print(fmt.Sprintf("Key share accepted. Progress: %v", progress))
					} else {
						printer.Print("Key share accepted. Vault still sealed — provide more shares.")
					}
				}
				return nil
			}
		}

		printer.Print(string(respBytes))
		return nil
	},
}

var vaultStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the current vault seal/unseal status",
	Long: `Show the current seal/unseal status and configuration of the local vault.

  ufctl secrets vault status`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		printer := ui.GetPrinter(ctx)

		port := getAgentPort(ctx)
		client := agent.NewClient(port)

		respBytes, err := client.DoRequest("GET", "/api/v1/secrets/status", nil)
		if err != nil {
			return fmt.Errorf("failed to get vault status: %w", err)
		}

		var result map[string]interface{}
		if err2 := json.Unmarshal(respBytes, &result); err2 != nil {
			printer.Print(string(respBytes))
			return nil
		}

		for k, v := range result {
			printer.Print(fmt.Sprintf("  %-20s %v", k+":", v))
		}

		return nil
	},
}

func init() {
	// Flag on vault init subcommand
	vaultInitCmd.Flags().IntVar(&sealShares, "shares", 5, "Number of key shares to generate")
	vaultInitCmd.Flags().IntVar(&sealThreshold, "threshold", 3, "Number of shares required to unseal")

	// Port flag on each vault subcommand
	vaultInitCmd.Flags().IntVarP(&agentPort, "port", "p", 0, "Agent port (default: 2240)")
	vaultSealCmd.Flags().IntVarP(&agentPort, "port", "p", 0, "Agent port (default: 2240)")
	vaultUnsealCmd.Flags().IntVarP(&agentPort, "port", "p", 0, "Agent port (default: 2240)")
	vaultStatusCmd.Flags().IntVarP(&agentPort, "port", "p", 0, "Agent port (default: 2240)")

	sealCmd.AddCommand(vaultInitCmd)
	sealCmd.AddCommand(vaultSealCmd)
	sealCmd.AddCommand(vaultUnsealCmd)
	sealCmd.AddCommand(vaultStatusCmd)
}
