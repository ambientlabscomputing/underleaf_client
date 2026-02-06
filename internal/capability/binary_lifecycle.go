package capability

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// BinaryLifecycleManager manages the lifecycle of binary providers.
type BinaryLifecycleManager struct {
	downloader      *BinaryDownloader
	supervisor      *ProcessSupervisor
	providerBaseDir string
	log             *slog.Logger
}

// NewBinaryLifecycleManager creates a new binary lifecycle manager.
func NewBinaryLifecycleManager(
	log *slog.Logger,
	providerBaseDir string,
	stagingDir string,
	logDir string,
	supervisor *ProcessSupervisor,
) *BinaryLifecycleManager {
	return &BinaryLifecycleManager{
		downloader:      NewBinaryDownloader(stagingDir),
		supervisor:      supervisor,
		providerBaseDir: providerBaseDir,
		log:             log,
	}
}

// Install downloads and installs a binary provider.
func (m *BinaryLifecycleManager) Install(provider *Provider) (*InstallState, error) {
	m.log.Info("Installing binary provider",
		"provider_id", provider.ProviderID,
		"version", provider.Version,
		"artifact_uri", provider.Artifact.URI)

	// Create provider directory
	providerDir := filepath.Join(m.providerBaseDir, provider.ProviderID, provider.Version)
	if err := os.MkdirAll(providerDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create provider directory: %w", err)
	}

	// Expand URI for current platform
	artifactURI := m.expandArtifactURI(provider)

	// Download binary
	var result *DownloadResult
	var err error

	// If there's a checksums URI, use it for verification
	if provider.Artifact.Checksums != nil && provider.Artifact.Checksums.URI != "" {
		checksumsURI := m.expandChecksumsURI(provider)
		result, err = m.downloader.DownloadWithChecksumsFile(artifactURI, provider.Version, checksumsURI)
	} else if provider.Artifact.Digest != "" {
		// Use provided digest
		result, err = m.downloader.Download(artifactURI, provider.Version, provider.Artifact.Digest)
	} else {
		// No verification
		m.log.Warn("Installing binary without checksum verification", "provider_id", provider.ProviderID)
		result, err = m.downloader.Download(artifactURI, provider.Version, "")
	}

	if err != nil {
		return nil, fmt.Errorf("failed to download binary: %w", err)
	}

	// Move binary to final location
	binaryName := m.getBinaryName(provider)
	finalPath := filepath.Join(providerDir, binaryName)
	if err := os.Rename(result.Path, finalPath); err != nil {
		return nil, fmt.Errorf("failed to move binary to final location: %w", err)
	}

	m.log.Info("Binary provider installed",
		"provider_id", provider.ProviderID,
		"version", provider.Version,
		"path", finalPath,
		"size", result.Size,
		"sha256", result.SHA256)

	// Create state
	state := &InstallState{
		ProviderID:  provider.ProviderID,
		Version:     provider.Version,
		Status:      ProviderStateInstalled,
		BinaryPath:  finalPath,
		Digest:      result.SHA256,
		Platform:    result.Platform,
		InstalledAt: result.Size, // Reusing this field for size
	}

	return state, nil
}

// Start starts a binary provider process.
func (m *BinaryLifecycleManager) Start(provider *Provider, state *InstallState) error {
	m.log.Info("Starting binary provider",
		"provider_id", provider.ProviderID,
		"version", provider.Version,
		"binary_path", state.BinaryPath)

	// Determine health endpoint
	healthEndpoint := fmt.Sprintf("http://localhost:%d/health", m.getDefaultPort(provider))
	if provider.RuntimeRequirements.HealthEndpoint != "" {
		healthEndpoint = provider.RuntimeRequirements.HealthEndpoint
	}

	// Determine args
	args := []string{}
	if len(provider.RuntimeRequirements.Args) > 0 {
		args = provider.RuntimeRequirements.Args
	}

	// Start supervised process
	policy := DefaultRestartPolicy()
	if err := m.supervisor.Start(
		provider.ProviderID,
		provider.Version,
		state.BinaryPath,
		args,
		healthEndpoint,
		policy,
	); err != nil {
		return fmt.Errorf("failed to start provider process: %w", err)
	}

	state.Status = ProviderStateRunning
	return nil
}

