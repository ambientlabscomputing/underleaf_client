package raft

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb/v2"
)

// Node represents a Raft node in the cluster.
type Node struct {
	// config is the node configuration
	config *NodeConfig

	// raft is the underlying Raft instance
	raft *raft.Raft

	// fsm is the finite state machine
	fsm *KVStateMachine

	// transport is the gRPC transport layer
	transport *GRPCTransport

	// logger for structured logging
	logger *slog.Logger

	// shutdownCh signals shutdown
	shutdownCh chan struct{}

	// mu protects concurrent access
	mu sync.RWMutex

	// maintenanceMode indicates if cluster is in maintenance mode
	maintenanceMode bool

	// leaderChangeCallbacks are called when leadership state changes
	leaderChangeCallbacks []LeaderChangeCallback

	// lastKnownRole tracks the last known role to detect transitions
	lastKnownRole NodeRole
}

// LeaderChangeCallback is called when the node's leadership state changes
type LeaderChangeCallback func(isLeader bool)

// NewNode creates a new Raft node.
func NewNode(config *NodeConfig, logger *slog.Logger) (*Node, error) {
	if config == nil {
		config = DefaultNodeConfig()
	}

	if logger == nil {
		logger = slog.Default()
	}

	// Validate configuration
	if config.NodeID == "" {
		return nil, fmt.Errorf("node ID is required")
	}
	if config.BindAddr == "" {
		return nil, fmt.Errorf("bind address is required")
	}
	if config.DataDir == "" {
		return nil, fmt.Errorf("data directory is required")
	}

	// Create data directory if it doesn't exist
	if err := os.MkdirAll(config.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	node := &Node{
		config:     config,
		logger:     logger.With("component", "raft", "node_id", config.NodeID),
		shutdownCh: make(chan struct{}),
	}

	return node, nil
}

// Start initializes and starts the Raft node.
func (n *Node) Start() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.logger.Info("starting raft node", "bind_addr", n.config.BindAddr, "bootstrap", n.config.Bootstrap)

	// Create FSM
	n.fsm = NewKVStateMachine(n.config.MaxValueSize, n.config.MaxStorageSize, n.logger)

	// Create transport
	transport, err := NewGRPCTransport(
		n.config.BindAddr,
		n.config.AdvertiseAddr,
		5*time.Second,
		n.logger,
	)
	if err != nil {
		return fmt.Errorf("failed to create transport: %w", err)
	}
	n.transport = transport

	// Start transport
	if err := n.transport.Start(); err != nil {
		return fmt.Errorf("failed to start transport: %w", err)
	}

	// Create Raft configuration
	raftConfig := raft.DefaultConfig()
	raftConfig.LocalID = raft.ServerID(n.config.NodeID)
	raftConfig.ProtocolVersion = raft.ProtocolVersionMax // Use latest protocol version for compatibility
	raftConfig.HeartbeatTimeout = n.config.HeartbeatTimeout
	raftConfig.ElectionTimeout = n.config.ElectionTimeout
	raftConfig.LeaderLeaseTimeout = n.config.LeaderLeaseTimeout
	raftConfig.SnapshotInterval = n.config.SnapshotInterval
	raftConfig.SnapshotThreshold = n.config.SnapshotThreshold

	// Create log store using BoltDB
	logStore, err := raftboltdb.NewBoltStore(filepath.Join(n.config.DataDir, "raft.db"))
	if err != nil {
		return fmt.Errorf("failed to create log store: %w", err)
	}

	// Create stable store (also using BoltDB)
	stableStore := logStore

	// Create snapshot store
	snapshotStore, err := raft.NewFileSnapshotStore(n.config.DataDir, 3, os.Stderr)
	if err != nil {
		return fmt.Errorf("failed to create snapshot store: %w", err)
	}

	// Create Raft instance
	r, err := raft.NewRaft(raftConfig, n.fsm, logStore, stableStore, snapshotStore, n.transport)
	if err != nil {
		return fmt.Errorf("failed to create raft instance: %w", err)
	}
	n.raft = r

	// Bootstrap cluster if needed
	if n.config.Bootstrap {
		if err := n.bootstrap(); err != nil {
			return fmt.Errorf("failed to bootstrap cluster: %w", err)
		}
	}

	n.logger.Info("raft node started successfully")

	// Start leadership monitoring in background
	go n.monitorLeadershipChanges()

	return nil
}

// BootstrapSelf bootstraps a single-node cluster with this node as the sole voter.
// This is used when the cluster has never been bootstrapped (nodes == {}) and the
// caller wants to elect this node as the initial leader without config changes.
func (n *Node) BootstrapSelf() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.raft == nil {
		return fmt.Errorf("raft not started")
	}

	n.logger.Info("bootstrapping self as single-node cluster leader")

	configuration := raft.Configuration{
		Servers: []raft.Server{
			{
				ID:       raft.ServerID(n.config.NodeID),
				Address:  n.transport.LocalAddr(),
				Suffrage: raft.Voter,
			},
		},
	}

	future := n.raft.BootstrapCluster(configuration)
	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to bootstrap cluster: %w", err)
	}

	n.logger.Info("single-node bootstrap complete, node will elect itself leader")
	return nil
}

