package controlplane

import (
	"context"
	"net/http"

	"github.com/ambientlabscomputing/underleaf_client/internal/config_manager"
)

type CPlaneClient struct {
	Servers    CPlaneServerClient
	Config     CPlaneConfigClient
	Commands   CPlaneCommandClient
	config     *config_manager.ConfigClient
	httpClient *http.Client
	apiClient  *APIClient
}

func NewCPlaneClient(config *config_manager.ConfigClient, h *http.Client) *CPlaneClient {
	apiClient := NewAPIClient(*config, h)
	return &CPlaneClient{
		config:     config,
		httpClient: h,
		apiClient:  apiClient,
		Servers:    NewCPlaneServerClient(config, apiClient),
		Config:     NewCPlaneConfigClient(apiClient),
		Commands:   NewCommandClient(apiClient),
	}
}

type CPlaneServerClient interface {
	RegisterServer(ctx context.Context, name string, platform interface{}) (interface{}, error)
	GetServer(ctx context.Context, serverID string) (interface{}, error)
	ListServers(ctx context.Context) ([]interface{}, error)
}

func NewCPlaneServerClient(config *config_manager.ConfigClient, a *APIClient) CPlaneServerClient {
	return NewServerClient(config, a)
}

type CPlaneConfigClient interface {
	GetServerConfig(ctx context.Context, serverID string) (map[string]interface{}, int, error)
}

func NewCPlaneConfigClient(a *APIClient) CPlaneConfigClient {
	return NewConfigClient(a)
}

func NewCPlaneCommandClient(a *APIClient) CPlaneCommandClient {
	return NewCommandClient(a)
}

// API returns the underlying API client for direct API calls
func (c *CPlaneClient) API() *APIClient {
	return c.apiClient
}
