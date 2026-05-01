package cli

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/channel"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/cluster"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/deploy"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/expose"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/infra"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/jobs"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/link"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/local"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/mmesh"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/provider"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/secrets"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/servers"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/templates"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/tunnel"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/update"
	"github.com/ambientlabscomputing/underleaf_client/internal/logging"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/ambientlabscomputing/underleaf_client/pkg/version"
	"github.com/spf13/cobra"
)

var (
	// Global flag for specifying which agent port to connect to
	configAgentPort int
	// Global flag for output format: human, shell, json (empty = auto-detect)
	formatFlag string
)

// Context key for agent port (using string to avoid import cycles)
const configAgentPortContextKey = "config-agent-port"

var rootCmd = &cobra.Command{
	Use:   "ufctl",
	Short: "Underleaf CLI - Manage edge servers and configurations",
	Long: `ufctl is the command-line interface for managing Underleaf edge servers,
configurations, and cluster operations. Use ufctl to interact with both
local servers and the control plane.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// This runs before every command and subcommand
		ctx := cmd.Context()

		ctx, _ = logging.GetCtxWithLogger(ctx)
		ctx = logging.AddRuntimeValuesToCtx(ctx)

		// Resolve output format: explicit flag > auto-detect (TTY/NO_COLOR)
		var format ui.OutputFormat
		switch formatFlag {
		case "human":
			format = ui.FormatHuman
		case "shell":
			format = ui.FormatShell
		case "json":
			format = ui.FormatJSON
		case "":
			format = ui.DetectFormat()
		default:
			fmt.Fprintf(os.Stderr, "invalid format %q: must be human, shell, or json\n", formatFlag)
			os.Exit(1)
		}

		// Update printer with resolved format
		printer := ui.GetPrinter(ctx)
		if printer != nil {
			printer.SetFormat(format)
		} else {
			ctx, _ = ui.NewPrinterToContext(ctx, format)
		}
		cmd.SetContext(ctx)

		// Store config agent port in context if specified
		if configAgentPort != 0 {
			ctx = context.WithValue(ctx, configAgentPortContextKey, configAgentPort)
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
	rootCmd.PersistentFlags().IntVar(&configAgentPort, "config-agent-port", 0, "Override the agent port to connect to (default: 2240)")
	rootCmd.PersistentFlags().StringVarP(&formatFlag, "format", "F", "", "Output format: human, shell, json (default: auto-detect)")

	// Add subcommands to the root command
	rootCmd.AddCommand(local.StartCmd)
	rootCmd.AddCommand(local.RegisterCmd) // deprecated alias — kept for backward compat
	rootCmd.AddCommand(local.AuthCmd)
	rootCmd.AddCommand(local.AgentCmd)
	rootCmd.AddCommand(local.OrgCmd)
	rootCmd.AddCommand(local.ConfigCmd)
	rootCmd.AddCommand(local.CSRCmd)
	rootCmd.AddCommand(servers.ServersCmd)
	rootCmd.AddCommand(deploy.DeployCmd)
	rootCmd.AddCommand(tunnel.TunnelCmd)
	rootCmd.AddCommand(channel.ChannelCmd)
	rootCmd.AddCommand(expose.ExposeCmd)
	rootCmd.AddCommand(link.LinkCmd)
	rootCmd.AddCommand(infra.NewInfraCmd())
	rootCmd.AddCommand(jobs.JobsCmd)
	rootCmd.AddCommand(templates.TemplatesCmd)
	rootCmd.AddCommand(update.UpdateCmd)
	rootCmd.AddCommand(cluster.ClusterCmd)
	rootCmd.AddCommand(mmesh.MMeshCmd)
	rootCmd.AddCommand(provider.ProviderCmd)
	rootCmd.AddCommand(secrets.SecretsCmd)
}

func Execute(ctx context.Context) error {
	return rootCmd.ExecuteContext(ctx)
}