// bootstrap handles cluster bootstrapping based on configuration.
func (n *Node) bootstrap() error {
	// For now, always attempt bootstrap if configured
	// In production, you'd check for existing state in the stores
	n.logger.Info("bootstrapping cluster")

	// Build server configuration based on bootstrap peers
	var servers []raft.Server

	// Add self as a voting member
	servers = append(servers, raft.Server{
		ID:       raft.ServerID(n.config.NodeID),
		Address:  n.transport.LocalAddr(),
		Suffrage: raft.Voter,
	})

	// Add bootstrap peers if configured
	for _, peer := range n.config.BootstrapPeers {
		// Don't add self again
		if peer == n.config.NodeID {
			continue
		}

		servers = append(servers, raft.Server{
			ID:       raft.ServerID(peer),
			Address:  raft.ServerAddress(peer),
			Suffrage: raft.Voter,
		})
	}

	// Enforce voting topology rules from RFC Section 4.1
	// Maximum 3 voters
	if len(servers) > 3 {
		n.logger.Warn("bootstrap config has more than 3 voters, limiting to 3",
			"total_servers", len(servers))

		// Keep first 3 as voters, rest as learners (though they're not added here)
		voters := servers[:3]
		configuration := raft.Configuration{
			Servers: voters,
		}

		future := n.raft.BootstrapCluster(configuration)
		if err := future.Error(); err != nil {
			return fmt.Errorf("failed to bootstrap cluster: %w", err)
		}

		n.logger.Info("cluster bootstrapped with limited voters",
			"voters", len(voters), "initial_servers", len(servers))
	} else {
		configuration := raft.Configuration{
			Servers: servers,
		}

		future := n.raft.BootstrapCluster(configuration)
		if err := future.Error(); err != nil {
			return fmt.Errorf("failed to bootstrap cluster: %w", err)
		}

		n.logger.Info("cluster bootstrapped", "servers", len(servers))
	}

	return nil
}

// Stop gracefully shuts down the Raft node.
func (n *Node) Stop() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.logger.Info("stopping raft node")

	// Signal shutdown
	close(n.shutdownCh)

	// Shutdown Raft
	if n.raft != nil {
		future := n.raft.Shutdown()
		if err := future.Error(); err != nil {
			n.logger.Error("error shutting down raft", "error", err)
			return err
		}
	}

	// Close transport
	if n.transport != nil {
		if err := n.transport.Close(); err != nil {
			n.logger.Error("error closing transport", "error", err)
			return err
		}
	}

	n.logger.Info("raft node stopped")
	return nil
}

// GetRole returns the current role of this node.
func (n *Node) GetRole() NodeRole {
	if n.raft == nil {
		return RoleFollower
	}

	state := n.raft.State()
	switch state {
	case raft.Leader:
		return RoleLeader
	case raft.Candidate:
		return RoleCandidate
	case raft.Follower:
		return RoleFollower
	default:
		return RoleFollower
	}
}

// IsLeader returns true if this node is the current leader.
func (n *Node) IsLeader() bool {
	return n.GetRole() == RoleLeader
}

// GetLeader returns the address of the current leader.
func (n *Node) GetLeader() (string, error) {
	if n.raft == nil {
		return "", fmt.Errorf("raft not initialized")
	}

	leaderAddr, leaderID := n.raft.LeaderWithID()
	if leaderAddr == "" {
		return "", fmt.Errorf("no leader elected")
	}

	n.logger.Debug("leader info", "addr", leaderAddr, "id", leaderID)
	return string(leaderAddr), nil
}

// GetStats returns statistics about the cluster state.
func (n *Node) GetStats() (*ClusterStats, error) {
	if n.raft == nil {
		return nil, fmt.Errorf("raft not initialized")
	}

	// Get Raft stats - LeaderWithID returns both address and ID
	_, leaderID := n.raft.LeaderWithID()
	lastLog := n.raft.LastIndex()

	// Get FSM stats
	revision, keyCount, storageSize := n.fsm.GetStats()

	// Count peers
	configFuture := n.raft.GetConfiguration()
	if err := configFuture.Error(); err != nil {
		return nil, fmt.Errorf("failed to get configuration: %w", err)
	}

	numPeers := len(configFuture.Configuration().Servers)

	// Get term from stats string map
	var term uint64
	if termStr, ok := n.raft.Stats()["term"]; ok {
		fmt.Sscanf(termStr, "%d", &term)
	}

	stats := &ClusterStats{
		NodeID:       n.config.NodeID,
		Role:         n.GetRole(),
		LeaderID:     string(leaderID), // Use leader ID (node UUID) not address
		Term:         term,
		CommitIndex:  n.raft.CommitIndex(),
		AppliedIndex: n.raft.AppliedIndex(),
		LastLogIndex: lastLog,
		NumPeers:     numPeers,
		Revision:     revision,
		KeyCount:     keyCount,
		StorageSize:  storageSize,
	}

	return stats, nil
}

