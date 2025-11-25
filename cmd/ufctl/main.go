package main

import (
	"context"

	"github.com/ambientlabscomputing/underleaf_client/internal/cli"
	"github.com/ambientlabscomputing/underleaf_client/internal/logging"
)

func main() {
	ctx, logger := logging.Init(context.Background(), logging.LoggerModeDev, nil)
	if err := cli.Execute(ctx); err != nil {
		logger.Error("command execution failed", "error", err)
		panic(err)
	}
}
