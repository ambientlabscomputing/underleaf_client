package types

import "time"

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
	Location  string `json:"location,omitempty"`
	IPAddress string `json:"ip_address,omitempty"`
	Hostname  string `json:"hostname,omitempty"`
}

// MetricsUpdateRequest represents the request to update server metrics
type MetricsUpdateRequest struct {
	CPUUsage    float64 `json:"cpu_usage"`
	MemoryUsage float64 `json:"memory_usage"`
	DiskUsage   float64 `json:"disk_usage"`
}
