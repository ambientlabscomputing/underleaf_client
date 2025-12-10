package config_manager

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ambientlabscomputing/underleaf_client/pkg/defaults"
)

// SnapshotConfigClient implements ConfigClient using snapshot manager
// Provides unified access to both versioned snapshot and local metadata
type SnapshotConfigClient struct {
	manager   *SnapshotConfigManager
	store     *Store
	isRuntime bool // true if manager is running, false if just reading from disk
}

// NewSnapshotConfigClient creates a config client backed by snapshot manager
func NewSnapshotConfigClient(ctx context.Context, serverID string, basePath string, isAgent bool) (*SnapshotConfigClient, error) {
	store := NewStore(basePath, isAgent)

	// Try to load from disk first
	_, err := store.LoadSnapshotWithMeta()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	return &SnapshotConfigClient{
		store:     store,
		isRuntime: false, // Not managing runtime sync, just reading
	}, nil
}

// NewSnapshotConfigClientWithManager creates a client with an active manager
func NewSnapshotConfigClientWithManager(manager *SnapshotConfigManager, store *Store) *SnapshotConfigClient {
	return &SnapshotConfigClient{
		manager:   manager,
		store:     store,
		isRuntime: true,
	}
}

// Get retrieves a value from either snapshot or local metadata
// Keys starting with "local." are from local metadata, others from snapshot
func (c *SnapshotConfigClient) Get(key string) (interface{}, bool) {
	data, err := c.store.LoadSnapshotWithMeta()
	if err != nil {
		return nil, false
	}

	// Check if it's a local metadata key
	if len(key) > 6 && key[:6] == "local." {
		localKey := key[6:]
		return c.getFromLocalMeta(&data.LocalMeta, localKey)
	}

	// Check snapshot payload
	if val, ok := data.Snapshot.Payload[key]; ok {
		return val, true
	}

	// Special keys for snapshot metadata
	switch key {
	case "snapshot.version":
		return data.Snapshot.Version, true
	case "snapshot.timestamp":
		return data.Snapshot.Timestamp, true
	case "snapshot.server_id":
		return data.Snapshot.ServerID, true
	case "snapshot.age":
		return data.Snapshot.Age().String(), true
	}

	return nil, false
}

// getFromLocalMeta retrieves value from local metadata by key path
func (c *SnapshotConfigClient) getFromLocalMeta(meta *LocalMetadata, key string) (interface{}, bool) {
	switch key {
	case "server_id":
		return meta.ServerID, meta.ServerID != ""
	case "server_name":
		return meta.ServerName, meta.ServerName != ""
	case "auth.token":
		return meta.AuthToken, meta.AuthToken != ""
	case "api.base_url":
		if meta.APIBaseURL != "" {
			return meta.APIBaseURL, true
		}
		return defaults.APIBaseURL, true
	case "event_bus.endpoint":
		if meta.EventBus.Endpoint != "" {
			return meta.EventBus.Endpoint, true
		}
		return defaults.EventBusEndpoint, true
	case "event_bus.commit_interval":
		return meta.EventBus.CommitInterval, meta.EventBus.CommitInterval != ""
	default:
		// Check extra fields
		if val, ok := meta.Extra[key]; ok {
			return val, true
		}
	}
	return nil, false
}

// Set sets a value in local metadata only (snapshot is read-only, managed by sync)
func (c *SnapshotConfigClient) Set(key string, value interface{}) error {
	meta, err := c.store.LoadLocalMeta()
	if err != nil {
		meta = &LocalMetadata{Extra: make(map[string]interface{})}
	}

	// Only allow setting local metadata
	if len(key) > 6 && key[:6] == "local." {
		key = key[6:]
	}

	// Map to appropriate field
	switch key {
	case "server_id":
		if v, ok := value.(string); ok {
			meta.ServerID = v
		}
	case "server_name":
		if v, ok := value.(string); ok {
			meta.ServerName = v
		}
	case "auth.token":
		if v, ok := value.(string); ok {
			meta.AuthToken = v
		}
	case "api.base_url":
		if v, ok := value.(string); ok {
			meta.APIBaseURL = v
		}
	case "event_bus.endpoint":
		if v, ok := value.(string); ok {
			meta.EventBus.Endpoint = v
		}
	case "event_bus.commit_interval":
		if v, ok := value.(string); ok {
			meta.EventBus.CommitInterval = v
		}
	default:
		// Store in extra
		if meta.Extra == nil {
			meta.Extra = make(map[string]interface{})
		}
		meta.Extra[key] = value
	}

	return c.store.SaveLocalMeta(meta)
}

