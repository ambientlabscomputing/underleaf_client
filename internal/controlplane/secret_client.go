package controlplane

import (
	"context"
	"fmt"
	"net/url"
)

// ==================== SECRET METADATA CLIENT ====================

// SecretMetadata mirrors server_api types.SecretMetadata for the client side.
type SecretMetadata struct {
	ID                 string              `json:"id"`
	OrgID              string              `json:"org_id"`
	Name               string              `json:"name"`
	Scope              string              `json:"scope"`
	OriginClusterID    string              `json:"origin_cluster_id"`
	CurrentVersion     uint64              `json:"current_version"`
	VersionHistory     []VersionEntry      `json:"version_history"`
	ReplicationTargets []ReplicationTarget `json:"replication_targets,omitempty"`
	State              string              `json:"state"`
	CreatedBy          string              `json:"created_by"`
	CreatedAt          string              `json:"created_at"`
	UpdatedAt          string              `json:"updated_at"`
	Version            int                 `json:"version"`
}

// VersionEntry mirrors server_api types.VersionEntry.
type VersionEntry struct {
	Version     uint64 `json:"version"`
	Fingerprint string `json:"fingerprint"`
	CreatedBy   string `json:"created_by"`
	CreatedAt   string `json:"created_at"`
}

// ReplicationTarget mirrors server_api types.ReplicationTarget.
type ReplicationTarget struct {
	ClusterID     string  `json:"cluster_id"`
	ServerID      string  `json:"server_id,omitempty"`
	Status        string  `json:"status"`
	GrantID       string  `json:"grant_id,omitempty"`
	LastSyncedAt  *string `json:"last_synced_at,omitempty"`
	SyncedVersion uint64  `json:"synced_version"`
}

// CreateSecretMetadataRequest is the body for POST /servers/secrets.
type CreateSecretMetadataRequest struct {
	Name            string `json:"name"`
	Scope           string `json:"scope"`
	OriginClusterID string `json:"origin_cluster_id"`
	Fingerprint     string `json:"fingerprint,omitempty"`
}

// QuerySecretMetadataResponse is the response for GET /servers/secrets.
type QuerySecretMetadataResponse struct {
	Results    []SecretMetadata `json:"results"`
	TotalCount int              `json:"total_count"`
	Count      int              `json:"count"`
	Timestamp  string           `json:"timestamp"`
}

// PatchSecretMetadataRequest is the body for PATCH /servers/secrets/:id.
type PatchSecretMetadataRequest struct {
	State              *string              `json:"state,omitempty"`
	CurrentVersion     *uint64              `json:"current_version,omitempty"`
	ReplicationTargets *[]ReplicationTarget `json:"replication_targets,omitempty"`
	VersionEntry       *VersionEntry        `json:"version_entry,omitempty"`
}

// CPlaneSecretsClient handles secret metadata coordination with server_api.
type CPlaneSecretsClient struct {
	api *APIClient
}

// NewCPlaneSecretsClient creates a new secrets client.
func NewCPlaneSecretsClient(api *APIClient) *CPlaneSecretsClient {
	return &CPlaneSecretsClient{api: api}
}

// CreateSecretMetadata registers a new secret in the control plane.
func (c *CPlaneSecretsClient) CreateSecretMetadata(ctx context.Context, req CreateSecretMetadataRequest) (*SecretMetadata, error) {
	var result SecretMetadata
	if err := c.api.POST(ctx, "/secrets", req, &result); err != nil {
		return nil, fmt.Errorf("failed to create secret metadata: %w", err)
	}
	return &result, nil
}

// ListSecretMetadata retrieves paginated secret metadata for the org.
func (c *CPlaneSecretsClient) ListSecretMetadata(ctx context.Context, nameContains, scope, state string, limit, offset int) (*QuerySecretMetadataResponse, error) {
	params := url.Values{}
	if nameContains != "" {
		params.Set("name_contains", nameContains)
	}
	if scope != "" {
		params.Set("scope", scope)
	}
	if state != "" {
		params.Set("state", state)
	}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	if offset > 0 {
		params.Set("offset", fmt.Sprintf("%d", offset))
	}

	var result QuerySecretMetadataResponse
	if err := c.api.GETWithParams(ctx, "/secrets", params, &result); err != nil {
		return nil, fmt.Errorf("failed to list secret metadata: %w", err)
	}
	return &result, nil
}

// GetSecretMetadata retrieves a single secret metadata record by ID.
func (c *CPlaneSecretsClient) GetSecretMetadata(ctx context.Context, id string) (*SecretMetadata, error) {
	var result SecretMetadata
	if err := c.api.GET(ctx, "/secrets/"+id, &result); err != nil {
		return nil, fmt.Errorf("failed to get secret metadata: %w", err)
	}
	return &result, nil
}

// PatchSecretMetadata partially updates a secret metadata record.
func (c *CPlaneSecretsClient) PatchSecretMetadata(ctx context.Context, id string, req PatchSecretMetadataRequest) (*SecretMetadata, error) {
	var result SecretMetadata
	if err := c.api.PATCH(ctx, "/secrets/"+id, req, &result); err != nil {
		return nil, fmt.Errorf("failed to patch secret metadata: %w", err)
	}
	return &result, nil
}

// DeleteSecretMetadata removes a secret metadata record from the control plane.
func (c *CPlaneSecretsClient) DeleteSecretMetadata(ctx context.Context, id string) error {
	var resp map[string]interface{}
	if err := c.api.DELETE(ctx, "/secrets/"+id, &resp); err != nil {
		return fmt.Errorf("failed to delete secret metadata: %w", err)
	}
	return nil
}

// IssueReplicationGrantRequest is the body for POST /servers/secrets/:id/grant.
type IssueReplicationGrantRequest struct {
	SourceServerID string `json:"source_server_id"`
	DestClusterID  string `json:"dest_cluster_id"`
	DestServerID   string `json:"dest_server_id"`
	TTLSeconds     int    `json:"ttl_seconds,omitempty"`
}

// IssueReplicationGrantResponse is the response from POST /servers/secrets/:id/grant.
type IssueReplicationGrantResponse struct {
	ChannelID     string `json:"channel_id"`
	ChannelGrant  string `json:"channel_grant"`
	SecretID      string `json:"secret_id"`
	SecretName    string `json:"secret_name"`
	Version       uint64 `json:"version"`
	DestPubKeyPEM string `json:"dest_public_key_pem,omitempty"`
}

// IssueReplicationGrant requests a signed channel grant JWT for replicating
// secretID to the specified destination cluster/server.
func (c *CPlaneSecretsClient) IssueReplicationGrant(ctx context.Context, secretID string, req IssueReplicationGrantRequest) (*IssueReplicationGrantResponse, error) {
	var result IssueReplicationGrantResponse
	if err := c.api.POST(ctx, "/secrets/"+secretID+"/grant", req, &result); err != nil {
		return nil, fmt.Errorf("failed to issue replication grant: %w", err)
	}
	return &result, nil
}