// WaitForLeader blocks until a leader is elected or timeout occurs.
func (n *Node) WaitForLeader(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if _, err := n.GetLeader(); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for leader election")
}

// Apply submits a command to the Raft log.
func (n *Node) Apply(cmd []byte, timeout time.Duration) error {
	if n.raft == nil {
		return fmt.Errorf("raft not initialized")
	}

	if !n.IsLeader() {
		return ErrNotLeader
	}

	future := n.raft.Apply(cmd, timeout)
	if err := future.Error(); err != nil {
		return err
	}

	// Check if the FSM returned an error
	if err, ok := future.Response().(error); ok {
		return err
	}

	return nil
}

// GetConfiguration returns the current cluster configuration.
func (n *Node) GetConfiguration() (*ClusterConfig, error) {
	if n.raft == nil {
		return nil, fmt.Errorf("raft not initialized")
	}

	configFuture := n.raft.GetConfiguration()
	if err := configFuture.Error(); err != nil {
		return nil, fmt.Errorf("failed to get configuration: %w", err)
	}

	config := &ClusterConfig{
		Nodes:           make(map[string]*NodeInfo),
		MaintenanceMode: n.maintenanceMode,
	}

	leaderAddr, _ := n.raft.LeaderWithID()

	for _, server := range configFuture.Configuration().Servers {
		nodeID := string(server.ID)

		role := RoleFollower
		if server.Suffrage == raft.Nonvoter {
			role = RoleLearner
		} else if string(server.Address) == string(leaderAddr) {
			role = RoleLeader
		}

		config.Nodes[nodeID] = &NodeInfo{
			ID:       nodeID,
			Address:  string(server.Address),
			Role:     role,
			Suffrage: server.Suffrage,
			IsLeader: string(server.Address) == string(leaderAddr),
		}
	}

	return config, nil
}

// SetMaintenanceMode enables or disables maintenance mode.
func (n *Node) SetMaintenanceMode(enabled bool) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if !n.IsLeader() {
		return ErrNotLeader
	}

	n.maintenanceMode = enabled
	n.logger.Info("maintenance mode changed", "enabled", enabled)
	return nil
}

// IsMaintenanceMode returns true if the cluster is in maintenance mode.
func (n *Node) IsMaintenanceMode() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.maintenanceMode
}

// VerifyLeader ensures this node is the leader or returns the leader address.
func (n *Node) VerifyLeader() error {
	if n.raft == nil {
		return fmt.Errorf("raft not initialized")
	}

	if n.raft.VerifyLeader().Error() != nil {
		leaderAddr, _ := n.GetLeader()
		return NewRaftErrorf("not_leader", "this node is not the leader; current leader is %s", leaderAddr)
	}

	return nil
}

// Barrier ensures all preceding operations have been applied to the FSM.
func (n *Node) Barrier(timeout time.Duration) error {
	if n.raft == nil {
		return fmt.Errorf("raft not initialized")
	}

	future := n.raft.Barrier(timeout)
	return future.Error()
}

// LeadershipTransfer attempts to transfer leadership to the specified node.
func (n *Node) LeadershipTransfer(targetID string, targetAddr string) error {
	if !n.IsLeader() {
		return ErrNotLeader
	}

	future := n.raft.LeadershipTransfer()
	return future.Error()
}

// GetRaft returns the underlying Raft instance (for advanced operations).
func (n *Node) GetRaft() *raft.Raft {
	return n.raft
}

// GetFSM returns the FSM instance.
func (n *Node) GetFSM() *KVStateMachine {
	return n.fsm
}

// RegisterLeaderChangeCallback registers a callback to be called when leadership state changes
func (n *Node) RegisterLeaderChangeCallback(callback LeaderChangeCallback) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.leaderChangeCallbacks = append(n.leaderChangeCallbacks, callback)
}

// monitorLeadershipChanges monitors for leadership state changes and invokes callbacks
func (n *Node) monitorLeadershipChanges() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			currentRole := n.GetRole()

			n.mu.Lock()
			if currentRole != n.lastKnownRole {
				// Leadership state changed
				wasLeader := n.lastKnownRole == RoleLeader
				isLeader := currentRole == RoleLeader

				if wasLeader != isLeader {
					n.logger.Info("leadership state changed",
						"was_leader", wasLeader,
						"is_leader", isLeader,
						"old_role", n.lastKnownRole,
						"new_role", currentRole)

					// Invoke callbacks
					for _, callback := range n.leaderChangeCallbacks {
						// Run callback in goroutine to avoid blocking
						go callback(isLeader)
					}
				}

				n.lastKnownRole = currentRole
			}
			n.mu.Unlock()

		case <-n.shutdownCh:
			n.logger.Info("stopping leadership monitor")
			return
		}
	}
}

// GetMembership returns a Membership manager for this node
// Note: Event publishing is not available when using this method - use NewMembership directly
func (n *Node) GetMembership() *Membership {
	return NewMembership(n, nil)
}

// ID returns the Node ID
func (n *Node) ID() string {
	return n.config.NodeID
}
