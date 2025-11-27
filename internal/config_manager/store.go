package config_manager

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

// Store handles persistent storage of config snapshots and local metadata
type Store struct {
	snapshotPath string
	localPath    string
	mu           sync.RWMutex
}

// NewStore creates a new config store
func NewStore(basePath string, isAgent bool) *Store {
	var snapshotPath, localPath string

	if isAgent {
		// Agent uses system-level paths
		snapshotPath = filepath.Join(basePath, "snapshot.yaml")
		localPath = filepath.Join(basePath, "local.yaml")
	} else {
		// CLI uses user home directory
		snapshotPath = filepath.Join(basePath, "snapshot.yaml")
		localPath = filepath.Join(basePath, "config.yaml") // Keep existing config.yaml for CLI
	}

	return &Store{
		snapshotPath: snapshotPath,
		localPath:    localPath,
	}
}

// SaveSnapshot atomically saves a config snapshot
func (s *Store) SaveSnapshot(snapshot *ConfigSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate before saving
	if err := snapshot.Validate(); err != nil {
		return fmt.Errorf("invalid snapshot: %w", err)
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(s.snapshotPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write to temp file first (atomic write)
	tempPath := s.snapshotPath + ".tmp"
	data, err := yaml.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, s.snapshotPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	slog.Info("snapshot saved", "server_id", snapshot.ServerID, "version", snapshot.Version, "age", snapshot.Age())
	return nil
}

// LoadSnapshot loads the config snapshot from disk
func (s *Store) LoadSnapshot() (*ConfigSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.snapshotPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No snapshot yet
		}
		return nil, fmt.Errorf("failed to read snapshot: %w", err)
	}

	var snapshot ConfigSnapshot
	if err := yaml.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("failed to unmarshal snapshot: %w", err)
	}

	// Validate loaded snapshot
	if err := snapshot.Validate(); err != nil {
		slog.Warn("loaded snapshot failed validation", "error", err)
		return nil, fmt.Errorf("snapshot validation failed: %w", err)
	}

	return &snapshot, nil
}

// SaveLocalMeta saves local metadata
func (s *Store) SaveLocalMeta(meta *LocalMetadata) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(s.localPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	data, err := yaml.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal local metadata: %w", err)
	}

	if err := os.WriteFile(s.localPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write local metadata: %w", err)
	}

	return nil
}

// LoadLocalMeta loads local metadata
func (s *Store) LoadLocalMeta() (*LocalMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.localPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &LocalMetadata{}, nil // Return empty metadata
		}
		return nil, fmt.Errorf("failed to read local metadata: %w", err)
	}

	var meta LocalMetadata
	if err := yaml.Unmarshal(data, &meta); err != nil {
		// Try JSON as fallback (for existing config.yaml files)
		if err := json.Unmarshal(data, &meta); err != nil {
			return nil, fmt.Errorf("failed to unmarshal local metadata: %w", err)
		}
	}

	return &meta, nil
}

// LoadSnapshotWithMeta loads both snapshot and local metadata
func (s *Store) LoadSnapshotWithMeta() (*SnapshotWithMeta, error) {
	snapshot, err := s.LoadSnapshot()
	if err != nil {
		return nil, err
	}

	meta, err := s.LoadLocalMeta()
	if err != nil {
		return nil, err
	}

	// If no snapshot, return empty one
	if snapshot == nil {
		snapshot = &ConfigSnapshot{
			Payload: make(map[string]interface{}),
		}
	}

	return &SnapshotWithMeta{
		Snapshot:  *snapshot,
		LocalMeta: *meta,
	}, nil
}

// GetSnapshotPath returns the snapshot file path
func (s *Store) GetSnapshotPath() string {
	return s.snapshotPath
}

// GetLocalPath returns the local metadata file path
func (s *Store) GetLocalPath() string {
	return s.localPath
}
