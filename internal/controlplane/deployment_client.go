package controlplane

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ambientlabscomputing/underleaf_client/internal/deployment"
)

// DeploymentClient handles deployment result reporting to the control plane
type DeploymentClient struct {
	api *APIClient
}

// NewDeploymentClient creates a new deployment client
func NewDeploymentClient(api *APIClient) *DeploymentClient {
	return &DeploymentClient{
		api: api,
	}
}

// DeploymentResultResponse is the response from reporting deployment results
type DeploymentResultResponse struct {
	Detail string `json:"detail"`
}

// ReportDeploymentResult reports a deployment execution result to the control plane
func (c *DeploymentClient) ReportDeploymentResult(ctx context.Context, result deployment.DeploymentResult) error {
	slog.Info("reporting deployment result",
		"job_id", result.JobID,
		"server_id", result.ServerID,
		"deployment_id", result.DeploymentID,
		"version", result.Version,
		"success", result.Success,
	)

	var response DeploymentResultResponse
	if err := c.api.POST(ctx, "/deployments/results", result, &response); err != nil {
		return fmt.Errorf("failed to report deployment result: %w", err)
	}

	slog.Debug("deployment result reported successfully",
		"job_id", result.JobID,
		"response", response.Detail,
	)

	return nil
}

// DeploymentProgressResponse is the response from reporting deployment progress
type DeploymentProgressResponse struct {
	Detail string `json:"detail"`
}

// ReportDeploymentProgress reports deployment progress to the control plane
func (c *DeploymentClient) ReportDeploymentProgress(ctx context.Context, progress deployment.DeploymentProgress) error {
	slog.Debug("reporting deployment progress",
		"job_id", progress.JobID,
		"server_id", progress.ServerID,
		"deployment_id", progress.DeploymentID,
		"version", progress.Version,
		"stage", progress.Stage,
	)

	var response DeploymentProgressResponse
	if err := c.api.POST(ctx, "/deployments/progress", progress, &response); err != nil {
		return fmt.Errorf("failed to report deployment progress: %w", err)
	}

	slog.Debug("deployment progress reported successfully",
		"job_id", progress.JobID,
		"stage", progress.Stage,
	)

	return nil
}
