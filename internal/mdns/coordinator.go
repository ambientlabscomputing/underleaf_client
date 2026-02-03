package mdns

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
)

// Coordinator manages the mDNS server lifecycle based on Raft leadership status
// It ensures only the Raft leader announces the api.underleaf.local service
type Coordinator struct {
	server   *Server
	config   *Config
	logger   *slog.Logger
	isLeader bool
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewCoordinator creates a new mDNS coordinator
func NewCoordinator(config *Config, logger *slog.Logger) (*Coordinator, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}

	if logger == nil {
		logger = slog.Default()
	}

	server, err := NewServer(config, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create mDNS server: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Coordinator{
		server:   server,
		config:   config,
		logger:   logger,
		isLeader: false,
		ctx:      ctx,
		cancel:   cancel,
	}, nil
}

// OnLeadershipChange is the callback to be registered with the Raft node
// It starts or stops the mDNS server based on leadership status
func (c *Coordinator) OnLeadershipChange(isLeader bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// No change in leadership status
	if c.isLeader == isLeader {
		return
	}

	c.isLeader = isLeader

	if isLeader {
		c.logger.Info("node became leader, starting mDNS leader announcements")
		if err := c.server.Start(c.ctx); err != nil {
			c.logger.Error("failed to start mDNS leader server", "error", err)
		}
	} else {
		c.logger.Info("node lost leadership, stopping mDNS leader announcements")
		if err := c.server.Stop(); err != nil {
			c.logger.Error("failed to stop mDNS leader server", "error", err)
		}
	}
}

// Start manually starts the mDNS server if the node is currently the leader
func (c *Coordinator) Start() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	c.logger.Info("DEBUG: Coordinator.Start() called", "is_leader", c.isLeader, "node_id", c.config.NodeID)

	// Always start node-specific announcement
	c.logger.Info("DEBUG: Calling server.StartNode()")
	if err := c.server.StartNode(c.ctx); err != nil {
		c.logger.Error("failed to start mDNS node server", "error", err)
		// Continue even if node announcement fails
	} else {
		c.logger.Info("DEBUG: server.StartNode() completed successfully")
	}

	// Only start leader announcement if we're the leader
	if !c.isLeader {
		c.logger.Debug("not starting mDNS leader server, node is not leader")
		return nil
	}

	return c.server.Start(c.ctx)
}

// Stop stops the mDNS server regardless of leadership status
func (c *Coordinator) Stop() error {
	c.cancel()

	// Stop both node and leader announcements
	var errs []error
	if err := c.server.Stop(); err != nil {
		errs = append(errs, fmt.Errorf("failed to stop leader server: %w", err))
	}
	if err := c.server.StopNode(); err != nil {
		errs = append(errs, fmt.Errorf("failed to stop node server: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors stopping mDNS servers: %v", errs)
	}
	return nil
}

// IsRunning returns true if the mDNS server is currently running
func (c *Coordinator) IsRunning() bool {
	return c.server.IsRunning()
}

// UpdateConfig updates the mDNS configuration
// If the server is running, it will be restarted with the new configuration
func (c *Coordinator) UpdateConfig(newConfig *Config) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	wasRunning := c.server.IsRunning()

	// Stop the server if running
	if wasRunning {
		if err := c.server.Stop(); err != nil {
			return fmt.Errorf("failed to stop server before config update: %w", err)
		}
	}

	// Create new server with updated config
	newServer, err := NewServer(newConfig, c.logger)
	if err != nil {
		return fmt.Errorf("failed to create server with new config: %w", err)
	}

	c.server = newServer
	c.config = newConfig

	// Restart if it was running
	if wasRunning && c.isLeader {
		if err := c.server.Start(c.ctx); err != nil {
			return fmt.Errorf("failed to restart server after config update: %w", err)
		}
	}

	c.logger.Info("mDNS configuration updated")
	return nil
}

// ComputeCAFingerprint computes a SHA-256 fingerprint of a CA certificate
// This is a helper function for setting Config.CAFingerprint
func ComputeCAFingerprint(caCertPEM []byte) string {
	hash := sha256.Sum256(caCertPEM)
	return hex.EncodeToString(hash[:])
}
