package updater

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/pkg/version"
)

// UpdateManager manages the auto-update lifecycle
type UpdateManager struct {
	store      *Store
	checker    *GitHubReleaseChecker
	downloader *Downloader
	installer  *Installer

	// State
	currentState *UpdateState
	stateMu      sync.RWMutex

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Configuration
	checkInterval time.Duration
	repository    string
}

// UpdateManagerConfig configures the update manager
type UpdateManagerConfig struct {
	Store         *Store
	Installer     *Installer
	CheckInterval time.Duration
	Repository    string
}

// NewUpdateManager creates a new update manager
func NewUpdateManager(config UpdateManagerConfig) *UpdateManager {
	if config.CheckInterval == 0 {
		config.CheckInterval = 1 * time.Hour
	}
	if config.Repository == "" {
		config.Repository = "ambientlabscomputing/underleaf_client"
	}

	checker := NewGitHubReleaseChecker(config.Repository)
	downloader := NewDownloader(config.Store.GetStagingPath())

	return &UpdateManager{
		store:         config.Store,
		checker:       checker,
		downloader:    downloader,
		installer:     config.Installer,
		checkInterval: config.CheckInterval,
		repository:    config.Repository,
	}
}

// Start begins the update manager lifecycle
func (m *UpdateManager) Start(ctx context.Context) error {
	m.ctx, m.cancel = context.WithCancel(ctx)

	// Ensure directories exist
	if err := m.store.EnsureDirectories(); err != nil {
		return fmt.Errorf("failed to ensure directories: %w", err)
	}

	// Load existing state
	state, err := m.store.LoadState()
	if err != nil {
		slog.Warn("failed to load update state, using default", "error", err)
		state = &UpdateState{
			Status:         UpdateStatusIdle,
			CurrentVersion: version.Version,
		}
	}

	m.stateMu.Lock()
	m.currentState = state
	m.stateMu.Unlock()

	// Bootstrap: if ReleaseSHA is empty, fetch it for the current version
	if m.currentState.ReleaseSHA == "" && version.Version != "" && version.Version != "0.0.0" {
		slog.Info("bootstrapping update state, fetching SHA for current version", "version", version.Version)
		if release, err := m.checker.FetchRelease(ctx, version.Version); err == nil && release.SHA256 != "" {
			m.stateMu.Lock()
			m.currentState.ReleaseSHA = release.SHA256
			m.currentState.CurrentVersion = version.Version
			m.stateMu.Unlock()
			if err := m.store.SaveState(m.currentState); err != nil {
				slog.Warn("failed to save bootstrapped state", "error", err)
			} else {
				slog.Info("bootstrapped update state", "version", version.Version, "sha256", release.SHA256)
			}
		} else {
			slog.Warn("failed to bootstrap update state", "error", err)
		}
	}

	// Start periodic SHA check (for detecting new builds of same version)
	m.wg.Add(1)
	go m.periodicSHACheck()

	slog.Info("update manager started",
		"current_version", version.Version,
		"check_interval", m.checkInterval)

	return nil
}

// Stop gracefully stops the update manager
func (m *UpdateManager) Stop(ctx context.Context) error {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()

	slog.Info("update manager stopped")
	return nil
}

// WatchConfigVersion watches for desired version changes from config manager
func (m *UpdateManager) WatchConfigVersion(configChan <-chan string) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()

		for {
			select {
			case desiredVersion := <-configChan:
				if desiredVersion == "" {
					continue
				}

				// Check if version changed
				m.stateMu.RLock()
				currentDesired := m.currentState.DesiredVersion
				m.stateMu.RUnlock()

				if desiredVersion != currentDesired {
					slog.Info("desired version changed",
						"from", currentDesired,
						"to", desiredVersion)

					// Trigger update check
					if err := m.CheckAndDownload(desiredVersion); err != nil {
						slog.Error("failed to check for updates", "error", err)
					}
				}

			case <-m.ctx.Done():
				return
			}
		}
	}()
}

// periodicSHACheck periodically checks if the release SHA has changed (new build)
func (m *UpdateManager) periodicSHACheck() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.stateMu.RLock()
			desiredVersion := m.currentState.DesiredVersion
			currentSHA := m.currentState.ReleaseSHA
			m.stateMu.RUnlock()

			if desiredVersion == "" {
				continue
			}

			// Fetch release info
			release, err := m.checker.FetchRelease(m.ctx, desiredVersion)
			if err != nil {
				slog.Warn("failed to fetch release for SHA check", "error", err)
				continue
			}

			// Check if SHA changed
			if release.SHA256 != "" && release.SHA256 != currentSHA {
				slog.Info("release SHA changed, triggering update",
					"version", desiredVersion,
					"old_sha", currentSHA,
					"new_sha", release.SHA256)

				if err := m.CheckAndDownload(desiredVersion); err != nil {
					slog.Error("failed to download updated release", "error", err)
				}
			}

		case <-m.ctx.Done():
			return
		}
	}
}