// Delete removes a value from local metadata (snapshot is read-only)
func (c *SnapshotConfigClient) Delete(key string) error {
	meta, err := c.store.LoadLocalMeta()
	if err != nil {
		return fmt.Errorf("failed to load local metadata: %w", err)
	}

	// Only allow deleting local metadata
	if len(key) > 6 && key[:6] == "local." {
		key = key[6:]
	}

	// Clear appropriate field
	switch key {
	case "server_id":
		meta.ServerID = ""
	case "server_name":
		meta.ServerName = ""
	case "auth.token":
		meta.AuthToken = ""
	case "api.base_url":
		meta.APIBaseURL = ""
	case "event_bus.endpoint":
		meta.EventBus.Endpoint = ""
	case "event_bus.commit_interval":
		meta.EventBus.CommitInterval = ""
	default:
		// Delete from extra
		if meta.Extra != nil {
			delete(meta.Extra, key)
		}
	}

	return c.store.SaveLocalMeta(meta)
}

// Config returns the full configuration (snapshot + local metadata merged)
func (c *SnapshotConfigClient) Config() Configuration {
	data, err := c.store.LoadSnapshotWithMeta()
	if err != nil {
		return Configuration{
			Version: 0,
			Payload: make(map[string]interface{}),
		}
	}

	// Merge snapshot and local metadata into single payload
	merged := make(map[string]interface{})

	// Add snapshot payload
	for k, v := range data.Snapshot.Payload {
		merged[k] = v
	}

	// Add local metadata under "local" prefix
	merged["local.server_id"] = data.LocalMeta.ServerID
	merged["local.server_name"] = data.LocalMeta.ServerName
	merged["local.auth.token"] = data.LocalMeta.AuthToken
	merged["local.api.base_url"] = data.LocalMeta.APIBaseURL
	merged["local.event_bus.endpoint"] = data.LocalMeta.EventBus.Endpoint
	merged["local.event_bus.commit_interval"] = data.LocalMeta.EventBus.CommitInterval

	// Add extra fields
	for k, v := range data.LocalMeta.Extra {
		merged["local."+k] = v
	}

	return Configuration{
		Version: data.Snapshot.Version,
		Payload: merged,
	}
}

// ConfigClientInfo returns information about this config client
func (c *SnapshotConfigClient) ConfigClientInfo() map[string]interface{} {
	info := map[string]interface{}{
		"type":          "SnapshotConfigClient",
		"snapshot_path": c.store.GetSnapshotPath(),
		"local_path":    c.store.GetLocalPath(),
		"runtime":       c.isRuntime,
	}

	if c.manager != nil {
		snapshot, _ := c.manager.GetSnapshot()
		if snapshot != nil {
			info["version"] = snapshot.Version
			info["age"] = snapshot.Age().String()
			info["is_stale"] = snapshot.IsStale(c.manager.maxAge)
		}
	}

	return info
}

// GetBasePath returns appropriate base path for config storage
// Uses XDG Base Directory specification on Unix and standard locations on other platforms
func GetBasePath(isAgent bool) string {
	if isAgent {
		// Agent uses XDG_STATE_HOME or falls back to user home
		// This is user-writable without requiring root
		if stateHome := os.Getenv("XDG_STATE_HOME"); stateHome != "" {
			return filepath.Join(stateHome, "underleaf")
		}
		// Fallback to ~/.local/state/underleaf (XDG default)
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".local", "state", "underleaf")
		}
		// Last resort: current directory
		return ".underleaf-agent"
	}

	// CLI uses XDG_CONFIG_HOME or falls back to ~/.config
	if configHome := os.Getenv("XDG_CONFIG_HOME"); configHome != "" {
		return filepath.Join(configHome, "underleaf")
	}
	// Fallback to ~/.config/underleaf (XDG default) or ~/.underleaf (traditional)
	if home, err := os.UserHomeDir(); err == nil {
		// Use ~/.underleaf for simplicity (matches existing usage)
		return filepath.Join(home, ".underleaf")
	}
	// Last resort: current directory
	return ".underleaf"
}
