package main

import (
	"context"
	"os"

	"github.com/ambientlabscomputing/underleaf_client/internal/cli"
	"github.com/ambientlabscomputing/underleaf_client/internal/logging"
	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/ambientlabscomputing/underleaf_client/pkg/version"
)

func main() {
	ctx, logger := logging.Init(context.Background(), logging.LoggerModeCLI, nil) // Change this to LoggerModeDev to show in the console
	ctx, printer := ui.NewPrinterToContext(ctx, ui.FormatHuman)
	ctx, config := policy_manager.NewConfigClientInCtx(ctx, policy_manager.ConfigClientTypeCLI)
	// Log version directly — intentionally NOT persisted to config.yaml
	// (version is a build-time constant, not a user setting).
	logger.Info("Loaded configuration", "version", version.Version)
	logger.Debug("config loaded", "configClient", config.ConfigClientInfo())

	if err := cli.Execute(ctx); err != nil {
		logger.Error("command execution failed", "error", err)
		printer.PrintError(err.Error())
		os.Exit(1)
	}
}
