package raft

import (
	"time"

	"github.com/hashicorp/raft"
)

// NodeRole represents the role of a node in the Raft cluster.
type NodeRole string

const (
	// RoleLeader indicates the node is the active leader.
	RoleLeader NodeRole = "leader"

	// RoleFollower indicates the node is a voting follower.
	RoleFollower NodeRole = "follower"

	// RoleLearner indicates the node is a non-voting learner.
	RoleLearner NodeRole = "learner"

	// RoleCandidate indicates the node is a candidate in an election.
	RoleCandidate NodeRole = "candidate"
)

// ReadMode specifies the consistency level for read operations.
type ReadMode string

const (
	// ReadModeLinearizable ensures reads reflect the latest committed state.
	// Requires leader confirmation.
	ReadModeLinearizable ReadMode = "linearizable"

	// ReadModeStale allows reads from local cache, may lag behind leader.
	// No leader confirmation required.
	ReadModeStale ReadMode = "stale"
)

// NodeConfig configures a single Raft node.
type NodeConfig struct {
	// NodeID is the unique identifier for this node.
	NodeID string

	// BindAddr is the address to bind the Raft gRPC server to.
	BindAddr string

	// AdvertiseAddr is the address other nodes use to reach this node.
	// If empty, defaults to BindAddr.
	AdvertiseAddr string

	// DataDir is the directory for persistent Raft data (logs, snapshots, stable store).
	DataDir string

	// Bootstrap indicates whether this is a new cluster bootstrap.
	Bootstrap bool

	// BootstrapPeers are the initial peers for a new cluster (for multi-node bootstrap).
	BootstrapPeers []string

	// HeartbeatTimeout is the time in follower state without a leader before attempting an election.
	HeartbeatTimeout time.Duration

	// ElectionTimeout is the time in candidate state without winning before restarting election.
	ElectionTimeout time.Duration

	// LeaderLeaseTimeout is how long leadership is maintained after quorum is lost.
	LeaderLeaseTimeout time.Duration

	// SnapshotInterval is how often to check for snapshot creation.
	SnapshotInterval time.Duration

	// SnapshotThreshold is the number of log entries before triggering a snapshot.
	SnapshotThreshold uint64

	// MaxValueSize is the maximum size in bytes for a single KV value.
	MaxValueSize int

	// MaxStorageSize is the maximum total storage budget in bytes.
	MaxStorageSize int64
}

// DefaultNodeConfig returns a NodeConfig with sensible defaults for edge deployments.
func DefaultNodeConfig() *NodeConfig {
	return &NodeConfig{
		HeartbeatTimeout:   1000 * time.Millisecond,
		ElectionTimeout:    1000 * time.Millisecond,
		LeaderLeaseTimeout: 500 * time.Millisecond,
		SnapshotInterval:   120 * time.Second,
		SnapshotThreshold:  8192,
		MaxValueSize:       1024 * 1024,       // 1 MB
		MaxStorageSize:     100 * 1024 * 1024, // 100 MB
	}
}

// ClusterConfig represents the configuration of the entire Raft cluster.
type ClusterConfig struct {
	// Nodes is the set of all nodes in the cluster.
	Nodes map[string]*NodeInfo

	// MaintenanceMode indicates if the cluster is in maintenance mode.
	MaintenanceMode bool
}

// NodeInfo contains information about a node in the cluster.
type NodeInfo struct {
	// ID is the node's unique identifier.
	ID string

	// Address is the node's advertise address.
	Address string

	// Role is the node's current role.
	Role NodeRole

	// Suffrage indicates voting status.
	Suffrage raft.ServerSuffrage

	// LastContact is the last time the leader heard from this node.
	LastContact time.Time

	// IsLeader indicates if this node is the current leader.
	IsLeader bool
}

// KVEntry represents a key-value entry in the store.
type KVEntry struct {
	// Key is the hierarchical key.
	Key string

	// Value is the stored value.
	Value []byte

	// Revision is the global monotonic revision number when this entry was created/updated.
	Revision uint64

	// CreatedRevision is the revision when the key was first created.
	CreatedRevision uint64

	// ModifiedRevision is the revision of the last modification.
	ModifiedRevision uint64

	// Version is the number of modifications to this key.
	Version int64

	// CreateTime is when the key was created.
	CreateTime time.Time

	// ModTime is when the key was last modified.
	ModTime time.Time
}

// LeaseInfo represents information about a lease.
type LeaseInfo struct {
	// ID is the lease identifier.
	ID string

	// TTL is the time-to-live in seconds.
	TTL int64

	// GrantedAt is when the lease was granted.
	GrantedAt time.Time

	// ExpiresAt is when the lease expires.
	ExpiresAt time.Time

	// Holder is the entity holding the lease.
	Holder string

	// Keys are the keys attached to this lease.
	Keys []string
}

// LockInfo represents information about a lock.
type LockInfo struct {
	// Key is the lock key.
	Key string

	// LeaseID is the backing lease ID.
	LeaseID string

	// Holder is the entity holding the lock.
	Holder string

	// AcquiredAt is when the lock was acquired.
	AcquiredAt time.Time
}

// WatchEvent represents a change event in the KV store.
type WatchEvent struct {
	// Type is the type of change (put, delete).
	Type WatchEventType

	// Key is the affected key.
	Key string

	// Value is the new value (nil for delete).
	Value []byte

	// Revision is the revision number of this change.
	Revision uint64

	// PrevValue is the previous value (if any).
	PrevValue []byte
}

// WatchEventType represents the type of watch event.
type WatchEventType string

const (
	// WatchEventPut indicates a key was created or updated.
	WatchEventPut WatchEventType = "put"

	// WatchEventDelete indicates a key was deleted.
	WatchEventDelete WatchEventType = "delete"
)

// ClusterStats provides statistics about the cluster state.
type ClusterStats struct {
	// NodeID is this node's ID.
	NodeID string

	// Role is this node's current role.
	Role NodeRole

	// LeaderID is the current leader's ID.
	LeaderID string

	// Term is the current Raft term.
	Term uint64

	// CommitIndex is the highest committed log index.
	CommitIndex uint64

	// AppliedIndex is the highest applied log index.
	AppliedIndex uint64

	// LastLogIndex is the index of the last log entry.
	LastLogIndex uint64

	// NumPeers is the number of peers in the cluster.
	NumPeers int

	// Revision is the current KV store revision.
	Revision uint64

	// KeyCount is the number of keys in the store.
	KeyCount int

	// StorageSize is the approximate storage size in bytes.
	StorageSize int64
}
