package capability

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/capability/store"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// LifecycleManager manages the lifecycle of provider instances
type LifecycleManager struct {
	dockerClient    *client.Client
	binaryLifecycle *BinaryLifecycleManager
	supervisor      *ProcessSupervisor
	providerStore   *store.ProviderStore
	config          LifecycleConfig
	log             *slog.Logger
}

// LifecycleConfig configures the lifecycle manager
type LifecycleConfig struct {
	Network     string // Docker network for providers
	ProviderDir string // Directory for provider data
	MemoryLimit string // Default memory limit (e.g., "512m")
	CPULimit    string // Default CPU limit (e.g., "1.0")
	LogDir      string // Directory for binary provider logs
	StagingDir  string // Directory for binary downloads
}

// NewLifecycleManager creates a new lifecycle manager
func NewLifecycleManager(dockerClient *client.Client, providerStore *store.ProviderStore, config LifecycleConfig, log *slog.Logger) (*LifecycleManager, error) {
	if providerStore == nil {
		return nil, fmt.Errorf("provider store cannot be nil")
	}
	if log == nil {
		log = slog.Default()
	}

	// Create process supervisor for binary providers
	supervisor := NewProcessSupervisor(log, config.LogDir)

	// Create binary lifecycle manager
	binaryLifecycle := NewBinaryLifecycleManager(
		log,
		config.ProviderDir,
		config.StagingDir,
		config.LogDir,
		supervisor,
	)

	return &LifecycleManager{
		dockerClient:    dockerClient,
		binaryLifecycle: binaryLifecycle,
		supervisor:      supervisor,
		providerStore:   providerStore,
		config:          config,
		log:             log,
	}, nil
}

// Install installs a provider (pulls OCI image or downloads binary)
func (l *LifecycleManager) Install(ctx context.Context, provider *Provider) (*store.ProviderInstance, error) {
	l.log.Info("installing provider",
		"provider_id", provider.ProviderID,
		"version", provider.Version,
		"artifact_type", provider.Artifact.Type)

	// Route based on artifact type
	switch provider.Artifact.Type {
	case ArtifactOCI:
		return l.installOCI(ctx, provider)
	case ArtifactBinary:
		return l.installBinary(ctx, provider)
	default:
		return nil, fmt.Errorf("unsupported artifact type: %s", provider.Artifact.Type)
	}
}

// installOCI installs an OCI container provider
func (l *LifecycleManager) installOCI(ctx context.Context, provider *Provider) (*store.ProviderInstance, error) {
	if l.dockerClient == nil {
		return nil, fmt.Errorf("docker client not available for OCI providers")
	}

	l.log.Info("installing OCI provider", "image", provider.Artifact.URI)

	// Create provider instance record
	instance := &store.ProviderInstance{
		ProviderID:   provider.ProviderID,
		Version:      provider.Version,
		State:        string(ProviderStateInstalling),
		Capabilities: extractCapabilityIDs(provider.Capabilities),
		InstalledAt:  time.Now(),
		Metadata:     make(map[string]string),
	}

	// Save initial state
	if err := l.providerStore.SaveProvider(instance); err != nil {
		return nil, fmt.Errorf("failed to save provider state: %w", err)
	}

	// Pull image
	slog.Debug("pulling OCI image", "image", provider.Artifact.URI)
	pullResp, err := l.dockerClient.ImagePull(ctx, provider.Artifact.URI, client.ImagePullOptions{})
	if err != nil {
		instance.State = string(ProviderStateFailed)
		l.providerStore.SaveProvider(instance)
		return nil, fmt.Errorf("failed to pull image: %w", err)
	}
	defer pullResp.Close()

	// Wait for pull to complete (consume output)
	io.Copy(io.Discard, pullResp)

	// Verify digest if provided (skipping for MVP - would need proper API)
	if provider.Artifact.Digest != "" {
		slog.Debug("image digest verification", "expected", provider.Artifact.Digest)
		// TODO: Implement proper digest verification with correct Docker API
	}

	// Create container
	containerName := fmt.Sprintf("underleaf-provider-%s-%s", provider.ProviderID, provider.Version)

	resp, err := l.dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: containerName,
		Config: &container.Config{
			Image: provider.Artifact.URI,
			Labels: map[string]string{
				"underleaf.provider":   provider.ProviderID,
				"underleaf.version":    provider.Version,
				"underleaf.trust_tier": string(provider.TrustTier),
				"underleaf.managed_by": "capability-manager",
			},
		},
		HostConfig: &container.HostConfig{
			NetworkMode:    container.NetworkMode(l.config.Network),
			ReadonlyRootfs: true,
			Resources: container.Resources{
				Memory: parseMemoryLimit(l.config.MemoryLimit),
			},
			SecurityOpt: []string{"no-new-privileges"},
			CapDrop:     []string{"ALL"},
			Tmpfs: map[string]string{
				"/tmp": "rw,noexec,nosuid,size=65536k",
			},
		},
	})
	if err != nil {
		instance.State = string(ProviderStateFailed)
		l.providerStore.SaveProvider(instance)
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	// Update instance with container ID
	instance.RuntimeID = resp.ID
	instance.State = string(ProviderStateStopped)
	instance.Endpoint = fmt.Sprintf("unix:///var/run/underleaf/providers/%s.sock", provider.ProviderID)

	if err := l.providerStore.SaveProvider(instance); err != nil {
		return nil, fmt.Errorf("failed to update provider state: %w", err)
	}

	slog.Info("provider installed successfully",
		"provider_id", provider.ProviderID,
		"version", provider.Version,
		"container_id", resp.ID)

	return instance, nil
}

