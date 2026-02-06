package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	ucrstypes "github.com/ambientlabscomputing/underleaf/capability_registry_service/types"
)

// SnapshotStore handles persistence of registry snapshots
type SnapshotStore struct {
	cacheDir string
}

// NewSnapshotStore creates a new snapshot store
func NewSnapshotStore(cacheDir string) (*SnapshotStore, error) {
	// Ensure cache directory exists
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	return &SnapshotStore{
		cacheDir: cacheDir,
	}, nil
}

// Save persists a registry snapshot to disk
func (s *SnapshotStore) Save(snapshot *ucrstypes.RegistrySnapshot) error {
	if snapshot == nil {
		return fmt.Errorf("snapshot cannot be nil")
	}

	snapshotPath := filepath.Join(s.cacheDir, "snapshot.json")

	// Marshal snapshot to JSON
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	// Write to temp file first, then atomic rename
	tempPath := snapshotPath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write snapshot to temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, snapshotPath); err != nil {
		os.Remove(tempPath) // Clean up temp file on failure
		return fmt.Errorf("failed to rename snapshot file: %w", err)
	}

	return nil
}

// Load reads a registry snapshot from disk
func (s *SnapshotStore) Load() (*ucrstypes.RegistrySnapshot, error) {
	snapshotPath := filepath.Join(s.cacheDir, "snapshot.json")

	// Check if file exists
	if _, err := os.Stat(snapshotPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("snapshot file does not exist: %w", err)
	}

	// Read snapshot file
	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read snapshot file: %w", err)
	}

	// Unmarshal snapshot
	var snapshot ucrstypes.RegistrySnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("failed to unmarshal snapshot: %w", err)
	}

	return &snapshot, nil
}

// Exists checks if a cached snapshot exists
func (s *SnapshotStore) Exists() bool {
	snapshotPath := filepath.Join(s.cacheDir, "snapshot.json")
	_, err := os.Stat(snapshotPath)
	return err == nil
}

// Delete removes the cached snapshot
func (s *SnapshotStore) Delete() error {
	snapshotPath := filepath.Join(s.cacheDir, "snapshot.json")
	if err := os.Remove(snapshotPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete snapshot: %w", err)
	}
	return nil
}

// GetPath returns the path to the snapshot file
func (s *SnapshotStore) GetPath() string {
	return filepath.Join(s.cacheDir, "snapshot.json")
}
