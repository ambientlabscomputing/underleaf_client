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

	// Check platform support before attempting download
	if len(provider.Artifact.SupportedPlatforms) > 0 {
		currentOS := runtime.GOOS
		currentArch := runtime.GOARCH
		platformSupported := false

		for _, platform := range provider.Artifact.SupportedPlatforms {
			if platform.OS == currentOS && platform.Arch == currentArch {
				platformSupported = true
				break
			}
		}

		if !platformSupported {
			supportedList := make([]string, len(provider.Artifact.SupportedPlatforms))
			for i, platform := range provider.Artifact.SupportedPlatforms {
				supportedList[i] = fmt.Sprintf("%s/%s", platform.OS, platform.Arch)
			}
			return nil, fmt.Errorf(
				"provider %s does not support platform %s/%s; supported platforms: %v",
				provider.ProviderID,
				currentOS,
				currentArch,
				supportedList,
			)
		}
	}

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

	// Determine health endpoint from UCRS provider record
	// Priority: explicit health_endpoint > port-based construction > none
	healthEndpoint := ""
	if provider.RuntimeRequirements.HealthEndpoint != "" {
		healthEndpoint = provider.RuntimeRequirements.HealthEndpoint
	} else if provider.RuntimeRequirements.Port != 0 {
		healthEndpoint = fmt.Sprintf("http://localhost:%d/health", provider.RuntimeRequirements.Port)
	}

	// Determine args
	args := []string{}
	if len(provider.RuntimeRequirements.Args) > 0 {
		args = provider.RuntimeRequirements.Args
	}

	// Get environment variables from provider requirements
	env := provider.RuntimeRequirements.Env
	if env == nil {
		env = make(map[string]string)
	}

	// Start supervised process
	policy := DefaultRestartPolicy()
	if err := m.supervisor.Start(
		provider.ProviderID,
		provider.Version,
		state.BinaryPath,
		args,
		env,
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

	// Expand version template
	uri = strings.ReplaceAll(uri, "{version}", provider.Version)

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