// Stop stops a binary provider process.
func (m *BinaryLifecycleManager) Stop(provider *Provider, state *InstallState) error {
	m.log.Info("Stopping binary provider",
		"provider_id", provider.ProviderID,
		"version", provider.Version)

	if err := m.supervisor.Stop(provider.ProviderID, provider.Version); err != nil {
		return fmt.Errorf("failed to stop provider process: %w", err)
	}

	state.Status = ProviderStateStopped
	return nil
}

// Uninstall stops and removes a binary provider.
func (m *BinaryLifecycleManager) Uninstall(provider *Provider, state *InstallState) error {
	m.log.Info("Uninstalling binary provider",
		"provider_id", provider.ProviderID,
		"version", provider.Version)

	// Stop if running
	if state.Status == ProviderStateRunning {
		if err := m.Stop(provider, state); err != nil {
			m.log.Warn("Failed to stop provider during uninstall", "error", err)
		}
	}

	// Remove provider directory
	providerDir := filepath.Join(m.providerBaseDir, provider.ProviderID, provider.Version)
	if err := os.RemoveAll(providerDir); err != nil {
		return fmt.Errorf("failed to remove provider directory: %w", err)
	}

	state.Status = ProviderStateUninstalled
	return nil
}

// GetStatus returns the status of a binary provider.
func (m *BinaryLifecycleManager) GetStatus(provider *Provider, state *InstallState) (*InstallState, error) {
	// Get process info from supervisor
	procInfo, err := m.supervisor.GetStatus(provider.ProviderID, provider.Version)
	if err != nil {
		// Not found in supervisor
		return state, nil
	}

	// Update state from process info
	switch procInfo.State {
	case ProcessStateRunning:
		state.Status = ProviderStateRunning
	case ProcessStateStopped:
		state.Status = ProviderStateStopped
	case ProcessStateCrashed, ProcessStateFailed:
		state.Status = ProviderStateFailed
	}

	state.PID = procInfo.PID
	state.Healthy = procInfo.Healthy

	return state, nil
}

// expandArtifactURI expands the artifact URI template for the current platform.
func (m *BinaryLifecycleManager) expandArtifactURI(provider *Provider) string {
	uri := provider.Artifact.URI

	// If it's a template, expand it
	if strings.Contains(uri, "{os}") || strings.Contains(uri, "{arch}") {
		uri = strings.ReplaceAll(uri, "{os}", runtime.GOOS)
		uri = strings.ReplaceAll(uri, "{arch}", runtime.GOARCH)
	}

	// Handle windows .exe extension
	if runtime.GOOS == "windows" && !strings.HasSuffix(uri, ".exe") {
		uri += ".exe"
	}

	return uri
}

// expandChecksumsURI expands the checksums URI.
func (m *BinaryLifecycleManager) expandChecksumsURI(provider *Provider) string {
	if provider.Artifact.Checksums == nil {
		return ""
	}
	// Checksums URI typically doesn't need platform expansion, but just in case
	return strings.ReplaceAll(provider.Artifact.Checksums.URI, "{version}", provider.Version)
}

// getBinaryName returns the binary filename for the provider.
func (m *BinaryLifecycleManager) getBinaryName(provider *Provider) string {
	name := provider.ProviderID
	// Remove any path separators
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, "\\", "-")

	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// getDefaultPort returns a default port for the provider based on its ID.
// This is a simple hash-based approach to avoid port conflicts.
func (m *BinaryLifecycleManager) getDefaultPort(provider *Provider) int {
	// MMA uses port 8080, other providers get ports in the 8100-8999 range
	if provider.ProviderID == "ambient.mycelium-mesh-agent" {
		return 8080
	}

	// Simple hash: sum of ASCII values modulo 900, then add 8100
	sum := 0
	for _, c := range provider.ProviderID {
		sum += int(c)
	}
	return 8100 + (sum % 900)
}
