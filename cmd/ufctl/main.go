package main

import (
	"context"
	"os"

	"github.com/ambientlabscomputing/underleaf_client/internal/cli"
	"github.com/ambientlabscomputing/underleaf_client/internal/logging"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
)

func main() {
	ctx, logger := logging.Init(context.Background(), logging.LoggerModeDev, nil)
	ctx, _ = ui.NewPrinterToContext(ctx, ui.FormatTable)

	if err := cli.Execute(ctx); err != nil {
		logger.Error("command execution failed", "error", err)
		os.Exit(1)
	}
}
