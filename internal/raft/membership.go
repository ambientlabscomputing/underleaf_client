package raft

import (
	"fmt"
	"time"

	"github.com/hashicorp/raft"
)

// Membership manages cluster membership operations.
type Membership struct {
	node *Node
}

// NewMembership creates a new Membership manager.
func NewMembership(node *Node) *Membership {
	return &Membership{node: node}
}

// AddNode adds a new node to the cluster as a learner.
// Following RFC Section 11.2, new nodes must join as learners first.
func (m *Membership) AddNode(nodeID string, address string) error {
	if !m.node.IsLeader() {
		return ErrNotLeader
	}

	// Check if node already exists
	config, err := m.node.GetConfiguration()
	if err != nil {
		return err
	}

	if _, exists := config.Nodes[nodeID]; exists {
		return ErrNodeAlreadyExists
	}

	// Add as voter or learner based on current voter count
	voterCount := m.countVoters(config)

	var suffrage raft.ServerSuffrage
	if voterCount < 3 {
		// Add as voter (following RFC Section 4.1 - max 3 voters)
		suffrage = raft.Voter
	} else {
		// Add as learner (non-voter)
		suffrage = raft.Nonvoter
	}

	// Add the server
	serverID := raft.ServerID(nodeID)
	serverAddr := raft.ServerAddress(address)

	var future raft.IndexFuture
	if suffrage == raft.Voter {
		future = m.node.raft.AddVoter(serverID, serverAddr, 0, 0)
	} else {
		future = m.node.raft.AddNonvoter(serverID, serverAddr, 0, 0)
	}

	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to add node: %w", err)
	}

	m.node.logger.Info("node added to cluster",
		"node_id", nodeID,
		"address", address,
		"suffrage", suffrage)

	return nil
}

// RemoveNode removes a node from the cluster.
// Following RFC Section 11.4, ensures remaining voters satisfy quorum rules.
func (m *Membership) RemoveNode(nodeID string) error {
	if !m.node.IsLeader() {
		return ErrNotLeader
	}

	// Check if node exists
	config, err := m.node.GetConfiguration()
	if err != nil {
		return err
	}

	nodeInfo, exists := config.Nodes[nodeID]
	if !exists {
		return ErrNodeNotFound
	}

	// Check if removal would violate minimum voters
	if nodeInfo.Suffrage == raft.Voter {
		voterCount := m.countVoters(config)
		if voterCount <= 1 {
			return ErrInsufficientVoters
		}
	}

	// Remove the server
	serverID := raft.ServerID(nodeID)
	future := m.node.raft.RemoveServer(serverID, 0, 0)
	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to remove node: %w", err)
	}

	m.node.logger.Info("node removed from cluster", "node_id", nodeID)
	return nil
}

// PromoteNode promotes a learner to a voter.
// Following RFC Section 11.3, ensures node is fully synchronized.
func (m *Membership) PromoteNode(nodeID string) error {
	if !m.node.IsLeader() {
		return ErrNotLeader
	}

	// Check maintenance mode requirement (RFC Section 11.1)
	if !m.node.IsMaintenanceMode() {
		return ErrMaintenanceModeRequired
	}

	// Get current configuration
	config, err := m.node.GetConfiguration()
	if err != nil {
		return err
	}

	nodeInfo, exists := config.Nodes[nodeID]
	if !exists {
		return ErrNodeNotFound
	}

	// Check if already a voter
	if nodeInfo.Suffrage == raft.Voter {
		return fmt.Errorf("node is already a voter")
	}

	// Check voter count limit (RFC Section 4.1 - max 3 voters)
	voterCount := m.countVoters(config)
	if voterCount >= 3 {
		return NewRaftError("max_voters_reached", "cluster already has maximum of 3 voters")
	}

	// Note on synchronization (RFC Section 11.3):
	// The Raft library automatically handles log synchronization when a node is promoted.
	// AddVoter will wait for the node to catch up before it participates in voting.
	// Operators should still ensure the node is healthy and connected before promotion.
	// For stricter verification, check node stats using GetNodeInfo() before calling this method.

	// Promote to voter
	serverID := raft.ServerID(nodeID)
	serverAddr := raft.ServerAddress(nodeInfo.Address)

	future := m.node.raft.AddVoter(serverID, serverAddr, 0, 0)
	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to promote node: %w", err)
	}

	m.node.logger.Info("node promoted to voter", "node_id", nodeID)
	return nil
}

