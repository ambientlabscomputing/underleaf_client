// Package testkernel provides utilities for testing with the kernel syscall server
package testkernel

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/ambientlabscomputing/underleaf_client/internal/kernel"
)

// TestServer wraps a kernel syscall server for testing
type TestServer struct {
	server *kernel.SyscallServer
}

// Config holds configuration for the test kernel server
type Config struct {
	SocketPath string
	NodeID     string
	OrgID      string
	ClusterID  string
	Logger     *slog.Logger
}

// NewTestServer creates a new test kernel server
func NewTestServer(cfg Config) (*TestServer, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	}

	kernelConfig := kernel.Config{
		SocketPath: cfg.SocketPath,
		Logger:     cfg.Logger,
		NodeID:     cfg.NodeID,
		OrgID:      cfg.OrgID,
		ClusterID:  cfg.ClusterID,
	}

	server := kernel.NewSyscallServer(kernelConfig)
	if err := server.Start(); err != nil {
		return nil, fmt.Errorf("failed to start kernel server: %w", err)
	}

	return &TestServer{server: server}, nil
}

// Stop stops the test kernel server
func (ts *TestServer) Stop() error {
	if ts.server == nil {
		return nil
	}
	return ts.server.Stop()
}

// Shutdown gracefully shuts down the test kernel server
func (ts *TestServer) Shutdown(ctx context.Context) error {
	if ts.server == nil {
		return nil
	}
	return ts.server.Shutdown(ctx)
}
