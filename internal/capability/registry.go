package capability

import (
	"fmt"
	"log/slog"
	"sync"
)

// Registry provides fast in-memory access to capabilities and providers
type Registry struct {
	snapshot *RegistrySnapshot

	// Indexes for fast lookups
	capabilitiesById      map[string]*Capability // Key: capability.ID
	providersByCapability map[string][]*Provider // Key: capability.ID
	providersById         map[string]*Provider   // Key: provider_id:version

	mu sync.RWMutex
}

// NewRegistry creates a new empty registry
func NewRegistry() *Registry {
	return &Registry{
		capabilitiesById:      make(map[string]*Capability),
		providersByCapability: make(map[string][]*Provider),
		providersById:         make(map[string]*Provider),
	}
}

// LoadSnapshot loads a registry snapshot and rebuilds indexes
func (r *Registry) LoadSnapshot(snapshot *RegistrySnapshot) error {
	if snapshot == nil {
		return fmt.Errorf("snapshot cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Clear existing indexes
	r.capabilitiesById = make(map[string]*Capability)
	r.providersByCapability = make(map[string][]*Provider)
	r.providersById = make(map[string]*Provider)

	// Index capabilities
	for i := range snapshot.Capabilities {
		cap := &snapshot.Capabilities[i]
		r.capabilitiesById[cap.ID] = cap
	}

	// Index providers
	for i := range snapshot.Providers {
		provider := &snapshot.Providers[i]

		// Index by provider ID:version
		providerKey := fmt.Sprintf("%s:%s", provider.ProviderID, provider.Version)
		r.providersById[providerKey] = provider

		// Index by capability ID
		for _, capRef := range provider.Capabilities {
			r.providersByCapability[capRef.ID] = append(r.providersByCapability[capRef.ID], provider)
		}
	}

	r.snapshot = snapshot

	slog.Info("registry snapshot loaded",
		"version", snapshot.Version,
		"capabilities", len(r.capabilitiesById),
		"providers", len(r.providersById))

	return nil
}

// GetCapability retrieves a capability by ID
func (r *Registry) GetCapability(id string) (*Capability, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cap, exists := r.capabilitiesById[id]
	if !exists {
		return nil, fmt.Errorf("capability not found: %s", id)
	}

	return cap, nil
}

// ListCapabilities returns all capabilities
func (r *Registry) ListCapabilities() []*Capability {
	r.mu.RLock()
	defer r.mu.RUnlock()

	capabilities := make([]*Capability, 0, len(r.capabilitiesById))
	for _, cap := range r.capabilitiesById {
		capabilities = append(capabilities, cap)
	}

	return capabilities
}

// FindProviders returns all providers that implement a given capability
func (r *Registry) FindProviders(capabilityID string) ([]*Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Check if capability exists
	if _, exists := r.capabilitiesById[capabilityID]; !exists {
		return nil, fmt.Errorf("capability not found: %s", capabilityID)
	}

	// Get providers for this capability
	providers, exists := r.providersByCapability[capabilityID]
	if !exists || len(providers) == 0 {
		return nil, fmt.Errorf("no providers found for capability: %s", capabilityID)
	}

	// Return a copy to prevent external modifications
	result := make([]*Provider, len(providers))
	copy(result, providers)

	return result, nil
}

// GetProvider retrieves a specific provider by ID and version
func (r *Registry) GetProvider(providerID, version string) (*Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	providerKey := fmt.Sprintf("%s:%s", providerID, version)
	provider, exists := r.providersById[providerKey]
	if !exists {
		return nil, fmt.Errorf("provider not found: %s:%s", providerID, version)
	}

	return provider, nil
}

// FindProviderByID finds a provider by just provider ID (returns latest version)
func (r *Registry) FindProviderByID(providerID string) (*Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var best *Provider
	for _, provider := range r.providersById {
		if provider.ProviderID == providerID {
			if best == nil || provider.Version > best.Version {
				best = provider
			}
		}
	}

	if best == nil {
		return nil, fmt.Errorf("provider not found: %s", providerID)
	}

	return best, nil
}

// ListProviders returns all providers
func (r *Registry) ListProviders() []*Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	providers := make([]*Provider, 0, len(r.providersById))
	for _, provider := range r.providersById {
		providers = append(providers, provider)
	}

	return providers
}

// GetSnapshot returns the current snapshot (read-only)
func (r *Registry) GetSnapshot() *RegistrySnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.snapshot
}

// Stats returns registry statistics
func (r *Registry) Stats() RegistryStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return RegistryStats{
		CapabilityCount: len(r.capabilitiesById),
		ProviderCount:   len(r.providersById),
		Version:         r.getSnapshotVersion(),
	}
}

// getSnapshotVersion returns the snapshot version (must be called with lock held)
func (r *Registry) getSnapshotVersion() string {
	if r.snapshot == nil {
		return ""
	}
	return r.snapshot.Version
}

// RegistryStats contains registry statistics
type RegistryStats struct {
	CapabilityCount int
	ProviderCount   int
	Version         string
}
