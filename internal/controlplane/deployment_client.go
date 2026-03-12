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

// SourceTargeting selects which servers receive a source-deployed app.
type SourceTargeting struct {
	Mode      string            `json:"mode"`
	ServerIDs []string          `json:"server_ids,omitempty"`
	Tags      map[string]string `json:"tags,omitempty"`
}

// DeployFromSourceRequest is the body for POST /deployments/source.
type DeployFromSourceRequest struct {
	Source      string           `json:"source"`
	Ref         string           `json:"ref,omitempty"`
	Targeting   *SourceTargeting `json:"targeting,omitempty"`
	GitHubToken string           `json:"github_token,omitempty"`
}

// DeployFromSourceResponse is returned by POST /deployments/source.
type DeployFromSourceResponse struct {
	DeploymentID string `json:"deployment_id"`
	JobID        string `json:"job_id"`
	Slug         string `json:"slug"`
	Source       string `json:"source"`
	Timestamp    string `json:"timestamp"`
}

// DeployFromSource calls POST /deployments/source and returns the created deployment info.
func (c *DeploymentClient) DeployFromSource(ctx context.Context, req DeployFromSourceRequest) (*DeployFromSourceResponse, error) {
	slog.Info("deploying from source", "source", req.Source, "ref", req.Ref)

	var response DeployFromSourceResponse
	if err := c.api.POST(ctx, "/deployments/source", req, &response); err != nil {
		return nil, fmt.Errorf("failed to deploy from source: %w", err)
	}

	slog.Info("deploy from source submitted",
		"deployment_id", response.DeploymentID,
		"job_id", response.JobID,
		"slug", response.Slug,
	)

	return &response, nil
}
