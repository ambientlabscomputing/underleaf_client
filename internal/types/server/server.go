package servertypes

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
)

// NullableTime is a time.Time that can be null or empty string in JSON
type NullableTime struct {
	Time  time.Time
	Valid bool
}

func (nt *NullableTime) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" || string(data) == `""` || string(data) == `"` {
		nt.Valid = false
		return nil
	}

	var t time.Time
	if err := json.Unmarshal(data, &t); err != nil {
		nt.Valid = false
		return nil // Silently ignore parse errors for empty/invalid times
	}

	nt.Time = t
	nt.Valid = true
	return nil
}

func (nt NullableTime) MarshalJSON() ([]byte, error) {
	if !nt.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(nt.Time)
}

func (nt *NullableTime) Ptr() *time.Time {
	if !nt.Valid {
		return nil
	}
	return &nt.Time
}

const (
	ServerOSDarwin  = "darwin"
	ServerOSLinux   = "linux"
	ServerOSWindows = "windows"
	ServerOSUnknown = "unknown"

	ServerArchAMD64   = "amd64"
	ServerArchARM64   = "arm64"
	ServerArchUnknown = "unknown"
)

type ServerCommandSettings struct {
	AllowLiteralCommands bool `json:"allow_literal_commands"`
}

type ServerPlatform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

type ConfigurationPayload struct {
	Commands ServerCommandSettings `json:"commands"`
	Platform ServerPlatform        `json:"platform"`
}

type Configuration struct {
	Version int
	Payload ConfigurationPayload
}

func (c Configuration) FromPolicyManagerConfig(cfg policy_manager.Configuration) Configuration {
	pBytes, err := json.Marshal(cfg.Payload)
	if err != nil {
		slog.Error("failed to marshal configuration payload", "error", err)
		return Configuration{}
	}
	var payload ConfigurationPayload
	if err := json.Unmarshal(pBytes, &payload); err != nil {
		slog.Error("failed to unmarshal configuration payload", "error", err)
		return Configuration{}
	}
	return Configuration{
		Version: cfg.Version,
		Payload: payload,
	}
}

// Server represents a server
type Server struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Status        string            `json:"status,omitempty"`
	Location      string            `json:"location,omitempty"`
	IPAddress     string            `json:"ip_address,omitempty"`
	Hostname      string            `json:"hostname,omitempty"`
	Tags          map[string]string `json:"tags,omitempty"`
	Metrics       *ServerMetrics    `json:"metrics,omitempty"`
	LastCheckIn   *NullableTime     `json:"last_check_in,omitempty"`
	CreatedAt     *NullableTime     `json:"created_at,omitempty"`
	UpdatedAt     *NullableTime     `json:"updated_at,omitempty"`
	Config        Configuration     `json:"configuration"`
	Platform      *ServerPlatform   `json:"platform,omitempty"`
	SSHPublicKeys []SSHPublicKey    `json:"ssh_public_keys,omitempty"`
}

// SSHPublicKey represents an SSH public key stored for a server
type SSHPublicKey struct {
	ID          string `json:"id"`
	Key         string `json:"key,omitempty"`
	Label       string `json:"label,omitempty"`
	Fingerprint string `json:"fingerprint"`
	AddedAt     string `json:"added_at"`
}

// ServerMetrics represents current server metrics
type ServerMetrics struct {
	CPUUsage    float64    `json:"cpu_usage"`
	MemoryUsage float64    `json:"memory_usage"`
	DiskUsage   float64    `json:"disk_usage"`
	UpdatedAt   *time.Time `json:"updated_at,omitempty"`
}

// MetricsDataPoint represents a single metrics data point in history
type MetricsDataPoint struct {
	Timestamp   time.Time `json:"timestamp"`
	CPUUsage    float64   `json:"cpu_usage"`
	MemoryUsage float64   `json:"memory_usage"`
	DiskUsage   float64   `json:"disk_usage"`
}

// MetricsHistoryResponse represents the response from metrics history endpoint
type MetricsHistoryResponse struct {
	ServerID   string             `json:"server_id"`
	Period     string             `json:"period"`
	Resolution string             `json:"resolution"`
	DataPoints []MetricsDataPoint `json:"data_points"`
}

// Activity represents a server activity event
type Activity struct {
	ID          string                 `json:"id"`
	ServerID    string                 `json:"server_id"`
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	Status      string                 `json:"status"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Timestamp   time.Time              `json:"timestamp"`
}

