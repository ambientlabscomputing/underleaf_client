package capability

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/ambientlabscomputing/underleaf_client/internal/capability/mcp"
	"github.com/ambientlabscomputing/underleaf_client/internal/capability/store"
	"github.com/moby/moby/client"
)

// Manager orchestrates capability resolution and provider management
type Manager struct {
	config       Config
	syncClient   *SyncClient
	registry     *Registry
	resolver     *Resolver
	lifecycle    *LifecycleManager
	dockerClient *client.Client

	// Runtime state
	mcpClients map[string]*mcp.Client // Key: provider_id:version
}

// NewManager creates a new capability manager
func NewManager(dockerClient *client.Client, config Config) (*Manager, error) {
	// Validate config
	if config.UCRSBaseURL == "" {
		return nil, fmt.Errorf("UCRS base URL is required")
	}
	if config.PublicKeyPath == "" {
		return nil, fmt.Errorf("public key path is required")
	}
	if config.CacheDir == "" {
		return nil, fmt.Errorf("cache directory is required")
	}
	if config.ProviderDir == "" {
		return nil, fmt.Errorf("provider directory is required")
	}

	// Create snapshot store
	snapshotStore, err := store.NewSnapshotStore(config.CacheDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot store: %w", err)
	}

	// Create registry
	registry := NewRegistry()

	// Create sync client
	syncClient, err := NewSyncClient(SyncClientConfig{
		UCRSBaseURL:   config.UCRSBaseURL,
		PublicKeyPath: config.PublicKeyPath,
		Store:         snapshotStore,
		SyncInterval:  config.SyncInterval,
		OnUpdate: func(snapshot *RegistrySnapshot) {
			// Update registry when snapshot changes
			if err := registry.LoadSnapshot(snapshot); err != nil {
				slog.Error("failed to load registry snapshot", "error", err)
			}
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create sync client: %w", err)
	}

	// Create resolver
	resolver := NewResolver(registry)

	// Create provider store
	providerStore, err := store.NewProviderStore(config.ProviderDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider store: %w", err)
	}

	// Create lifecycle manager
	lifecycle, err := NewLifecycleManager(dockerClient, providerStore, LifecycleConfig{
		Network:     config.Network,
		ProviderDir: config.ProviderDir,
		MemoryLimit: config.MemoryLimit,
		CPULimit:    config.CPULimit,
		LogDir:      filepath.Join(config.ProviderDir, "logs"),
		StagingDir:  filepath.Join(config.ProviderDir, "staging"),
	}, slog.Default())
	if err != nil {
		return nil, fmt.Errorf("failed to create lifecycle manager: %w", err)
	}

	return &Manager{
		config:       config,
		syncClient:   syncClient,
		registry:     registry,
		resolver:     resolver,
		lifecycle:    lifecycle,
		dockerClient: dockerClient,
		mcpClients:   make(map[string]*mcp.Client),
	}, nil
}

// Start starts the capability manager
func (m *Manager) Start(ctx context.Context) error {
	slog.Info("starting capability manager")

	// Start sync client
	if err := m.syncClient.Start(ctx); err != nil {
		return fmt.Errorf("failed to start sync client: %w", err)
	}

	slog.Info("capability manager started successfully")
	return nil
}

// Stop gracefully stops the capability manager
func (m *Manager) Stop(ctx context.Context) error {
	slog.Info("stopping capability manager")

	// Stop sync client
	if err := m.syncClient.Stop(ctx); err != nil {
		slog.Warn("failed to stop sync client", "error", err)
	}

	// Close all MCP clients
	for key, client := range m.mcpClients {
		if err := client.Close(); err != nil {
			slog.Warn("failed to close MCP client", "provider", key, "error", err)
		}
	}

	slog.Info("capability manager stopped")
	return nil
}

// EnsureCapability ensures a capability is available and returns the provider endpoint
func (m *Manager) EnsureCapability(ctx context.Context, req CapabilityRequest) (*ProviderEndpoint, error) {
	slog.Info("ensuring capability", "capability_id", req.CapabilityID, "version_range", req.VersionRange)

	// Resolve provider
	provider, capability, err := m.resolver.Resolve(req)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve capability: %w", err)
	}

	slog.Debug("resolved provider", "provider_id", provider.ProviderID, "version", provider.Version)

	// Check if provider is already installed
	providerKey := fmt.Sprintf("%s:%s", provider.ProviderID, provider.Version)
	installed, err := m.lifecycle.ListInstalled(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list installed providers: %w", err)
	}

	var providerInstance *store.ProviderInstance
	for _, inst := range installed {
		if inst.ProviderID == provider.ProviderID && inst.Version == provider.Version {
			providerInstance = inst
			break
		}
	}

	// Install if not installed
	if providerInstance == nil {
		slog.Info("installing provider", "provider_id", provider.ProviderID, "version", provider.Version)
		providerInstance, err = m.lifecycle.Install(ctx, provider)
		if err != nil {
			return nil, fmt.Errorf("failed to install provider: %w", err)
		}
	}

	// Start if not running
	if providerInstance.State != string(ProviderStateRunning) {
		slog.Info("starting provider", "provider_id", provider.ProviderID, "version", provider.Version)
		if err := m.lifecycle.Start(ctx, provider.ProviderID, provider.Version); err != nil {
			return nil, fmt.Errorf("failed to start provider: %w", err)
		}
		providerInstance.State = string(ProviderStateRunning)
	}

	// Create MCP client if not exists
	if _, exists := m.mcpClients[providerKey]; !exists {
		// TODO: Initialize MCP client with Unix socket transport
		slog.Debug("MCP client initialization deferred", "provider_key", providerKey)
	}

	return &ProviderEndpoint{
		Provider:   provider,
		Capability: capability,
		Endpoint:   providerInstance.Endpoint,
		State:      ProviderState(providerInstance.State),
	}, nil
}

// ResolveCapability resolves a capability to a provider endpoint (without installing)
func (m *Manager) ResolveCapability(ctx context.Context, capabilityID string) (*ProviderEndpoint, error) {
	// Check if any installed provider provides this capability
	installed, err := m.lifecycle.ListInstalled(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list installed providers: %w", err)
	}

	for _, inst := range installed {
		for _, capID := range inst.Capabilities {
			if capID == capabilityID {
				// Get provider metadata from registry
				provider, err := m.registry.GetProvider(inst.ProviderID, inst.Version)
				if err != nil {
					continue
				}

				// Get capability metadata
				capability, err := m.registry.GetCapability(capabilityID)
				if err != nil {
					continue
				}

				return &ProviderEndpoint{
					Provider:   provider,
					Capability: capability,
					Endpoint:   inst.Endpoint,
					State:      ProviderState(inst.State),
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("no installed provider found for capability: %s", capabilityID)
}

// ListAvailableCapabilities returns all capabilities from the registry
func (m *Manager) ListAvailableCapabilities(ctx context.Context) ([]*Capability, error) {
	return m.registry.ListCapabilities(), nil
}

// ListInstalledProviders returns all installed provider instances
func (m *Manager) ListInstalledProviders(ctx context.Context) ([]*store.ProviderInstance, error) {
	return m.lifecycle.ListInstalled(ctx)
}

// GetRegistryStats returns registry statistics
func (m *Manager) GetRegistryStats() RegistryStats {
	return m.registry.Stats()
}

// SetProviderEnvOverride registers additional environment variables that will be
// merged into a provider's env each time it is (re)started. Calling this before
// InstallProviderByID/EnsureCapability ensures the overrides are applied on first
// start. Calling it while a provider is already running takes effect on the next
// restart (the agent restarts MMA when a new binary is deployed).
func (m *Manager) SetProviderEnvOverride(providerID string, env map[string]string) {
	m.lifecycle.SetEnvOverride(providerID, env)
}

// InstallProviderByID installs a provider by its provider ID (not capability ID)
func (m *Manager) InstallProviderByID(ctx context.Context, providerID string) (*ProviderEndpoint, error) {
	slog.Info("installing provider by ID", "provider_id", providerID)

	// Look up provider in registry
	provider, err := m.registry.FindProviderByID(providerID)
	if err != nil {
		return nil, fmt.Errorf("provider not found in registry: %w", err)
	}

	slog.Info("found provider in registry", "provider_id", provider.ProviderID, "version", provider.Version, "capabilities", len(provider.Capabilities))

	// Check if already installed
	installed, err := m.lifecycle.ListInstalled(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list installed providers: %w", err)
	}

	var providerInstance *store.ProviderInstance
	for _, inst := range installed {
		if inst.ProviderID == provider.ProviderID && inst.Version == provider.Version {
			// Skip failed installations - will reinstall
			if inst.State == string(ProviderStateFailed) {
				slog.Info("previous install failed, will reinstall", "provider_id", provider.ProviderID)
				continue
			}
			providerInstance = inst
			break
		}
	}

	// Install if not already installed (or previous install failed)
	if providerInstance == nil {
		slog.Info("installing provider", "provider_id", provider.ProviderID, "version", provider.Version)
		providerInstance, err = m.lifecycle.Install(ctx, provider)
		if err != nil {
			return nil, fmt.Errorf("failed to install provider: %w", err)
		}
	}

	// Start provider if not running
	if providerInstance.State != string(ProviderStateRunning) {
		slog.Info("starting provider", "provider_id", provider.ProviderID, "version", provider.Version)
		if err := m.lifecycle.Start(ctx, provider.ProviderID, provider.Version); err != nil {
			return nil, fmt.Errorf("failed to start provider: %w", err)
		}
		providerInstance.State = string(ProviderStateRunning)
	}

	// Get first capability for response
	var capability *Capability
	if len(provider.Capabilities) > 0 {
		capability, _ = m.registry.GetCapability(provider.Capabilities[0].ID)
	}

	return &ProviderEndpoint{
		Provider:   provider,
		Capability: capability,
		Endpoint:   providerInstance.Endpoint,
		State:      ProviderState(providerInstance.State),
	}, nil
}

// UninstallProviderByID uninstalls providers by provider ID.
// If version is empty, all installed versions for that provider ID are removed.
func (m *Manager) UninstallProviderByID(ctx context.Context, providerID string, version string) (int, error) {
	slog.Info("uninstalling provider by ID", "provider_id", providerID, "version", version)

	installed, err := m.lifecycle.ListInstalled(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to list installed providers: %w", err)
	}

	var targets []*store.ProviderInstance
	for _, inst := range installed {
		if inst.ProviderID != providerID {
			continue
		}
		if version != "" && inst.Version != version {
			continue
		}
		targets = append(targets, inst)
	}

	if len(targets) == 0 {
		return 0, fmt.Errorf("provider not installed: %s", providerID)
	}

	for _, inst := range targets {
		if err := m.lifecycle.Uninstall(ctx, inst.ProviderID, inst.Version); err != nil {
			return 0, fmt.Errorf("failed to uninstall provider %s@%s: %w", inst.ProviderID, inst.Version, err)
		}
	}

	return len(targets), nil
}

// GetLifecycleManager returns the lifecycle manager (for kernel syscall wiring).
func (m *Manager) GetLifecycleManager() *LifecycleManager {
	return m.lifecycle
}
