package controlplane

import (
	"context"
	"fmt"
)

// ExposureResultRequest is the payload sent to server_api POST /exposures/{id}/results
type ExposureResultRequest struct {
	ExposureID string `json:"exposure_id"`
	Status     string `json:"status"` // "bound" or "error"
	PublicURL  string `json:"public_url,omitempty"`
	Error      string `json:"error,omitempty"`
}

// ExposureRecord mirrors the server_api Exposure type for CLI use.
type ExposureRecord struct {
	ID           string `json:"id"`
	OrgID        string `json:"org_id"`
	ServerID     string `json:"server_id"`
	DeploymentID string `json:"deployment_id"`
	ServiceName  string `json:"service_name"`
	TargetPort   int    `json:"target_port"`
	Hostname     string `json:"hostname"`
	LeaseID      string `json:"lease_id"`
	Status       string `json:"status"`
	PublicURL    string `json:"public_url"`
	ErrorMessage string `json:"error_message"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

// QueryExposuresResponse is returned by server_api GET /exposures or GET /deployments/:id/exposures
type QueryExposuresResponse struct {
	Exposures []*ExposureRecord `json:"exposures"`
	Total     int               `json:"total"`
	Limit     int               `json:"limit"`
	Offset    int               `json:"offset"`
}

// CPlaneExposureClient wraps exposure-related control plane API calls.
type CPlaneExposureClient struct {
	api *APIClient
}

// NewCPlaneExposureClient creates a new exposure control plane client.
func NewCPlaneExposureClient(api *APIClient) *CPlaneExposureClient {
	return &CPlaneExposureClient{api: api}
}

// PostExposureResult reports a bind/unbind result for an exposure to server_api.
func (c *CPlaneExposureClient) PostExposureResult(ctx context.Context, exposureID, status, publicURL, errMsg string) error {
	req := ExposureResultRequest{
		ExposureID: exposureID,
		Status:     status,
		PublicURL:  publicURL,
		Error:      errMsg,
	}

	if err := c.api.POST(ctx, fmt.Sprintf("/exposures/%s/results", exposureID), req, nil); err != nil {
		return fmt.Errorf("failed to post exposure result: %w", err)
	}

	return nil
}

// GetDeploymentExposures retrieves all exposures for a deployment.
func (c *CPlaneExposureClient) GetDeploymentExposures(ctx context.Context, deploymentID string) (*QueryExposuresResponse, error) {
	var resp QueryExposuresResponse
	if err := c.api.GET(ctx, "/deployments/"+deploymentID+"/exposures", &resp); err != nil {
		return nil, fmt.Errorf("failed to get deployment exposures: %w", err)
	}
	return &resp, nil
}
