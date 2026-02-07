package provider

import (
	"github.com/spf13/cobra"
)

// ProviderCmd is the root command for provider management
var ProviderCmd = &cobra.Command{
	Use:   "provider",
	Short: "Manage capability providers",
	Long: `Manage capability providers - install, start, stop, and query providers
that implement capabilities for the Underleaf Agent.

Providers can be binary executables or OCI containers that implement
specific capabilities (e.g., mesh.binding.engine, mesh.policy.evaluator).`,
}

func init() {
	// Add subcommands
	ProviderCmd.AddCommand(listCmd)
	ProviderCmd.AddCommand(installCmd)
	ProviderCmd.AddCommand(startCmd)
	ProviderCmd.AddCommand(statusCmd)
	ProviderCmd.AddCommand(stopCmd)
	ProviderCmd.AddCommand(restartCmd)
	ProviderCmd.AddCommand(uninstallCmd)
	ProviderCmd.AddCommand(logsCmd)
}