// installBinary installs a binary provider
func (l *LifecycleManager) installBinary(ctx context.Context, provider *Provider) (*store.ProviderInstance, error) {
	l.log.Info("installing binary provider", "provider_id", provider.ProviderID)

	// Create provider instance record
	instance := &store.ProviderInstance{
		ProviderID:   provider.ProviderID,
		Version:      provider.Version,
		State:        string(ProviderStateInstalling),
		Capabilities: extractCapabilityIDs(provider.Capabilities),
		InstalledAt:  time.Now(),
		Metadata:     make(map[string]string),
	}

	// Save initial state
	if err := l.providerStore.SaveProvider(instance); err != nil {
		return nil, fmt.Errorf("failed to save provider state: %w", err)
	}

	// Install using binary lifecycle manager
	installState, err := l.binaryLifecycle.Install(provider)
	if err != nil {
		instance.State = string(ProviderStateFailed)
		l.providerStore.SaveProvider(instance)
		return nil, fmt.Errorf("failed to install binary: %w", err)
	}

	// Update instance with binary info
	instance.RuntimeID = installState.BinaryPath // Store binary path in RuntimeID
	instance.State = string(ProviderStateInstalled)
	instance.Endpoint = "" // No endpoint until started
	instance.Metadata["binary_path"] = installState.BinaryPath
	instance.Metadata["digest"] = installState.Digest
	instance.Metadata["platform"] = installState.Platform

	if err := l.providerStore.SaveProvider(instance); err != nil {
		return nil, fmt.Errorf("failed to update provider state: %w", err)
	}

	l.log.Info("binary provider installed successfully",
		"provider_id", provider.ProviderID,
		"version", provider.Version,
		"binary_path", installState.BinaryPath)

	return instance, nil
}

// Start starts a provider (container or binary process)
func (l *LifecycleManager) Start(ctx context.Context, providerID, version string) error {
	instance, err := l.providerStore.GetProvider(providerID, version)
	if err != nil {
		return fmt.Errorf("provider not found: %w", err)
	}

	if instance.RuntimeID == "" {
		return fmt.Errorf("provider has no runtime ID")
	}

	l.log.Info("starting provider", "provider_id", providerID, "version", version, "state", instance.State)

	// Determine if this is a binary or OCI provider based on metadata or RuntimeID pattern
	isBinary := instance.Metadata != nil && instance.Metadata["binary_path"] != ""

	if isBinary {
		return l.startBinary(ctx, instance)
	} else {
		return l.startOCI(ctx, instance)
	}
}

// startOCI starts an OCI container provider
func (l *LifecycleManager) startOCI(ctx context.Context, instance *store.ProviderInstance) error {
	if l.dockerClient == nil {
		return fmt.Errorf("docker client not available")
	}

	// Start container
	_, err := l.dockerClient.ContainerStart(ctx, instance.RuntimeID, client.ContainerStartOptions{})
	if err != nil {
		instance.State = string(ProviderStateFailed)
		l.providerStore.SaveProvider(instance)
		return fmt.Errorf("failed to start container: %w", err)
	}

	// Update state
	instance.State = string(ProviderStateRunning)
	if err := l.providerStore.SaveProvider(instance); err != nil {
		return fmt.Errorf("failed to update provider state: %w", err)
	}

	l.log.Info("OCI provider started successfully", "provider_id", instance.ProviderID, "version", instance.Version)
	return nil
}

