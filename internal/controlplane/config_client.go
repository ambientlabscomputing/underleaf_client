package controlplane

import (
	"context"
	"fmt"
)

// ConfigClient handles config-specific control plane API calls
type ConfigClient struct {
	api *APIClient
}

// NewConfigClient creates a new config client
func NewConfigClient(api *APIClient) *ConfigClient {
	return &ConfigClient{
		api: api,
	}
}

// GetServerConfig fetches the config for a specific server
// Returns the config payload, version, and error
func (c *ConfigClient) GetServerConfig(ctx context.Context, serverID string) (map[string]interface{}, int, error) {
	var response struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Config struct {
			Version int                    `json:"version"`
			Payload map[string]interface{} `json:"payload"`
		} `json:"configuration"`
	}

	if err := c.api.GET("/servers/"+serverID, &response); err != nil {
		return nil, 0, fmt.Errorf("failed to fetch server: %w", err)
	}

	return response.Config.Payload, response.Config.Version, nil
}
