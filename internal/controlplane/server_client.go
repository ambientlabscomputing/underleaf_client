package controlplane

import (
	"context"

	"github.com/ambientlabscomputing/underleaf_client/internal/config_manager"
)

type ServerClient struct {
	config *config_manager.ConfigClient
	api    *APIClient
}

func NewServerClient(config *config_manager.ConfigClient, api *APIClient) *ServerClient {
	return &ServerClient{
		config: config,
		api:    api,
	}
}

func (c *ServerClient) RegisterServer(ctx context.Context, name string, platform interface{}) (interface{}, error) {
	newServerRequest := map[string]interface{}{
		"name":     name,
		"platform": platform,
	}
	var response interface{}
	if err := c.api.POST("/servers", newServerRequest, &response); err != nil {
		return nil, err
	}
	return response, nil
}

func (c *ServerClient) GetServer(ctx context.Context, serverID string) (interface{}, error) {
	var response interface{}
	if err := c.api.GET("/servers/"+serverID, &response); err != nil {
		return nil, err
	}
	return response, nil
}

func (c *ServerClient) ListServers(ctx context.Context) ([]interface{}, error) {
	var response []interface{}
	if err := c.api.GET("/servers", &response); err != nil {
		return nil, err
	}
	return response, nil
}
