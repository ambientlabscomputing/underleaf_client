package updater

import (
	"time"
)

// UpdateStatus represents the current state of an update
type UpdateStatus string

const (
	UpdateStatusIdle        UpdateStatus = "idle"        // No update in progress
	UpdateStatusChecking    UpdateStatus = "checking"    // Checking for updates
	UpdateStatusDownloading UpdateStatus = "downloading" // Downloading update
	UpdateStatusPending     UpdateStatus = "pending"     // Update downloaded, ready to apply
	UpdateStatusApplying    UpdateStatus = "applying"    // Applying update
	UpdateStatusFailed      UpdateStatus = "failed"      // Update failed
)

// UpdateState tracks the current update state
type UpdateState struct {
	CurrentVersion string       `json:"current_version"` // Running version
	DesiredVersion string       `json:"desired_version"` // Version from server config
	ReleaseSHA     string       `json:"release_sha"`     // SHA256 of currently installed release
	Status         UpdateStatus `json:"status"`          // Current status
	LastCheck      time.Time    `json:"last_check"`      // Last update check time
	PendingAgent   string       `json:"pending_agent"`   // Path to downloaded agent binary
	PendingUfctl   string       `json:"pending_ufctl"`   // Path to downloaded ufctl binary
	PendingSHA     string       `json:"pending_sha"`     // SHA256 of pending release
	BackupAgent    string       `json:"backup_agent"`    // Path to agent backup
	BackupUfctl    string       `json:"backup_ufctl"`    // Path to ufctl backup
	ErrorMessage   string       `json:"error_message"`   // Last error if any
	UpdatedAt      time.Time    `json:"updated_at"`      // Last state update
}

// ReleaseInfo contains metadata about a GitHub release
type ReleaseInfo struct {
	Version      string         `json:"version"`       // e.g., "v1.2.3" or "dev"
	SHA256       string         `json:"sha256"`        // SHA256 hash of the release
	IsPrerelease bool           `json:"is_prerelease"` // Is this a prerelease
	PublishedAt  time.Time      `json:"published_at"`  // Release date
	Assets       []ReleaseAsset `json:"assets"`        // Binary assets
	Body         string         `json:"body"`          // Release notes
}

// ReleaseAsset represents a downloadable binary
type ReleaseAsset struct {
	Name        string `json:"name"`         // e.g., "underleaf_agent-darwin-arm64"
	DownloadURL string `json:"download_url"` // Direct download URL
	Size        int64  `json:"size"`         // File size in bytes
	SHA256      string `json:"sha256"`       // SHA256 checksum
	Platform    string `json:"platform"`     // darwin, linux, windows
	Arch        string `json:"arch"`         // amd64, arm64
	Binary      string `json:"binary"`       // agent or ufctl
}

// Updater defines the interface for update operations
type Updater interface {
	// Check checks if an update is available
	Check(desiredVersion string) (*ReleaseInfo, bool, error)

	// Download downloads the update binaries
	Download(release *ReleaseInfo) error

	// Apply applies the downloaded update
	Apply() error

	// Rollback reverts to the previous version
	Rollback() error

	// GetState returns the current update state
	GetState() (*UpdateState, error)
}

// ReleaseChecker fetches release information
type ReleaseChecker interface {
	// FetchRelease fetches a specific release by version
	FetchRelease(version string) (*ReleaseInfo, error)

	// FetchLatestStable fetches the latest stable release
	FetchLatestStable() (*ReleaseInfo, error)

	// FetchLatestPrerelease fetches the latest prerelease
	FetchLatestPrerelease() (*ReleaseInfo, error)
}

// UpdateConfig holds configuration for the updater
type UpdateConfig struct {
	// GitHub repository (owner/repo)
	Repository string

	// Check interval for periodic updates
	CheckInterval time.Duration

	// Download timeout
	DownloadTimeout time.Duration

	// Storage paths
	StoragePath string // Base path for update storage
	StagingPath string // Staging directory for downloads
	BackupPath  string // Backup directory
}

// DefaultUpdateConfig returns default configuration
func DefaultUpdateConfig() UpdateConfig {
	return UpdateConfig{
		Repository:      "ambientlabscomputing/underleaf_client",
		CheckInterval:   1 * time.Hour,
		DownloadTimeout: 5 * time.Minute,
	}
}
