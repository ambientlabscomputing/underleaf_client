package policy_manager

import (
	"context"
	"encoding/json"
	"log/slog"
)

// ControlPlaneConfigAdapter adapts controlplane.ConfigClient to our interface
type ControlPlaneConfigAdapter struct {
	configClient interface {
		GetServerConfig(ctx context.Context, serverID string) (map[string]interface{}, int, error)
	}
}

// NewControlPlaneConfigAdapter creates a new control plane adapter
func NewControlPlaneConfigAdapter(client interface {
	GetServerConfig(ctx context.Context, serverID string) (map[string]interface{}, int, error)
}) *ControlPlaneConfigAdapter {
	return &ControlPlaneConfigAdapter{
		configClient: client,
	}
}

// GetServerConfig fetches config from control plane
func (a *ControlPlaneConfigAdapter) GetServerConfig(ctx context.Context, serverID string) (map[string]interface{}, int, error) {
	return a.configClient.GetServerConfig(ctx, serverID)
}

// ParseServerDataUpdateEvent parses the server-data-update event payload
func ParseServerDataUpdateEvent(payload []byte) (serverID string, version int, config map[string]interface{}, err error) {
	var event struct {
		ServerID string                 `json:"server_id"`
		Version  int                    `json:"version"`
		Config   map[string]interface{} `json:"config"`
	}

	if err := json.Unmarshal(payload, &event); err != nil {
		slog.Error("failed to parse server-data-update event", "error", err)
		return "", 0, nil, err
	}

	return event.ServerID, event.Version, event.Config, nil
}
