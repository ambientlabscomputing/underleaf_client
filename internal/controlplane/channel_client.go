package controlplane

import (
	"context"
	"fmt"
)

// CreateChannelRequest is sent to server_api POST /channels
type CreateChannelRequest struct {
	SourceServerID string `json:"source_server_id"`
	DestServerID   string `json:"dest_server_id"`
	Purpose        string `json:"purpose,omitempty"`
	TTLSeconds     int    `json:"ttl_seconds,omitempty"`
}

// ChannelRecord mirrors the server_api Channel type for CLI use.
type ChannelRecord struct {
	ID                 string `json:"id"`
	OrgID              string `json:"org_id"`
	SourceServerID     string `json:"source_server_id"`
	DestServerID       string `json:"dest_server_id"`
	Purpose            string `json:"purpose"`
	Status             string `json:"status"`
	ErrorMessage       string `json:"error_message"`
	InitiatorLocalAddr string `json:"initiator_local_addr,omitempty"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
	ExpiresAt          string `json:"expires_at"`
}

// CreateChannelResponse is returned by server_api POST /channels.
// Grant is an ES256 JWT signed by the server — only returned once at creation time.
type CreateChannelResponse struct {
	Channel *ChannelRecord `json:"channel"`
	Grant   string         `json:"grant"`
}

// QueryChannelsResponse is returned by server_api GET /channels
type QueryChannelsResponse struct {
	Channels   []*ChannelRecord `json:"channels"`
	TotalCount int              `json:"total_count"`
	Count      int              `json:"count"`
}

// CPlaneChannelClient wraps channel-related control plane API calls.
type CPlaneChannelClient struct {
	api *APIClient
}

// NewCPlaneChannelClient creates a new CPlaneChannelClient.
func NewCPlaneChannelClient(api *APIClient) *CPlaneChannelClient {
	return &CPlaneChannelClient{api: api}
}

// CreateChannel calls POST /channels and returns the created channel and its one-time grant JWT.
func (c *CPlaneChannelClient) CreateChannel(ctx context.Context, req CreateChannelRequest) (*CreateChannelResponse, error) {
	var resp CreateChannelResponse
	if err := c.api.POST(ctx, "/channels", req, &resp); err != nil {
		return nil, fmt.Errorf("create channel: %w", err)
	}
	return &resp, nil
}

// GetChannel retrieves a single channel by ID.
func (c *CPlaneChannelClient) GetChannel(ctx context.Context, id string) (*ChannelRecord, error) {
	var ch ChannelRecord
	if err := c.api.GET(ctx, fmt.Sprintf("/channels/%s", id), &ch); err != nil {
		return nil, fmt.Errorf("get channel: %w", err)
	}
	return &ch, nil
}

// QueryChannels lists channels with optional filters via query parameters.
func (c *CPlaneChannelClient) QueryChannels(ctx context.Context, sourceServerID, destServerID, status string, limit, offset int) (*QueryChannelsResponse, error) {
	path := "/channels"
	sep := "?"
	if sourceServerID != "" {
		path += sep + "source_server_id=" + sourceServerID
		sep = "&"
	}
	if destServerID != "" {
		path += sep + "dest_server_id=" + destServerID
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

	var resp QueryChannelsResponse
	if err := c.api.GET(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("query channels: %w", err)
	}
	return &resp, nil
}

// PostChannelResult reports a bind result for a channel to server_api.
// localAddr is the loopback relay address for the initiator role (empty for listener).
func (c *CPlaneChannelClient) PostChannelResult(ctx context.Context, channelID, serverID, role, status, errMsg, localAddr string) error {
	req := struct {
		ServerID  string `json:"server_id"`
		Role      string `json:"role"`
		Status    string `json:"status"`
		Error     string `json:"error,omitempty"`
		LocalAddr string `json:"local_addr,omitempty"`
	}{
		ServerID:  serverID,
		Role:      role,
		Status:    status,
		Error:     errMsg,
		LocalAddr: localAddr,
	}

	if err := c.api.POST(ctx, fmt.Sprintf("/channels/%s/results", channelID), req, nil); err != nil {
		return fmt.Errorf("post channel result: %w", err)
	}
	return nil
}

// CloseChannel calls DELETE /channels/:id to close and revoke the channel.
func (c *CPlaneChannelClient) CloseChannel(ctx context.Context, id string) error {
	if err := c.api.DELETE(ctx, fmt.Sprintf("/channels/%s", id), nil); err != nil {
		return fmt.Errorf("close channel: %w", err)
	}
	return nil
}
