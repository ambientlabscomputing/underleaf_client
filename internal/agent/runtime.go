package agent

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// Runtime manages the agent lifecycle and signal handling
type Runtime struct {
	launcher *Launcher
	signals  chan os.Signal
}

// NewRuntime creates a new runtime manager
func NewRuntime(launcher *Launcher) *Runtime {
	return &Runtime{
		launcher: launcher,
		signals:  make(chan os.Signal, 1),
	}
}

// Run starts the agent and handles signals
func (r *Runtime) Run(ctx context.Context) error {
	// Setup signal handling
	signal.Notify(r.signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start agent
	errChan := make(chan error, 1)
	go func() {
		errChan <- r.launcher.Start(ctx)
	}()

	// Wait for signal or error
	select {
	case sig := <-r.signals:
		switch sig {
		case syscall.SIGINT, syscall.SIGTERM:
			// Graceful shutdown
			cancel()
			return r.launcher.Stop()
		case syscall.SIGHUP:
			// Reload/restart
			return r.launcher.Restart(ctx)
		}
	case err := <-errChan:
		return err
	}

	return nil
}

// Stop stops the runtime
func (r *Runtime) Stop() error {
	return r.launcher.Stop()
}
