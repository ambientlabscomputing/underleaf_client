package updater

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Store manages persistent storage for update state
type Store struct {
	basePath    string
	statePath   string
	stagingPath string
	backupPath  string
	mu          sync.RWMutex
}

// NewStore creates a new update store
func NewStore(basePath string) *Store {
	return &Store{
		basePath:    basePath,
		statePath:   filepath.Join(basePath, "update-state.json"),
		stagingPath: filepath.Join(basePath, "staging"),
		backupPath:  filepath.Join(basePath, "backup"),
	}
}

// EnsureDirectories creates all required directories
func (s *Store) EnsureDirectories() error {
	dirs := []string{
		s.basePath,
		s.stagingPath,
		s.backupPath,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

// LoadState loads the update state from disk
func (s *Store) LoadState() (*UpdateState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Return default state if file doesn't exist
			return &UpdateState{
				Status: UpdateStatusIdle,
			}, nil
		}
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var state UpdateState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	return &state, nil
}

// SaveState saves the update state to disk
func (s *Store) SaveState(state *UpdateState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Write to temp file first
	tmpPath := s.statePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp state file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, s.statePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to save state file: %w", err)
	}

	return nil
}

// GetStagingPath returns the staging directory path
func (s *Store) GetStagingPath() string {
	return s.stagingPath
}

// GetBackupPath returns the backup directory path
func (s *Store) GetBackupPath() string {
	return s.backupPath
}

// CleanStaging removes all files from staging directory
func (s *Store) CleanStaging() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.RemoveAll(s.stagingPath); err != nil {
		return fmt.Errorf("failed to remove staging directory: %w", err)
	}

	return os.MkdirAll(s.stagingPath, 0755)
}

// CleanBackup removes all backup files
func (s *Store) CleanBackup() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.RemoveAll(s.backupPath); err != nil {
		return fmt.Errorf("failed to remove backup directory: %w", err)
	}

	return os.MkdirAll(s.backupPath, 0755)
}
