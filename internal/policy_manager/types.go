package policy_manager

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// PolicySnapshot represents a versioned, validated policy snapshot from control plane
type PolicySnapshot struct {
	// Core versioned policy from control plane (the Server.Config field)
	Version   int                    `json:"version"`
	Payload   map[string]interface{} `json:"payload"`
	Hash      string                 `json:"hash"`      // SHA256 of payload for integrity
	Timestamp time.Time              `json:"timestamp"` // When snapshot was created
	ServerID  string                 `json:"server_id"` // Which server this policy belongs to

	// Local metadata (not part of versioned snapshot, stored separately)
	LocalMeta LocalMetadata `json:"-"` // Don't serialize with snapshot
}

// LocalMetadata represents local-only configuration not synced from control plane
type LocalMetadata struct {
	ServerID      string                 `json:"server_id"`
	ServerName    string                 `json:"server_name"`
	AuthToken     string                 `json:"auth_token"`
	APIBaseURL    string                 `json:"api_base_url"`
	MyceliumSpine MyceliumSpineConfig    `json:"mycelium_spine"`
	Extra         map[string]interface{} `json:"extra,omitempty"` // Any additional local-only values
}

// MyceliumSpineConfig holds Mycelium Spine gRPC connection details
type MyceliumSpineConfig struct {
	Endpoint string `json:"endpoint"`
}

// SnapshotWithMeta combines policy snapshot and local metadata for unified access
type SnapshotWithMeta struct {
	Snapshot  PolicySnapshot `json:"snapshot"`
	LocalMeta LocalMetadata  `json:"local_meta"`
}

// ComputeHash calculates SHA256 hash of the payload
func (s *PolicySnapshot) ComputeHash() string {
	data, _ := json.Marshal(s.Payload)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// Validate checks if the snapshot is valid
func (s *PolicySnapshot) Validate() error {
	expectedHash := s.ComputeHash()
	if s.Hash != expectedHash {
		return ErrInvalidHash
	}
	if s.ServerID == "" {
		return ErrMissingServerID
	}
	return nil
}

// Age returns how old the snapshot is
func (s *PolicySnapshot) Age() time.Duration {
	return time.Since(s.Timestamp)
}

// IsStale checks if snapshot exceeds max age
func (s *PolicySnapshot) IsStale(maxAge time.Duration) bool {
	return s.Age() > maxAge
}

// NewPolicySnapshot creates a new validated policy snapshot
func NewPolicySnapshot(serverID string, version int, payload map[string]interface{}) *PolicySnapshot {
	snapshot := &PolicySnapshot{
		Version:   version,
		Payload:   payload,
		Timestamp: time.Now(),
		ServerID:  serverID,
	}
	snapshot.Hash = snapshot.ComputeHash()
	return snapshot
}

// Errors
var (
	ErrInvalidHash     = &PolicyError{Code: "invalid_hash", Message: "snapshot hash validation failed"}
	ErrMissingServerID = &PolicyError{Code: "missing_server_id", Message: "server ID is required"}
	ErrStaleSnapshot   = &PolicyError{Code: "stale_snapshot", Message: "snapshot exceeds maximum age"}
)

// PolicyError represents a policy error
type PolicyError struct {
	Code    string
	Message string
}

func (e *PolicyError) Error() string {
	return e.Message
}