// startBinary starts a binary provider process
func (l *LifecycleManager) startBinary(ctx context.Context, instance *store.ProviderInstance) error {
	// Need to reconstruct provider info for Start() call
	// This is a simplification - in production you'd store more provider metadata
	provider := &Provider{
		ProviderID:          instance.ProviderID,
		Version:             instance.Version,
		RuntimeRequirements: RuntimeRequirements{},
	}

	installState := &InstallState{
		ProviderID: instance.ProviderID,
		Version:    instance.Version,
		BinaryPath: instance.Metadata["binary_path"],
	}

	if err := l.binaryLifecycle.Start(provider, installState); err != nil {
		instance.State = string(ProviderStateFailed)
		l.providerStore.SaveProvider(instance)
		return fmt.Errorf("failed to start binary: %w", err)
	}

	// Update state
	instance.State = string(ProviderStateRunning)
	if err := l.providerStore.SaveProvider(instance); err != nil {
		return fmt.Errorf("failed to update provider state: %w", err)
	}

	l.log.Info("binary provider started successfully", "provider_id", instance.ProviderID, "version", instance.Version)
	return nil
}

// Stop stops a provider (container or binary process)
func (l *LifecycleManager) Stop(ctx context.Context, providerID, version string) error {
	instance, err := l.providerStore.GetProvider(providerID, version)
	if err != nil {
		return fmt.Errorf("provider not found: %w", err)
	}

	if instance.RuntimeID == "" {
		return fmt.Errorf("provider has no runtime ID")
	}

	slog.Info("stopping provider", "provider_id", providerID, "version", version)

	// Stop container with timeout
	timeout := 10 // seconds
	_, err = l.dockerClient.ContainerStop(ctx, instance.RuntimeID, client.ContainerStopOptions{Timeout: &timeout})
	if err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}

	// Update state
	instance.State = string(ProviderStateStopped)
	if err := l.providerStore.SaveProvider(instance); err != nil {
		return fmt.Errorf("failed to update provider state: %w", err)
	}

	slog.Info("provider stopped successfully", "provider_id", providerID, "version", version)

	return nil
}

// Uninstall removes a provider (stops and removes container)
func (l *LifecycleManager) Uninstall(ctx context.Context, providerID, version string) error {
	instance, err := l.providerStore.GetProvider(providerID, version)
	if err != nil {
		return fmt.Errorf("provider not found: %w", err)
	}

	if instance.RuntimeID != "" {
		slog.Info("uninstalling provider", "provider_id", providerID, "version", version)

		// Stop container if running
		l.Stop(ctx, providerID, version) // Ignore error

		// Remove container
		_, err := l.dockerClient.ContainerRemove(ctx, instance.RuntimeID, client.ContainerRemoveOptions{Force: true})
		if err != nil {
			slog.Warn("failed to remove container", "container_id", instance.RuntimeID, "error", err)
		}
	}

	// Remove from store
	if err := l.providerStore.DeleteProvider(providerID, version); err != nil {
		return fmt.Errorf("failed to delete provider from store: %w", err)
	}

	slog.Info("provider uninstalled successfully", "provider_id", providerID, "version", version)

	return nil
}

// GetStatus returns the current status of a provider
func (l *LifecycleManager) GetStatus(ctx context.Context, providerID, version string) (ProviderState, error) {
	instance, err := l.providerStore.GetProvider(providerID, version)
	if err != nil {
		return ProviderStateUnknown, fmt.Errorf("provider not found: %w", err)
	}

	if instance.RuntimeID == "" {
		return ProviderState(instance.State), nil
	}

	// For MVP, return the stored state
	// TODO: Query Docker for actual container state with proper API
	state := ProviderState(instance.State)

	// Update stored state if different
	if instance.State != string(state) {
		instance.State = string(state)
		l.providerStore.SaveProvider(instance)
	}

	return state, nil
}

// ListInstalled returns all installed providers
func (l *LifecycleManager) ListInstalled(ctx context.Context) ([]*store.ProviderInstance, error) {
	return l.providerStore.ListProviders()
}

// extractCapabilityIDs extracts capability IDs from capability references
func extractCapabilityIDs(refs []CapabilityRef) []string {
	ids := make([]string, len(refs))
	for i, ref := range refs {
		ids[i] = ref.ID
	}
	return ids
}

// parseMemoryLimit parses memory limit string (e.g., "512m") to bytes
func parseMemoryLimit(limit string) int64 {
	if limit == "" {
		return 512 * 1024 * 1024 // Default 512MB
	}

	// Simple parsing for MVP (supports only "m" suffix)
	limit = strings.ToLower(strings.TrimSpace(limit))
	if strings.HasSuffix(limit, "m") {
		limitStr := strings.TrimSuffix(limit, "m")
		var mb int64
		fmt.Sscanf(limitStr, "%d", &mb)
		return mb * 1024 * 1024
	}

	return 512 * 1024 * 1024 // Default
}
