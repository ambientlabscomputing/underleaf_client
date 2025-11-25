package logging

import (
	"context"
	"log/slog"
	"os"

	"github.com/lmittmann/tint"
	"gopkg.in/natefinch/lumberjack.v2"
)

var logger *slog.Logger

type LogKey struct{}

const (
	LoggerModeCLI   = "cli"   // logs to file in json format, uses lumberjack for log rotation
	LoggerModeAgent = "agent" // logs to file in json format, uses lumberjack for log rotation
	LoggerModeDev   = "dev"   // logs to stderr in human friendly tint format
)

func Init(parentCtx context.Context, mode string, filePath *string) (context.Context, *slog.Logger) {
	var handler slog.Handler
	switch mode {
	case LoggerModeCLI, LoggerModeAgent:
		var logFile string
		if filePath != nil {
			logFile = *filePath
		} else if mode == LoggerModeCLI {
			logFile = "underleaf_cli.log"
		} else {
			logFile = "underleaf_agent.log"
		}
		handler = slog.NewJSONHandler(&lumberjack.Logger{
			Filename:   logFile,
			MaxSize:    10, // megabytes
			MaxBackups: 5,
			MaxAge:     28,   //days
			Compress:   true, // disabled by default
		}, &slog.HandlerOptions{Level: slog.LevelDebug})
	case LoggerModeDev:
		handler = tint.NewHandler(os.Stderr, &tint.Options{Level: slog.LevelDebug})
	default:
		panic("unknown logger mode: " + mode)
	}

	logger = slog.New(handler)
	ctx := context.WithValue(parentCtx, LogKey{}, logger)
	return ctx, logger
}
