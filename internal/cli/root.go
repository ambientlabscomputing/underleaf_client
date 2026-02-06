package cli

import (
	"context"
	"fmt"
	"runtime"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/cluster"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/deploy"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/jobs"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/local"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/mmesh"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/servers"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/templates"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/update"
	"github.com/ambientlabscomputing/underleaf_client/internal/logging"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/ambientlabscomputing/underleaf_client/pkg/version"
	"github.com/spf13/cobra"
)

var (
	// Global flag for specifying which agent port to connect to
	configAgentPort int
)

type configAgentPortKey struct{}

var rootCmd = &cobra.Command{
	Use:   "ufctl",
	Short: "Underleaf CLI - Manage edge servers and configurations",
	Long: `ufctl is the command-line interface for managing Underleaf edge servers,
configurations, and cluster operations. Use ufctl to interact with both
local servers and the control plane.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// This runs before every command and subcommand
		// Add anything you need to the context here
		ctx := cmd.Context()

		ctx, _ = logging.GetCtxWithLogger(ctx)
		ctx = logging.AddRuntimeValuesToCtx(ctx)
		// add printer
		if ui.GetPrinter(ctx) == nil {
			ctx, _ = ui.NewPrinterToContext(ctx, ui.FormatTable)
			cmd.SetContext(ctx)
		}

		// Store config agent port in context if specified
		if configAgentPort != 0 {
			ctx = context.WithValue(ctx, configAgentPortKey{}, configAgentPort)
			cmd.SetContext(ctx)
		}
	},
	Run: func(cmd *cobra.Command, args []string) {
		// Default action when no subcommands are provided
		printer := ui.GetPrinter(cmd.Context())
		printer.PrintSuccess("Underleaf CLI - Use --help to see available commands")
		printer.Print(fmt.Sprintf("\n\nVersion: %s, Go Version: %s, OS/Arch: %s/%s", version.Version, runtime.Version(), runtime.GOOS, runtime.GOARCH))
	},
}

func init() {
	// Add global persistent flags
	rootCmd.PersistentFlags().IntVar(&configAgentPort, "config-agent-port", 0, "Override the agent port to connect to (default: 8081)")

	// Add subcommands to the root command
	rootCmd.AddCommand(local.StartCmd)
	rootCmd.AddCommand(local.AuthCmd)
	rootCmd.AddCommand(local.AgentCmd)
	rootCmd.AddCommand(local.OrgCmd)
	rootCmd.AddCommand(local.ConfigCmd)
	rootCmd.AddCommand(local.CSRCmd)
	rootCmd.AddCommand(servers.ServersCmd)
	rootCmd.AddCommand(deploy.DeployCmd)
	rootCmd.AddCommand(jobs.JobsCmd)
	rootCmd.AddCommand(templates.TemplatesCmd)
	rootCmd.AddCommand(update.UpdateCmd)
	rootCmd.AddCommand(cluster.ClusterCmd)
	rootCmd.AddCommand(mmesh.MMeshCmd)
}

func Execute(ctx context.Context) error {
	return rootCmd.ExecuteContext(ctx)
}
