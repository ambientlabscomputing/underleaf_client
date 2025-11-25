package cli

import (
	"context"
	"fmt"
	"runtime"

	"github.com/ambientlabscomputing/underleaf_client/internal/logging"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/ambientlabscomputing/underleaf_client/pkg/version"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "ufctl",
	Short: "Underleaf CLI - Manage edge nodes and configurations",
	Long: `ufctl is the command-line interface for managing Underleaf edge nodes,
configurations, and cluster operations. Use ufctl to interact with both
local nodes and the control plane.`,
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

	},
	Run: func(cmd *cobra.Command, args []string) {
		// Default action when no subcommands are provided
		printer := ui.GetPrinter(cmd.Context())
		printer.PrintSuccess("Underleaf CLI - Use --help to see available commands")
		printer.Print(fmt.Sprintf("\n\nVersion: %s, Go Version: %s, OS/Arch: %s/%s", version.Version, runtime.Version(), runtime.GOOS, runtime.GOARCH))
	},
}

func Execute(ctx context.Context) error {
	return rootCmd.ExecuteContext(ctx)
}