// ActivityResponse represents the response from activity endpoint
type ActivityResponse struct {
	Count      int        `json:"count"`
	TotalCount int        `json:"total_count"`
	Results    []Activity `json:"results"`
	Timestamp  time.Time  `json:"timestamp"`
}

// ServerListResponse represents the response from list servers endpoint
type ServerListResponse struct {
	Results    []Server         `json:"results"`
	TotalCount int              `json:"total_count"`
	Count      int              `json:"count"`
	Stats      *ServerListStats `json:"stats,omitempty"`
	Timestamp  time.Time        `json:"timestamp"`
}

// ServerListStats represents aggregated server statistics
type ServerListStats struct {
	Total          int     `json:"total"`
	Online         int     `json:"online"`
	Offline        int     `json:"offline"`
	Degraded       int     `json:"degraded"`
	AvgCPUUsage    float64 `json:"avg_cpu_usage"`
	AvgMemoryUsage float64 `json:"avg_memory_usage"`
}

// ListServersParams represents query parameters for listing servers
type ListServersParams struct {
	Status   string            `json:"status,omitempty"`
	Location string            `json:"location,omitempty"`
	Search   string            `json:"search,omitempty"`
	IDs      []string          `json:"ids,omitempty"`
	Tags     map[string]string `json:"tags,omitempty"`
	Limit    int               `json:"limit,omitempty"`
	Offset   int               `json:"offset,omitempty"`
}

// GetActivityParams represents query parameters for getting activity
type GetActivityParams struct {
	Type   string     `json:"type,omitempty"`
	From   *time.Time `json:"from,omitempty"`
	To     *time.Time `json:"to,omitempty"`
	Limit  int        `json:"limit,omitempty"`
	Offset int        `json:"offset,omitempty"`
}

// UpdateServerRequest represents the request to update server metadata
type UpdateServerRequest struct {
	Location      string `json:"location,omitempty"`
	IPAddress     string `json:"ip_address,omitempty"`
	Hostname      string `json:"hostname,omitempty"`
	RaftAddress   string `json:"raft_address,omitempty"`   // Auto-reported by agent when Raft enabled
	CAFingerprint string `json:"ca_fingerprint,omitempty"` // Auto-reported by agent
}

// MetricsUpdateRequest represents the request to update server metrics
type MetricsUpdateRequest struct {
	CPUUsage    float64 `json:"cpu_usage"`
	MemoryUsage float64 `json:"memory_usage"`
	DiskUsage   float64 `json:"disk_usage"`
}

// DockerData represents Docker information for a server
type DockerData struct {
	Containers []interface{} `json:"containers"`
	Images     []interface{} `json:"images"`
	Volumes    []interface{} `json:"volumes"`
	Networks   []interface{} `json:"networks"`
	Services   []interface{} `json:"services,omitempty"`
}

// DockerDataUpdateRequest represents the request to update server Docker data
type DockerDataUpdateRequest struct {
	Containers []interface{} `json:"containers,omitempty"`
	Images     []interface{} `json:"images,omitempty"`
	Volumes    []interface{} `json:"volumes,omitempty"`
	Networks   []interface{} `json:"networks,omitempty"`
	Services   []interface{} `json:"services,omitempty"`
}

// ProviderInstance represents an installed provider on a server
type ProviderInstance struct {
	ProviderID   string   `json:"provider_id"`
	Version      string   `json:"version"`
	State        string   `json:"state"` // installing, installed, running, stopped, failed, uninstalled
	Capabilities []string `json:"capabilities"`
	InstalledAt  string   `json:"installed_at,omitempty"`
	UpdatedAt    string   `json:"updated_at,omitempty"`
	Error        string   `json:"error,omitempty"` // Error message if state is failed
}

// ProvidersUpdateRequest represents the request to update server provider data
type ProvidersUpdateRequest struct {
	Providers       []ProviderInstance `json:"providers"`
	RegistryVersion int                `json:"registry_version"`
	LastSyncAt      string             `json:"last_sync_at"`
}
