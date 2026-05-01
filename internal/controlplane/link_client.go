package controlplane

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// LinkRecord mirrors the server_api Link projection for CLI use.
// A Link is a read-side unification of Exposures, Tunnels, and Channels.
type LinkRecord struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`       // "exposure" | "tunnel" | "channel"
	Visibility string `json:"visibility"` // "public" | "token" | "peer"
	Target     string `json:"target"`
	URLOrPeer  string `json:"url_or_peer"`
	ServerID   string `json:"server_id"`
	Hostname   string `json:"hostname"`
	State      string `json:"state"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

// QueryLinksResponse is returned by server_api GET /links.
type QueryLinksResponse struct {
	Links        []*LinkRecord  `json:"links"`
	Total        int            `json:"total"`
	Limit        int            `json:"limit"`
	Offset       int            `json:"offset"`
	CountsByKind map[string]int `json:"counts_by_kind"`
}

// CPlaneLinkClient wraps link-related (read-only) control plane API calls.
type CPlaneLinkClient struct {
	api *APIClient
}

// NewCPlaneLinkClient creates a new CPlaneLinkClient.
func NewCPlaneLinkClient(api *APIClient) *CPlaneLinkClient {
	return &CPlaneLinkClient{api: api}
}

// QueryLinks lists links across all kinds with optional filters.
// kind: "" | "exposure" | "tunnel" | "channel"
// visibility: "" | "public" | "token" | "peer"
// status: "" | "in_progress" | "success" | "failure"
// serverID: optional server filter
func (c *CPlaneLinkClient) QueryLinks(ctx context.Context, kind, visibility, status, serverID string, limit, offset int) (*QueryLinksResponse, error) {
	q := url.Values{}
	if kind != "" {
		q.Set("kind", kind)
	}
	if visibility != "" {
		q.Set("visibility", visibility)
	}
	if status != "" {
		q.Set("status", status)
	}
	if serverID != "" {
		q.Set("server_id", serverID)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}

	path := "/links"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}

	var resp QueryLinksResponse
	if err := c.api.GET(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("query links: %w", err)
	}
	return &resp, nil
}
