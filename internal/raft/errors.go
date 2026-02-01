package raft

import "fmt"

// RaftError represents a typed error from the Raft subsystem.
type RaftError struct {
	Code    string
	Message string
}

func (e *RaftError) Error() string {
	return e.Message
}

// Predefined error codes
var (
	// ErrNotLeader is returned when an operation requires leadership.
	ErrNotLeader = &RaftError{
		Code:    "not_leader",
		Message: "operation requires leader; this node is not the current leader",
	}

	// ErrNoQuorum is returned when the cluster has lost quorum.
	ErrNoQuorum = &RaftError{
		Code:    "no_quorum",
		Message: "cluster has lost quorum; writes are not allowed",
	}

	// ErrKeyNotFound is returned when a key does not exist.
	ErrKeyNotFound = &RaftError{
		Code:    "key_not_found",
		Message: "key not found in store",
	}

	// ErrValueTooLarge is returned when a value exceeds the maximum size.
	ErrValueTooLarge = &RaftError{
		Code:    "value_too_large",
		Message: "value exceeds maximum allowed size",
	}

	// ErrStorageFull is returned when the storage budget is exceeded.
	ErrStorageFull = &RaftError{
		Code:    "storage_full",
		Message: "storage budget exceeded",
	}

	// ErrInvalidKey is returned when a key format is invalid.
	ErrInvalidKey = &RaftError{
		Code:    "invalid_key",
		Message: "key format is invalid; must be hierarchical (e.g., /org/{orgId}/cluster/{clusterId}/...)",
	}

	// ErrNodeAlreadyExists is returned when trying to add a node that already exists.
	ErrNodeAlreadyExists = &RaftError{
		Code:    "node_already_exists",
		Message: "node already exists in the cluster",
	}

	// ErrNodeNotFound is returned when a node is not found in the cluster.
	ErrNodeNotFound = &RaftError{
		Code:    "node_not_found",
		Message: "node not found in cluster",
	}

	// ErrMaintenanceModeRequired is returned when an operation requires maintenance mode.
	ErrMaintenanceModeRequired = &RaftError{
		Code:    "maintenance_mode_required",
		Message: "operation requires cluster to be in maintenance mode",
	}

	// ErrMembershipChangeInProgress is returned when a membership change is already in progress.
	ErrMembershipChangeInProgress = &RaftError{
		Code:    "membership_change_in_progress",
		Message: "another membership change is already in progress",
	}

	// ErrNodeNotSynced is returned when trying to promote a node that hasn't caught up.
	ErrNodeNotSynced = &RaftError{
		Code:    "node_not_synced",
		Message: "node has not fully synchronized with the leader",
	}

	// ErrInsufficientVoters is returned when removal would drop below minimum voters.
	ErrInsufficientVoters = &RaftError{
		Code:    "insufficient_voters",
		Message: "removal would result in insufficient voting members",
	}

	// ErrLeaseNotFound is returned when a lease does not exist.
	ErrLeaseNotFound = &RaftError{
		Code:    "lease_not_found",
		Message: "lease not found",
	}

	// ErrLeaseExpired is returned when a lease has expired.
	ErrLeaseExpired = &RaftError{
		Code:    "lease_expired",
		Message: "lease has expired",
	}

	// ErrLeaseAlreadyHeld is returned when trying to acquire a lease that's already held.
	ErrLeaseAlreadyHeld = &RaftError{
		Code:    "lease_already_held",
		Message: "lease is already held by another holder",
	}

	// ErrLockHeld is returned when trying to acquire a lock that's already held.
	ErrLockHeld = &RaftError{
		Code:    "lock_held",
		Message: "lock is already held by another process",
	}
)

// NewRaftError creates a new RaftError with a custom message.
func NewRaftError(code, message string) *RaftError {
	return &RaftError{
		Code:    code,
		Message: message,
	}
}

// NewRaftErrorf creates a new RaftError with a formatted message.
func NewRaftErrorf(code, format string, args ...interface{}) *RaftError {
	return &RaftError{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}

// IsRaftError checks if an error is a RaftError with the given code.
func IsRaftError(err error, code string) bool {
	if err == nil {
		return false
	}
	if raftErr, ok := err.(*RaftError); ok {
		return raftErr.Code == code
	}
	return false
}
