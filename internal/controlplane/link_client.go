package controlplane

import (
	"context"
	"fmt"
	"net/url"
)

type LinkSpec struct {
	DeploymentID       string `json:"deployment_id,omitempty"`
	ServiceName        string `json:"service_name,omitempty"`
	TargetPort         int    `json:"target_port,omitempty"`
	Target             string `json:"target,omitempty"`
	TargetType         string `json:"target_type,omitempty"`
	SourceServerID     string `json:"source_server_id,omitempty"`
	DestServerID       string `json:"dest_server_id,omitempty"`
	Purpose            string `json:"purpose,omitempty"`
	InitiatorLocalAddr string `json:"initiator_local_addr,omitempty"`
	TTLSeconds         int    `json:"ttl_seconds,omitempty"`
}

type LinkRecord struct {
	ID           string   `json:"id"`
	OrgID        string   `json:"org_id"`
	Kind         string   `json:"kind"`
	Visibility   string   `json:"visibility"`
	ServerID     string   `json:"server_id,omitempty"`
	Hostname     string   `json:"hostname,omitempty"`
	LeaseID      string   `json:"lease_id,omitempty"`
	PublicURL    string   `json:"public_url,omitempty"`
	State        string   `json:"state"`
	Status       string   `json:"status"`
	ErrorMessage string   `json:"error_message,omitempty"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
	ExpiresAt    string   `json:"expires_at,omitempty"`
	Spec         LinkSpec `json:"spec"`
}

type QueryLinksResponse struct {
	Links  []*LinkRecord `json:"links"`
	Total  int           `json:"total"`
	Limit  int           `json:"limit"`
	Offset int           `json:"offset"`
}

type CreateLinkRequest struct {
	Kind            string   `json:"kind"`
	Visibility      string   `json:"visibility,omitempty"`
	ServerID        string   `json:"server_id,omitempty"`
	Hostname        string   `json:"hostname,omitempty"`
	Spec            LinkSpec `json:"spec"`
	DeploymentJobID string   `json:"deployment_job_id,omitempty"`
}

type CreateLinkResponse struct {
	Link             *LinkRecord `json:"link"`
	Grant            string      `json:"grant,omitempty"`
	HyphaeTunnelAddr string      `json:"hyphae_tunnel_addr,omitempty"`
}

type LinkResultRequest struct {
	ServerID  string `json:"server_id,omitempty"`
	Role      string `json:"role,omitempty"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	LocalAddr string `json:"local_addr,omitempty"`
}

type CPlaneLinkClient struct {
	api *APIClient
}

func NewCPlaneLinkClient(api *APIClient) *CPlaneLinkClient {
	return &CPlaneLinkClient{api: api}
}

func (c *CPlaneLinkClient) CreateLink(ctx context.Context, req CreateLinkRequest) (*CreateLinkResponse, error) {
	var resp CreateLinkResponse
	if err := c.api.POST(ctx, "/links", req, &resp); err != nil {
		return nil, fmt.Errorf("create link: %w", err)
	}
	return &resp, nil
}

func (c *CPlaneLinkClient) QueryLinks(ctx context.Context, kind, visibility, serverID, deploymentID, status string, limit, offset int) (*QueryLinksResponse, error) {
	params := url.Values{}
	if kind != "" {
		params.Set("kind", kind)
	}
	if visibility != "" {
		params.Set("visibility", visibility)
	}
	if serverID != "" {
		params.Set("server_id", serverID)
	}
	if deploymentID != "" {
		params.Set("deployment_id", deploymentID)
	}
	if status != "" {
		params.Set("status", status)
	}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	if offset > 0 {
		params.Set("offset", fmt.Sprintf("%d", offset))
	}

	path := "/links"
	if encoded := params.Encode(); encoded != "" {
		path += "?" + encoded
	}

	var resp QueryLinksResponse
	if err := c.api.GET(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("query links: %w", err)
	}
	return &resp, nil
}

func (c *CPlaneLinkClient) GetLink(ctx context.Context, id string) (*LinkRecord, error) {
	var link LinkRecord
	if err := c.api.GET(ctx, fmt.Sprintf("/links/%s", id), &link); err != nil {
		return nil, fmt.Errorf("get link: %w", err)
	}
	return &link, nil
}

func (c *CPlaneLinkClient) CloseLink(ctx context.Context, id string) error {
	if err := c.api.DELETE(ctx, fmt.Sprintf("/links/%s", id), nil); err != nil {
		return fmt.Errorf("close link: %w", err)
	}
	return nil
}

func (c *CPlaneLinkClient) PostLinkResult(ctx context.Context, linkID string, req LinkResultRequest) error {
	if err := c.api.POST(ctx, fmt.Sprintf("/links/%s/results", linkID), req, nil); err != nil {
		return fmt.Errorf("post link result: %w", err)
	}
	return nil
}
