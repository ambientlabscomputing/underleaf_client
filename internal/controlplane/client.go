package controlplane

import (
	"context"
	"net/http"

	"github.com/ambientlabscomputing/underleaf_client/internal/config_manager"
)

type CPlaneClient struct {
	Servers    CPlaneServerClient
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
