package capability

import (
	"time"

	ucrstypes "github.com/ambientlabscomputing/ucrs/types"
)

// Re-export UCRS types for convenience
type (
	Capability          = ucrstypes.Capability
	Provider            = ucrstypes.Provider
	RegistrySnapshot    = ucrstypes.RegistrySnapshot
	TrustTier           = ucrstypes.TrustTier
	RiskClass           = ucrstypes.RiskClass
	ArtifactType        = ucrstypes.ArtifactType
	Artifact            = ucrstypes.Artifact
	CapabilityRef       = ucrstypes.CapabilityRef
	RuntimeRequirements = ucrstypes.RuntimeRequirements
	SignedManifest      = ucrstypes.SignedManifest
)

// Trust tier constants
const (
	TrustOfficial     = ucrstypes.TrustOfficial
	TrustCertified    = ucrstypes.TrustCertified
	TrustCommunity    = ucrstypes.TrustCommunity
	TrustExperimental = ucrstypes.TrustExperimental
	TrustLocal        = ucrstypes.TrustLocal
)

// Risk class constants
const (
	RiskLow    = ucrstypes.RiskLow
	RiskMedium = ucrstypes.RiskMedium
	RiskHigh   = ucrstypes.RiskHigh
)

// Artifact type constants
const (
	ArtifactOCI    = ucrstypes.ArtifactOCI
	ArtifactBinary = ucrstypes.ArtifactBinary
	ArtifactGit    = ucrstypes.ArtifactGit
	ArtifactNPM    = ucrstypes.ArtifactNPM
	ArtifactPyPI   = ucrstypes.ArtifactPyPI
)

// CapabilityRequest represents a request to resolve and ensure a capability
type CapabilityRequest struct {
	CapabilityID string             `json:"capability_id"`
	VersionRange string             `json:"version_range,omitempty"` // e.g., "^1.0", ">=2.0 <3.0", empty means latest
	Constraints  ResolveConstraints `json:"constraints,omitempty"`
}

// ResolveConstraints specifies filtering constraints for provider selection
type ResolveConstraints struct {
	TrustTier    string `json:"trust_tier,omitempty"`   // "official_only", "certified+", "all"
	RiskClass    string `json:"risk_class,omitempty"`   // "low_only", "medium+", "all"
	Platform     string `json:"platform,omitempty"`     // "darwin", "linux", "windows"
	Architecture string `json:"architecture,omitempty"` // "amd64", "arm64"
}

// ProviderState represents the runtime state of a provider
type ProviderState string

const (
	ProviderStateInstalling  ProviderState = "installing"
	ProviderStateInstalled   ProviderState = "installed"
	ProviderStateRunning     ProviderState = "running"
	ProviderStateStopped     ProviderState = "stopped"
	ProviderStateFailed      ProviderState = "failed"
	ProviderStateUnknown     ProviderState = "unknown"
	ProviderStateUninstalled ProviderState = "uninstalled"
)

// InstallState tracks the installation and runtime state of a provider
type InstallState struct {
	ProviderID  string        `json:"provider_id"`
	Version     string        `json:"version"`
	Status      ProviderState `json:"status"`
	BinaryPath  string        `json:"binary_path,omitempty"`  // For binary providers
	ContainerID string        `json:"container_id,omitempty"` // For OCI providers
	Digest      string        `json:"digest,omitempty"`       // SHA256 or OCI digest
	Platform    string        `json:"platform,omitempty"`     // e.g., "darwin/arm64"
	PID         int           `json:"pid,omitempty"`          // For binary providers
	Healthy     bool          `json:"healthy"`
	InstalledAt int64         `json:"installed_at,omitempty"` // Unix timestamp or size (overloaded)
	Error       string        `json:"error,omitempty"`
}

// ProviderInstance represents an installed provider instance
type ProviderInstance struct {
	ProviderID   string            `json:"provider_id"`
	Version      string            `json:"version"`
	State        ProviderState     `json:"state"`
	Capabilities []string          `json:"capabilities"` // Capability IDs this provider implements
	Endpoint     string            `json:"endpoint"`     // MCP endpoint (e.g., "unix:///var/run/underleaf/providers/xyz.sock")
	InstalledAt  time.Time         `json:"installed_at"`
	RuntimeID    string            `json:"runtime_id,omitempty"` // Docker container ID or process PID
	Metadata     map[string]string `json:"metadata,omitempty"`   // Additional metadata
}

// ProviderEndpoint represents a resolved provider endpoint
type ProviderEndpoint struct {
	Provider   *Provider     `json:"provider"`
	Capability *Capability   `json:"capability"`
	Endpoint   string        `json:"endpoint"`
	State      ProviderState `json:"state"`
}

// EnsureCapabilityRequest is the API request for ensuring a capability is available
type EnsureCapabilityRequest struct {
	CapabilityID string             `json:"capability_id"`
	VersionRange string             `json:"version_range,omitempty"`
	Constraints  ResolveConstraints `json:"constraints,omitempty"`
}

// EnsureCapabilityResponse is the API response for capability ensure
type EnsureCapabilityResponse struct {
	Provider   ProviderInfo   `json:"provider"`
	Capability CapabilityInfo `json:"capability"`
}

// ProviderInfo is a simplified provider info for API responses
type ProviderInfo struct {
	ProviderID string        `json:"provider_id"`
	Version    string        `json:"version"`
	Endpoint   string        `json:"endpoint"`
	State      ProviderState `json:"state"`
}

// CapabilityInfo is a simplified capability info for API responses
type CapabilityInfo struct {
	ID          string    `json:"id"`
	Version     string    `json:"version"`
	Description string    `json:"description"`
	RiskClass   RiskClass `json:"risk_class"`
}

// ListCapabilitiesResponse is the API response for listing capabilities
type ListCapabilitiesResponse struct {
	Capabilities []CapabilityInfo `json:"capabilities"`
}

// ListProvidersResponse is the API response for listing installed providers
type ListProvidersResponse struct {
	Providers []ProviderInstance `json:"providers"`
}

// Config contains configuration for the capability manager
type Config struct {
	UCRSBaseURL   string        // Base URL for UCRS API
	PublicKeyPath string        // Path to UCRS public key for signature verification
	CacheDir      string        // Directory for caching registry snapshots
	ProviderDir   string        // Directory for provider installations
	SyncInterval  time.Duration // Interval for periodic registry sync
	Network       string        // Docker network name for providers
	MemoryLimit   string        // Default memory limit for providers (e.g., "512m")
	CPULimit      string        // Default CPU limit for providers (e.g., "1.0")
	TrustTier     string        // Default trust tier constraint (e.g., "certified+")
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() Config {
	return Config{
		UCRSBaseURL:  "https://registry.underleaf.io",
		SyncInterval: 10 * time.Minute,
		Network:      "underleaf-providers",
		MemoryLimit:  "512m",
		CPULimit:     "1.0",
		TrustTier:    "certified+",
	}
}