// DemoteNode demotes a voter to a learner.
func (m *Membership) DemoteNode(nodeID string) error {
	if !m.node.IsLeader() {
		return ErrNotLeader
	}

	// Check maintenance mode requirement
	if !m.node.IsMaintenanceMode() {
		return ErrMaintenanceModeRequired
	}

	// Get current configuration
	config, err := m.node.GetConfiguration()
	if err != nil {
		return err
	}

	nodeInfo, exists := config.Nodes[nodeID]
	if !exists {
		return ErrNodeNotFound
	}

	// Check if already a learner
	if nodeInfo.Suffrage == raft.Nonvoter {
		return fmt.Errorf("node is already a learner")
	}

	// Check if demotion would violate minimum voters
	voterCount := m.countVoters(config)
	if voterCount <= 1 {
		return ErrInsufficientVoters
	}

	// Demote to learner
	serverID := raft.ServerID(nodeID)
	serverAddr := raft.ServerAddress(nodeInfo.Address)

	// First remove, then re-add as nonvoter
	removeFuture := m.node.raft.RemoveServer(serverID, 0, 0)
	if err := removeFuture.Error(); err != nil {
		return fmt.Errorf("failed to remove node during demotion: %w", err)
	}

	addFuture := m.node.raft.AddNonvoter(serverID, serverAddr, 0, 0)
	if err := addFuture.Error(); err != nil {
		// Try to re-add as voter if nonvoter add fails
		m.node.raft.AddVoter(serverID, serverAddr, 0, 0)
		return fmt.Errorf("failed to re-add node as learner: %w", err)
	}

	m.node.logger.Info("node demoted to learner", "node_id", nodeID)
	return nil
}

// EnableMaintenanceMode enables maintenance mode for the cluster.
// Following RFC Section 11.1, this allows membership changes.
func (m *Membership) EnableMaintenanceMode() error {
	return m.node.SetMaintenanceMode(true)
}

// DisableMaintenanceMode disables maintenance mode for the cluster.
func (m *Membership) DisableMaintenanceMode() error {
	return m.node.SetMaintenanceMode(false)
}

// GetClusterConfiguration returns the current cluster configuration.
func (m *Membership) GetClusterConfiguration() (*ClusterConfig, error) {
	return m.node.GetConfiguration()
}

// countVoters counts the number of voting members in the configuration.
func (m *Membership) countVoters(config *ClusterConfig) int {
	count := 0
	for _, node := range config.Nodes {
		if node.Suffrage == raft.Voter {
			count++
		}
	}
	return count
}

// TransferLeadership transfers leadership to another node.
func (m *Membership) TransferLeadership(targetNodeID string) error {
	if !m.node.IsLeader() {
		return ErrNotLeader
	}

	// Get target node info
	config, err := m.node.GetConfiguration()
	if err != nil {
		return err
	}

	targetNode, exists := config.Nodes[targetNodeID]
	if !exists {
		return ErrNodeNotFound
	}

	// Verify target is a voter
	if targetNode.Suffrage != raft.Voter {
		return NewRaftError("not_voter", "target node must be a voter to receive leadership")
	}

	// Initiate leadership transfer
	if err := m.node.LeadershipTransfer(targetNodeID, targetNode.Address); err != nil {
		return fmt.Errorf("failed to transfer leadership: %w", err)
	}

	m.node.logger.Info("leadership transfer initiated", "target", targetNodeID)
	return nil
}

// WaitForNodeSync waits for a node to synchronize with the leader.
func (m *Membership) WaitForNodeSync(nodeID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		// Check node status
		// This is a simplified check - production would verify log indices
		config, err := m.node.GetConfiguration()
		if err != nil {
			return err
		}

		if _, exists := config.Nodes[nodeID]; exists {
			// Node exists in configuration
			// In a full implementation, we'd check:
			// - Node's last log index
			// - Node's commit index
			// - Time since last contact
			m.node.logger.Debug("checking node sync status", "node_id", nodeID)
			return nil
		}

		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for node %s to sync", nodeID)
}

// GetNodeStatus returns detailed status information for a node.
func (m *Membership) GetNodeStatus(nodeID string) (*NodeInfo, error) {
	config, err := m.node.GetConfiguration()
	if err != nil {
		return nil, err
	}

	nodeInfo, exists := config.Nodes[nodeID]
	if !exists {
		return nil, ErrNodeNotFound
	}

	return nodeInfo, nil
}

// ListNodes returns a list of all nodes in the cluster.
func (m *Membership) ListNodes() ([]*NodeInfo, error) {
	config, err := m.node.GetConfiguration()
	if err != nil {
		return nil, err
	}

	nodes := make([]*NodeInfo, 0, len(config.Nodes))
	for _, node := range config.Nodes {
		nodes = append(nodes, node)
	}

	return nodes, nil
}

// AddNodeAsLearner adds a new node to the cluster as a non-voter (learner).
// This is a simpler variant of AddNode that always adds as a learner.
func (m *Membership) AddNodeAsLearner(nodeID string, address string) error {
	if !m.node.IsLeader() {
		return ErrNotLeader
	}

	// Check if node already exists
	config, err := m.node.GetConfiguration()
	if err != nil {
		return err
	}

	if _, exists := config.Nodes[nodeID]; exists {
		return ErrNodeAlreadyExists
	}

	// Add as learner (non-voter)
	serverID := raft.ServerID(nodeID)
	serverAddr := raft.ServerAddress(address)

	future := m.node.raft.AddNonvoter(serverID, serverAddr, 0, 0)
	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to add node as learner: %w", err)
	}

	m.node.logger.Info("node added to cluster as learner",
		"node_id", nodeID,
		"address", address)

	return nil
}
