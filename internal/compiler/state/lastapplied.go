package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/ambientlabscomputing/underleaf_client/internal/types"
)

// LastAppliedStore persists and retrieves last applied state
type LastAppliedStore interface {
	GetLatest(deploymentID string) (*types.LastAppliedSnapshot, error)
	GetVersion(deploymentID string, version int) (*types.LastAppliedSnapshot, error)
	Save(snapshot *types.LastAppliedSnapshot) error
	ListVersions(deploymentID string) ([]int, error)
	Delete(deploymentID string, version int) error
}

// FileLastAppliedStore implements LastAppliedStore using JSON files
type FileLastAppliedStore struct {
	basePath string
}

// NewFileLastAppliedStore creates a new file-based last applied store
func NewFileLastAppliedStore(basePath string) *FileLastAppliedStore {
	return &FileLastAppliedStore{
		basePath: basePath,
	}
}

// GetLatest retrieves the most recent snapshot for a deployment
func (s *FileLastAppliedStore) GetLatest(deploymentID string) (*types.LastAppliedSnapshot, error) {
	latestPath := filepath.Join(s.basePath, deploymentID, "latest.json")

	data, err := os.ReadFile(latestPath)
	if err != nil {
		return nil, fmt.Errorf("no latest snapshot found: %w", err)
	}

	var snapshot types.LastAppliedSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("failed to unmarshal snapshot: %w", err)
	}

	return &snapshot, nil
}

// GetVersion retrieves a specific version snapshot for a deployment
func (s *FileLastAppliedStore) GetVersion(deploymentID string, version int) (*types.LastAppliedSnapshot, error) {
	versionPath := filepath.Join(s.basePath, deploymentID, fmt.Sprintf("v%d.json", version))

	data, err := os.ReadFile(versionPath)
	if err != nil {
		return nil, fmt.Errorf("snapshot v%d not found: %w", version, err)
	}

	var snapshot types.LastAppliedSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("failed to unmarshal snapshot: %w", err)
	}

	return &snapshot, nil
}

// Save persists a snapshot
func (s *FileLastAppliedStore) Save(snapshot *types.LastAppliedSnapshot) error {
	deploymentDir := filepath.Join(s.basePath, snapshot.DeploymentID)

	// Ensure directory exists
	if err := os.MkdirAll(deploymentDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Marshal snapshot
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	// Write versioned file
	versionPath := filepath.Join(deploymentDir, fmt.Sprintf("v%d.json", snapshot.Version))
	if err := os.WriteFile(versionPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write versioned snapshot: %w", err)
	}

	// Write/update latest.json
	latestPath := filepath.Join(deploymentDir, "latest.json")
	if err := os.WriteFile(latestPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write latest snapshot: %w", err)
	}

	return nil
}

// ListVersions returns all available versions for a deployment
func (s *FileLastAppliedStore) ListVersions(deploymentID string) ([]int, error) {
	deploymentDir := filepath.Join(s.basePath, deploymentID)

	entries, err := os.ReadDir(deploymentDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []int{}, nil
		}
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var versions []int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		var version int
		if _, err := fmt.Sscanf(entry.Name(), "v%d.json", &version); err == nil {
			versions = append(versions, version)
		}
	}

	sort.Ints(versions)
	return versions, nil
}

// Delete removes a specific version snapshot
func (s *FileLastAppliedStore) Delete(deploymentID string, version int) error {
	versionPath := filepath.Join(s.basePath, deploymentID, fmt.Sprintf("v%d.json", version))

	if err := os.Remove(versionPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete snapshot: %w", err)
	}

	return nil
}
