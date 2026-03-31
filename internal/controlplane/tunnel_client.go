package controlplane

import (
	"context"
	"fmt"
)

// TunnelResultRequest is the payload sent to server_api POST /tunnels/{id}/results
type TunnelResultRequest struct {
	TunnelID  string `json:"tunnel_id"`
	Status    string `json:"status"` // "bound" or "error"
	PublicURL string `json:"public_url,omitempty"`
	Error     string `json:"error,omitempty"`
}

// CreateTunnelRequest is sent to server_api POST /tunnels
type CreateTunnelRequest struct {
	Target   string `json:"target"`              // port number or full URL
	Hostname string `json:"hostname,omitempty"`  // custom hostname (optional)
	ServerID string `json:"server_id,omitempty"` // omit for local tunnels
}

// TunnelRecord mirrors the server_api Tunnel type for CLI use.
type TunnelRecord struct {
	ID           string `json:"id"`
	OrgID        string `json:"org_id"`
	ServerID     string `json:"server_id"`
	Target       string `json:"target"`
	TargetType   string `json:"target_type"`
	Hostname     string `json:"hostname"`
	LeaseID      string `json:"lease_id"`
	State        string `json:"state"`
	Status       string `json:"status"`
	PublicURL    string `json:"public_url"`
	ErrorMessage string `json:"error_message"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

// CreateTunnelResponse is returned by server_api POST /tunnels
type CreateTunnelResponse struct {
	TunnelRecord
	HyphaeTunnelAddr string `json:"hyphae_tunnel_addr"`
}

// QueryTunnelsResponse is returned by server_api GET /tunnels
type QueryTunnelsResponse struct {
	Tunnels []*TunnelRecord `json:"tunnels"`
	Total   int             `json:"total"`
	Limit   int             `json:"limit"`
	Offset  int             `json:"offset"`
}

// CPlaneTunnelClient wraps tunnel-related control plane API calls.
type CPlaneTunnelClient struct {
	api *APIClient
}

// NewCPlaneTunnelClient creates a new CPlaneTunnelClient.
func NewCPlaneTunnelClient(api *APIClient) *CPlaneTunnelClient {
	return &CPlaneTunnelClient{api: api}
}

// CreateTunnel calls POST /tunnels and returns the created tunnel and its lease address.
func (c *CPlaneTunnelClient) CreateTunnel(ctx context.Context, req CreateTunnelRequest) (*CreateTunnelResponse, error) {
	var resp CreateTunnelResponse
	if err := c.api.POST(ctx, "/tunnels", req, &resp); err != nil {
		return nil, fmt.Errorf("create tunnel: %w", err)
	}
	return &resp, nil
}

// GetTunnel retrieves a single tunnel by ID.
func (c *CPlaneTunnelClient) GetTunnel(ctx context.Context, id string) (*TunnelRecord, error) {
	var tunnel TunnelRecord
	if err := c.api.GET(ctx, fmt.Sprintf("/tunnels/%s", id), &tunnel); err != nil {
		return nil, fmt.Errorf("get tunnel: %w", err)
	}
	return &tunnel, nil
}

// QueryTunnels lists tunnels with optional filters via query parameters.
func (c *CPlaneTunnelClient) QueryTunnels(ctx context.Context, serverID, status string, limit, offset int) (*QueryTunnelsResponse, error) {
	path := "/tunnels"
	sep := "?"
	if serverID != "" {
		path += sep + "server_id=" + serverID
		sep = "&"
	}
	if status != "" {
		path += sep + "status=" + status
		sep = "&"
	}
	if limit > 0 {
		path += fmt.Sprintf("%slimit=%d", sep, limit)
		sep = "&"
	}
	if offset > 0 {
		path += fmt.Sprintf("%soffset=%d", sep, offset)
	}

	var resp QueryTunnelsResponse
	if err := c.api.GET(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("query tunnels: %w", err)
	}
	return &resp, nil
}

// CloseTunnel calls DELETE /tunnels/:id to close and revoke the tunnel.
func (c *CPlaneTunnelClient) CloseTunnel(ctx context.Context, id string) error {
	if err := c.api.DELETE(ctx, fmt.Sprintf("/tunnels/%s", id), nil); err != nil {
		return fmt.Errorf("close tunnel: %w", err)
	}
	return nil
}

// PostTunnelResult reports a bind result for a tunnel to server_api.
// Called by the remote agent after MMA completes a tunnel bind.
func (c *CPlaneTunnelClient) PostTunnelResult(ctx context.Context, tunnelID, status, publicURL, errMsg string) error {
	req := TunnelResultRequest{
		TunnelID:  tunnelID,
		Status:    status,
		PublicURL: publicURL,
		Error:     errMsg,
	}

	if err := c.api.POST(ctx, fmt.Sprintf("/tunnels/%s/results", tunnelID), req, nil); err != nil {
		return fmt.Errorf("failed to post tunnel result: %w", err)
	}

	return nil
}
