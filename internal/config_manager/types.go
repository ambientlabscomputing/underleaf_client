package config_manager

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// ConfigSnapshot represents a versioned, validated configuration snapshot
type ConfigSnapshot struct {
	// Core versioned config from control plane (the Server.Config field)
	Version   int                    `json:"version"`
	Payload   map[string]interface{} `json:"payload"`
	Hash      string                 `json:"hash"`      // SHA256 of payload for integrity
	Timestamp time.Time              `json:"timestamp"` // When snapshot was created
	ServerID  string                 `json:"server_id"` // Which server this config belongs to

	// Local metadata (not part of versioned snapshot, stored separately)
	LocalMeta LocalMetadata `json:"-"` // Don't serialize with snapshot
}

// LocalMetadata represents local-only configuration not synced from control plane
type LocalMetadata struct {
	ServerID   string                 `json:"server_id"`
	ServerName string                 `json:"server_name"`
	AuthToken  string                 `json:"auth_token"`
	APIBaseURL string                 `json:"api_base_url"`
	EventBus   EventBusConfig         `json:"event_bus"`
	Extra      map[string]interface{} `json:"extra,omitempty"` // Any additional local-only values
}

// EventBusConfig holds event bus connection details
type EventBusConfig struct {
	Endpoint       string `json:"endpoint"`
	CommitInterval string `json:"commit_interval"`
}

// SnapshotWithMeta combines snapshot and local metadata for unified access
type SnapshotWithMeta struct {
	Snapshot  ConfigSnapshot `json:"snapshot"`
	LocalMeta LocalMetadata  `json:"local_meta"`
}

// ComputeHash calculates SHA256 hash of the payload
func (s *ConfigSnapshot) ComputeHash() string {
	data, _ := json.Marshal(s.Payload)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// Validate checks if the snapshot is valid
func (s *ConfigSnapshot) Validate() error {
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
func (s *ConfigSnapshot) Age() time.Duration {
	return time.Since(s.Timestamp)
}

// IsStale checks if snapshot exceeds max age
func (s *ConfigSnapshot) IsStale(maxAge time.Duration) bool {
	return s.Age() > maxAge
}

// NewConfigSnapshot creates a new validated snapshot
func NewConfigSnapshot(serverID string, version int, payload map[string]interface{}) *ConfigSnapshot {
	snapshot := &ConfigSnapshot{
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
	ErrInvalidHash     = &ConfigError{Code: "invalid_hash", Message: "snapshot hash validation failed"}
	ErrMissingServerID = &ConfigError{Code: "missing_server_id", Message: "server ID is required"}
	ErrStaleSnapshot   = &ConfigError{Code: "stale_snapshot", Message: "snapshot exceeds maximum age"}
)

// ConfigError represents a configuration error
type ConfigError struct {
	Code    string
	Message string
}

func (e *ConfigError) Error() string {
	return e.Message
}
