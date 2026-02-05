package controlplane

import (
	"context"
	"net/http"

	"github.com/ambientlabscomputing/underleaf_client/internal/deployment"
	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
	servertypes "github.com/ambientlabscomputing/underleaf_client/internal/types/server"
)

type CPlaneClient struct {
	Servers     CPlaneServerClient
	Config      CPlaneConfigClient
	Commands    CPlaneCommandClient
	Deployments CPlaneDeploymentClient
	Auth        *CPlaneAuthClient
	Users       *CPlaneUserClient
	config      *policy_manager.ConfigClient
	httpClient  *http.Client
	apiClient   *APIClient
}

func NewCPlaneClient(config *policy_manager.ConfigClient, h *http.Client) *CPlaneClient {
	apiClient := NewAPIClient(*config, h)
	return &CPlaneClient{
		config:      config,
		httpClient:  h,
		apiClient:   apiClient,
		Servers:     NewCPlaneServerClient(config, apiClient),
		Config:      NewCPlaneConfigClient(apiClient),
		Commands:    NewCommandClient(apiClient),
		Deployments: NewDeploymentClient(apiClient),
		Auth:        NewAuthClient(apiClient),
		Users:       NewUserClient(apiClient),
	}
}

type CPlaneServerClient interface {
	RegisterServer(ctx context.Context, name string, platform interface{}) (interface{}, error)
	GetServer(ctx context.Context, serverID string) (interface{}, error)
	ListServers(ctx context.Context) ([]interface{}, error)
	ListServersWithParams(ctx context.Context, params servertypes.ListServersParams) ([]interface{}, error)
	UpdateServer(ctx context.Context, serverID string, updates servertypes.UpdateServerRequest) (interface{}, error)
	GetServerMetrics(ctx context.Context, serverID string) (*servertypes.ServerMetrics, error)
	UpdateServerMetrics(ctx context.Context, serverID string, metrics servertypes.MetricsUpdateRequest) error
	UpdateServerDockerData(ctx context.Context, serverID string, dockerData servertypes.DockerDataUpdateRequest) error
	UpdateClusterMemberStatus(ctx context.Context, clusterID, serverID, role, leaderID string) error
	GetMetricsHistory(ctx context.Context, serverID string, period string, resolution string) (*servertypes.MetricsHistoryResponse, error)
	GetServerActivity(ctx context.Context, serverID string, params servertypes.GetActivityParams) (*servertypes.ActivityResponse, error)
}

func NewCPlaneServerClient(config *policy_manager.ConfigClient, a *APIClient) CPlaneServerClient {
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

type CPlaneDeploymentClient interface {
	ReportDeploymentResult(ctx context.Context, result deployment.DeploymentResult) error
	ReportDeploymentProgress(ctx context.Context, progress deployment.DeploymentProgress) error
}

func NewCPlaneDeploymentClient(a *APIClient) CPlaneDeploymentClient {
	return NewDeploymentClient(a)
}

// API returns the underlying API client for direct API calls
func (c *CPlaneClient) API() *APIClient {
	return c.apiClient
}
