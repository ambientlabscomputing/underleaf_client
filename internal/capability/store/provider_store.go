package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ProviderInstance represents an installed provider instance
type ProviderInstance struct {
	ProviderID   string            `json:"provider_id"`
	Version      string            `json:"version"`
	State        string            `json:"state"` // installing, running, stopped, failed
	Capabilities []string          `json:"capabilities"`
	Endpoint     string            `json:"endpoint"`
	InstalledAt  time.Time         `json:"installed_at"`
	RuntimeID    string            `json:"runtime_id,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// ProviderState represents the persisted state of all installed providers
type ProviderState struct {
	Providers map[string]*ProviderInstance `json:"providers"` // Key: provider_id:version
	UpdatedAt time.Time                    `json:"updated_at"`
}

// ProviderStore handles persistence of installed provider state
type ProviderStore struct {
	providerDir string
	mu          sync.RWMutex
}

// NewProviderStore creates a new provider store
func NewProviderStore(providerDir string) (*ProviderStore, error) {
	// Ensure provider directory exists
	if err := os.MkdirAll(providerDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create provider directory: %w", err)
	}

	return &ProviderStore{
		providerDir: providerDir,
	}, nil
}

// Save persists the provider state to disk
func (s *ProviderStore) Save(state *ProviderState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if state == nil {
		return fmt.Errorf("provider state cannot be nil")
	}

	state.UpdatedAt = time.Now()

	statePath := filepath.Join(s.providerDir, "state.json")

	// Marshal state to JSON
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal provider state: %w", err)
	}

	// Write to temp file first, then atomic rename
	tempPath := statePath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write provider state to temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, statePath); err != nil {
		os.Remove(tempPath) // Clean up temp file on failure
		return fmt.Errorf("failed to rename provider state file: %w", err)
	}

	return nil
}

// Load reads the provider state from disk
func (s *ProviderStore) Load() (*ProviderState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	statePath := filepath.Join(s.providerDir, "state.json")

	// Check if file exists
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		// Return empty state if file doesn't exist
		return &ProviderState{
			Providers: make(map[string]*ProviderInstance),
			UpdatedAt: time.Now(),
		}, nil
	}

	// Read state file
	data, err := os.ReadFile(statePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read provider state file: %w", err)
	}

	// Unmarshal state
	var state ProviderState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal provider state: %w", err)
	}

	// Ensure providers map is initialized
	if state.Providers == nil {
		state.Providers = make(map[string]*ProviderInstance)
	}

	return &state, nil
}

// SaveProvider saves or updates a single provider instance
func (s *ProviderStore) SaveProvider(instance *ProviderInstance) error {
	if instance == nil {
		return fmt.Errorf("provider instance cannot be nil")
	}

	// Load current state
	state, err := s.Load()
	if err != nil {
		return fmt.Errorf("failed to load provider state: %w", err)
	}

	// Update or add provider
	providerKey := fmt.Sprintf("%s:%s", instance.ProviderID, instance.Version)
	state.Providers[providerKey] = instance

	// Save updated state
	return s.Save(state)
}

// GetProvider retrieves a single provider instance
func (s *ProviderStore) GetProvider(providerID, version string) (*ProviderInstance, error) {
	state, err := s.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load provider state: %w", err)
	}

	providerKey := fmt.Sprintf("%s:%s", providerID, version)
	instance, exists := state.Providers[providerKey]
	if !exists {
		return nil, fmt.Errorf("provider not found: %s:%s", providerID, version)
	}

	return instance, nil
}

// DeleteProvider removes a provider from the state
func (s *ProviderStore) DeleteProvider(providerID, version string) error {
	state, err := s.Load()
	if err != nil {
		return fmt.Errorf("failed to load provider state: %w", err)
	}

	providerKey := fmt.Sprintf("%s:%s", providerID, version)
	delete(state.Providers, providerKey)

	return s.Save(state)
}

// ListProviders returns all installed provider instances
func (s *ProviderStore) ListProviders() ([]*ProviderInstance, error) {
	state, err := s.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load provider state: %w", err)
	}

	instances := make([]*ProviderInstance, 0, len(state.Providers))
	for _, instance := range state.Providers {
		instances = append(instances, instance)
	}

	return instances, nil
}

// Exists checks if a provider state file exists
func (s *ProviderStore) Exists() bool {
	statePath := filepath.Join(s.providerDir, "state.json")
	_, err := os.Stat(statePath)
	return err == nil
}
