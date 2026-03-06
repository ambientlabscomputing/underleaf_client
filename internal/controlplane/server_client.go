package controlplane

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
	servertypes "github.com/ambientlabscomputing/underleaf_client/internal/types/server"
)

type ServerClient struct {
	config *policy_manager.ConfigClient
	api    *APIClient
}

func NewServerClient(config *policy_manager.ConfigClient, api *APIClient) *ServerClient {
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
	if err := c.api.POST(ctx, "/servers", newServerRequest, &response); err != nil {
		return nil, err
	}
	return response, nil
}

func (c *ServerClient) CreateServer(ctx context.Context, name string, sshPublicKey *string) (servertypes.Server, error) {
	newServerRequest := map[string]interface{}{
		"name": name,
	}

	// Add SSH public key if provided
	if sshPublicKey != nil && *sshPublicKey != "" {
		newServerRequest["ssh_public_keys"] = []map[string]string{
			{
				"key": *sshPublicKey,
			},
		}
	}

	var response servertypes.Server
	if err := c.api.POST(ctx, "/servers", newServerRequest, &response); err != nil {
		return servertypes.Server{}, err
	}
	return response, nil
}

func (c *ServerClient) GetServer(ctx context.Context, serverID string) (interface{}, error) {
	var response interface{}
	if err := c.api.GET(ctx, "/servers/"+serverID, &response); err != nil {
		return nil, err
	}
	return response, nil
}

func (c *ServerClient) ListServers(ctx context.Context) ([]interface{}, error) {
	return c.ListServersWithParams(ctx, servertypes.ListServersParams{})
}

func (c *ServerClient) ListServersWithParams(ctx context.Context, params servertypes.ListServersParams) ([]interface{}, error) {
	queryParams := url.Values{}
	if params.Status != "" {
		queryParams.Set("status", params.Status)
	}
	if params.Location != "" {
		queryParams.Set("location", params.Location)
	}
	if params.Search != "" {
		queryParams.Set("search", params.Search)
	}
	if params.Limit > 0 {
		queryParams.Set("limit", strconv.Itoa(params.Limit))
	}
	if params.Offset > 0 {
		queryParams.Set("offset", strconv.Itoa(params.Offset))
	}

	var response struct {
		Results []interface{} `json:"results"`
		Total   int           `json:"total_count"`
	}
	if err := c.api.GETWithParams(ctx, "/servers", queryParams, &response); err != nil {
		return nil, err
	}
	return response.Results, nil
}

func (c *ServerClient) UpdateServer(ctx context.Context, serverID string, updates servertypes.UpdateServerRequest) (interface{}, error) {
	var response interface{}
	if err := c.api.PATCH(ctx, "/servers/"+serverID, updates, &response); err != nil {
		return nil, err
	}
	return response, nil
}

func (c *ServerClient) GetServerMetrics(ctx context.Context, serverID string) (*servertypes.ServerMetrics, error) {
	var response servertypes.ServerMetrics
	if err := c.api.GET(ctx, fmt.Sprintf("/servers/%s/metrics", serverID), &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *ServerClient) UpdateServerMetrics(ctx context.Context, serverID string, metrics servertypes.MetricsUpdateRequest) error {
	var response interface{}
	if err := c.api.PUT(ctx, fmt.Sprintf("/servers/%s/metrics", serverID), metrics, &response); err != nil {
		return err
	}
	return nil
}

func (c *ServerClient) UpdateServerDockerData(ctx context.Context, serverID string, dockerData servertypes.DockerDataUpdateRequest) error {
	var response interface{}
	payload := map[string]servertypes.DockerDataUpdateRequest{
		"docker_data": dockerData,
	}
	if err := c.api.PATCH(ctx, fmt.Sprintf("/servers/%s", serverID), payload, &response); err != nil {
		return err
	}
	return nil
}

func (c *ServerClient) UpdateServerProviders(ctx context.Context, serverID string, providers servertypes.ProvidersUpdateRequest) error {
	var response interface{}
	if err := c.api.PUT(ctx, fmt.Sprintf("/servers/%s/providers", serverID), providers, &response); err != nil {
		return err
	}
	return nil
}

func (c *ServerClient) GetMetricsHistory(ctx context.Context, serverID string, period string, resolution string) (*servertypes.MetricsHistoryResponse, error) {
	queryParams := url.Values{}
	if period != "" {
		queryParams.Set("period", period)
	}
	if resolution != "" {
		queryParams.Set("resolution", resolution)
	}

	var response servertypes.MetricsHistoryResponse
	if err := c.api.GETWithParams(ctx, fmt.Sprintf("/servers/%s/metrics/history", serverID), queryParams, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *ServerClient) GetServerActivity(ctx context.Context, serverID string, params servertypes.GetActivityParams) (*servertypes.ActivityResponse, error) {
	queryParams := url.Values{}
	if params.Type != "" {
		queryParams.Set("type", params.Type)
	}
	if params.From != nil {
		queryParams.Set("from", params.From.Format("2006-01-02T15:04:05Z07:00"))
	}
	if params.To != nil {
		queryParams.Set("to", params.To.Format("2006-01-02T15:04:05Z07:00"))
	}
	if params.Limit > 0 {
		queryParams.Set("limit", strconv.Itoa(params.Limit))
	}
	if params.Offset > 0 {
		queryParams.Set("offset", strconv.Itoa(params.Offset))
	}

	var response servertypes.ActivityResponse
	if err := c.api.GETWithParams(ctx, fmt.Sprintf("/servers/%s/activity", serverID), queryParams, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *ServerClient) UpdateClusterMemberStatus(ctx context.Context, clusterID, serverID, role, leaderID string) error {
	var response interface{}
	payload := map[string]interface{}{
		"role":      role,
		"leader_id": leaderID,
	}
	if err := c.api.POST(ctx, fmt.Sprintf("/clusters/%s/members/%s/heartbeat", clusterID, serverID), payload, &response); err != nil {
		return err
	}
	return nil
}

// AddSSHKey adds an SSH public key to a server
func (c *ServerClient) AddSSHKey(ctx context.Context, serverID string, publicKey string, label string) (servertypes.SSHPublicKey, error) {
	payload := map[string]string{
		"key": publicKey,
	}
	if label != "" {
		payload["label"] = label
	}
	var key servertypes.SSHPublicKey
	if err := c.api.POST(ctx, fmt.Sprintf("/servers/%s/ssh-keys", serverID), payload, &key); err != nil {
		return servertypes.SSHPublicKey{}, err
	}
	return key, nil
}

// ListSSHKeys lists the SSH public keys registered for a server
func (c *ServerClient) ListSSHKeys(ctx context.Context, serverID string) ([]servertypes.SSHPublicKey, error) {
	var resp struct {
		Keys []servertypes.SSHPublicKey `json:"keys"`
	}
	if err := c.api.GET(ctx, fmt.Sprintf("/servers/%s/ssh-keys", serverID), &resp); err != nil {
		return nil, err
	}
	return resp.Keys, nil
}

// RemoveSSHKey removes an SSH public key from a server by key ID
func (c *ServerClient) RemoveSSHKey(ctx context.Context, serverID string, keyID string) error {
	return c.api.DELETE(ctx, fmt.Sprintf("/servers/%s/ssh-keys/%s", serverID, keyID), nil)
}
