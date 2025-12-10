package main

import (
	"context"
	"os"

	"github.com/ambientlabscomputing/underleaf_client/internal/cli"
	"github.com/ambientlabscomputing/underleaf_client/internal/config_manager"
	"github.com/ambientlabscomputing/underleaf_client/internal/logging"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/ambientlabscomputing/underleaf_client/pkg/version"
)

func main() {
	ctx, logger := logging.Init(context.Background(), logging.LoggerModeCLI, nil) // Change this to LoggerModeDev to show in the console
	ctx, printer := ui.NewPrinterToContext(ctx, ui.FormatTable)
	ctx, config := config_manager.NewConfigClientInCtx(ctx, config_manager.ConfigClientTypeCLI)
	config.Set("version", version.Version)
	if versionInCfg, found := config.Get("version"); found {
		logger.Info("Loaded configuration", "version", versionInCfg)
	} else {
		logger.Warn("No version found in configuration")
	}
	logger.Debug("config loaded", "configClient", config.ConfigClientInfo())

	if err := cli.Execute(ctx); err != nil {
		logger.Error("command execution failed", "error", err)
		printer.PrintError(err.Error())
		os.Exit(1)
	}
}
