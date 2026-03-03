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