// CheckAndDownload checks for updates and downloads if available
func (m *UpdateManager) CheckAndDownload(desiredVersion string) error {
	m.stateMu.Lock()
	m.currentState.Status = UpdateStatusChecking
	m.currentState.DesiredVersion = desiredVersion
	m.currentState.LastCheck = time.Now()
	m.currentState.UpdatedAt = time.Now()
	m.stateMu.Unlock()

	if err := m.store.SaveState(m.currentState); err != nil {
		slog.Warn("failed to save state", "error", err)
	}

	// Fetch release info
	release, err := m.checker.FetchRelease(m.ctx, desiredVersion)
	if err != nil {
		m.updateStatus(UpdateStatusFailed, fmt.Sprintf("failed to fetch release: %v", err))
		return fmt.Errorf("failed to fetch release: %w", err)
	}

	slog.Info("found release",
		"version", release.Version,
		"sha256", release.SHA256,
		"is_prerelease", release.IsPrerelease)

	// Check if SHA changed (indicates new build)
	if release.SHA256 == m.currentState.ReleaseSHA && version.Version == release.Version {
		slog.Info("release unchanged, skipping download")
		m.updateStatus(UpdateStatusIdle, "")
		return nil
	}

	// Download binaries
	if err := m.downloadBinaries(release); err != nil {
		m.updateStatus(UpdateStatusFailed, fmt.Sprintf("download failed: %v", err))
		return fmt.Errorf("failed to download binaries: %w", err)
	}

	// Update state - mark as pending, but don't update ReleaseSHA until installation succeeds
	m.stateMu.Lock()
	m.currentState.Status = UpdateStatusPending
	m.currentState.PendingSHA = release.SHA256
	m.currentState.ErrorMessage = ""
	m.currentState.UpdatedAt = time.Now()
	m.stateMu.Unlock()

	if err := m.store.SaveState(m.currentState); err != nil {
		slog.Warn("failed to save state", "error", err)
	}

	slog.Info("update downloaded and ready to apply", "version", release.Version)
	return nil
}

// downloadBinaries downloads agent and ufctl binaries
func (m *UpdateManager) downloadBinaries(release *ReleaseInfo) error {
	m.updateStatus(UpdateStatusDownloading, "")

	// Download agent
	agentAsset, err := release.GetAssetForPlatform("underleaf_agent")
	if err != nil {
		return fmt.Errorf("failed to find agent asset: %w", err)
	}

	slog.Info("downloading agent", "asset", agentAsset.Name, "size", agentAsset.Size)
	agentPath, err := m.downloader.Download(agentAsset, agentAsset.SHA256)
	if err != nil {
		return fmt.Errorf("failed to download agent: %w", err)
	}

	m.stateMu.Lock()
	m.currentState.PendingAgent = agentPath
	m.stateMu.Unlock()

	// Download ufctl
	ufctlAsset, err := release.GetAssetForPlatform("ufctl")
	if err != nil {
		return fmt.Errorf("failed to find ufctl asset: %w", err)
	}

	slog.Info("downloading ufctl", "asset", ufctlAsset.Name, "size", ufctlAsset.Size)
	ufctlPath, err := m.downloader.Download(ufctlAsset, ufctlAsset.SHA256)
	if err != nil {
		return fmt.Errorf("failed to download ufctl: %w", err)
	}

	m.stateMu.Lock()
	m.currentState.PendingUfctl = ufctlPath
	m.stateMu.Unlock()

	return nil
}

// ApplyUpdate applies the pending update
func (m *UpdateManager) ApplyUpdate() error {
	m.stateMu.RLock()
	state := *m.currentState
	m.stateMu.RUnlock()

	if state.Status != UpdateStatusPending {
		return fmt.Errorf("no pending update")
	}

	m.updateStatus(UpdateStatusApplying, "")

	// Apply the update
	if err := m.installer.ApplyUpdate(&state); err != nil {
		m.updateStatus(UpdateStatusFailed, fmt.Sprintf("apply failed: %v", err))
		return fmt.Errorf("failed to apply update: %w", err)
	}

	// Update succeeded - now update the ReleaseSHA to reflect the installed version
	m.stateMu.Lock()
	m.currentState.Status = UpdateStatusIdle
	m.currentState.CurrentVersion = m.currentState.DesiredVersion
	m.currentState.ReleaseSHA = m.currentState.PendingSHA // Promote pending SHA to current
	m.currentState.PendingAgent = ""
	m.currentState.PendingUfctl = ""
	m.currentState.PendingSHA = ""
	m.currentState.ErrorMessage = ""
	m.currentState.UpdatedAt = time.Now()
	m.stateMu.Unlock()

	if err := m.store.SaveState(m.currentState); err != nil {
		slog.Warn("failed to save state after update", "error", err)
	}

	// Clean staging
	if err := m.store.CleanStaging(); err != nil {
		slog.Warn("failed to clean staging directory", "error", err)
	}

	slog.Info("update applied successfully", "version", m.currentState.CurrentVersion)
	return nil
}

// Rollback rolls back to the previous version
func (m *UpdateManager) Rollback() error {
	m.stateMu.RLock()
	state := *m.currentState
	m.stateMu.RUnlock()

	slog.Info("rolling back update")

	if err := m.installer.Rollback(&state); err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}

	// Update state
	m.stateMu.Lock()
	m.currentState.Status = UpdateStatusIdle
	m.currentState.BackupAgent = ""
	m.currentState.BackupUfctl = ""
	m.currentState.PendingAgent = ""
	m.currentState.PendingUfctl = ""
	m.currentState.UpdatedAt = time.Now()
	m.stateMu.Unlock()

	if err := m.store.SaveState(m.currentState); err != nil {
		slog.Warn("failed to save state after rollback", "error", err)
	}

	slog.Info("rollback completed")
	return nil
}

// GetState returns the current update state
func (m *UpdateManager) GetState() *UpdateState {
	m.stateMu.RLock()
	defer m.stateMu.RUnlock()

	state := *m.currentState
	return &state
}

// updateStatus updates the status and error message
func (m *UpdateManager) updateStatus(status UpdateStatus, errorMsg string) {
	m.stateMu.Lock()
	m.currentState.Status = status
	m.currentState.ErrorMessage = errorMsg
	m.currentState.UpdatedAt = time.Now()
	m.stateMu.Unlock()

	if err := m.store.SaveState(m.currentState); err != nil {
		slog.Warn("failed to save state", "error", err)
	}
}
