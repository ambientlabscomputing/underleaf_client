package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

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

// ReportDeploymentResult reports a deployment execution result to the control plane.
// Retries up to 3 times with exponential backoff (1s, 2s, 4s) on transient failures.
func (c *DeploymentClient) ReportDeploymentResult(ctx context.Context, result deployment.DeploymentResult) error {
	slog.Info("reporting deployment result",
		"job_id", result.JobID,
		"server_id", result.ServerID,
		"deployment_id", result.DeploymentID,
		"version", result.Version,
		"success", result.Success,
	)

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<(attempt-1)) * time.Second
			slog.Warn("retrying deployment result report",
				"attempt", attempt+1,
				"backoff", backoff,
				"deployment_id", result.DeploymentID,
			)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return fmt.Errorf("context cancelled during result retry: %w", ctx.Err())
			}
		}
		var response DeploymentResultResponse
		if err := c.api.POST(ctx, "/deployments/results", result, &response); err != nil {
			lastErr = err
			continue
		}
		slog.Debug("deployment result reported successfully",
			"job_id", result.JobID,
			"response", response.Detail,
		)
		return nil
	}
	return fmt.Errorf("failed to report deployment result after 3 attempts: %w", lastErr)
}

// DeploymentProgressResponse is the response from reporting deployment progress
type DeploymentProgressResponse struct {
	Detail string `json:"detail"`
}

// ReportDeploymentProgress reports deployment progress to the control plane.
// Retries up to 3 times with exponential backoff (1s, 2s, 4s) on transient failures.
func (c *DeploymentClient) ReportDeploymentProgress(ctx context.Context, progress deployment.DeploymentProgress) error {
	slog.Debug("reporting deployment progress",
		"job_id", progress.JobID,
		"server_id", progress.ServerID,
		"deployment_id", progress.DeploymentID,
		"version", progress.Version,
		"stage", progress.Stage,
	)

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<(attempt-1)) * time.Second
			slog.Warn("retrying deployment progress report",
				"attempt", attempt+1,
				"backoff", backoff,
				"deployment_id", progress.DeploymentID,
				"stage", progress.Stage,
			)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return fmt.Errorf("context cancelled during progress retry: %w", ctx.Err())
			}
		}
		var response DeploymentProgressResponse
		if err := c.api.POST(ctx, "/deployments/progress", progress, &response); err != nil {
			lastErr = err
			continue
		}
		slog.Debug("deployment progress reported successfully",
			"job_id", progress.JobID,
			"stage", progress.Stage,
		)
		return nil
	}
	return fmt.Errorf("failed to report deployment progress after 3 attempts: %w", lastErr)
}

// SourceTargeting selects which servers receive a source-deployed app.
type SourceTargeting struct {
	Mode           string            `json:"mode"`
	ServerIDs      []string          `json:"server_ids,omitempty"`
	Tags           map[string]string `json:"tags,omitempty"`
	PreferServerID string            `json:"prefer_server_id,omitempty"`
}

// InlineManifest is the parsed .underleaf/deploy.yaml for local: source deploys.
// It mirrors the server_api InlineManifest type field-for-field.
type InlineManifest struct {
	Version  string                  `json:"version"`
	Name     string                  `json:"name"`
	Slug     string                  `json:"slug,omitempty"`
	Services []InlineManifestService `json:"services"`
	Networks []InlineManifestNetwork `json:"networks,omitempty"`
	Volumes  []InlineManifestVolume  `json:"volumes,omitempty"`
}

// InlineManifestService is a single service entry in an InlineManifest.
type InlineManifestService struct {
	Name        string                `json:"name"`
	Image       string                `json:"image,omitempty"`
	Build       *InlineManifestBuild  `json:"build,omitempty"`
	Ports       []string              `json:"ports,omitempty"`
	Environment map[string]string     `json:"environment,omitempty"`
	Networks    []string              `json:"networks,omitempty"`
	Volumes     []string              `json:"volumes,omitempty"`
	Expose      *InlineManifestExpose `json:"expose,omitempty"`
}

// InlineManifestBuild describes how to build a Docker image from the uploaded archive.
type InlineManifestBuild struct {
	Context    string            `json:"context,omitempty"`
	Dockerfile string            `json:"dockerfile,omitempty"`
	Args       map[string]string `json:"args,omitempty"`
}

// InlineManifestExpose declares that a service should be publicly exposed.
type InlineManifestExpose struct {
	Port     int    `json:"port"`
	Hostname string `json:"hostname,omitempty"`
}

// InlineManifestNetwork defines a Docker network.
type InlineManifestNetwork struct {
	Name   string `json:"name"`
	Driver string `json:"driver,omitempty"`
}

// InlineManifestVolume defines a Docker volume.
type InlineManifestVolume struct {
	Name string `json:"name"`
}

// DeployFromSourceRequest is the body for POST /deployments/source.
type DeployFromSourceRequest struct {
	Source      string           `json:"source"`
	Ref         string           `json:"ref,omitempty"`
	Targeting   *SourceTargeting `json:"targeting,omitempty"`
	GitHubToken string           `json:"github_token,omitempty"`
	// Manifest and ArchiveRef are set for local: sources.
	Manifest   *InlineManifest `json:"manifest,omitempty"`
	ArchiveRef string          `json:"archive_ref,omitempty"`
}

// ExposureInfo is a summary of an auto-created exposure.
type ExposureInfo struct {
	ExposureID  string `json:"exposure_id"`
	ServiceName string `json:"service_name"`
	PublicURL   string `json:"public_url"`
	ServerID    string `json:"server_id"`
}

// DeployFromSourceResponse is returned by POST /deployments/source.
type DeployFromSourceResponse struct {
	DeploymentID string         `json:"deployment_id"`
	JobID        string         `json:"job_id"`
	Slug         string         `json:"slug"`
	Source       string         `json:"source"`
	Timestamp    string         `json:"timestamp"`
	Exposures    []ExposureInfo `json:"exposures,omitempty"`
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

// UploadArchiveResponse is returned by POST /deployments/upload-context.
type UploadArchiveResponse struct {
	ArchiveRef string `json:"archive_ref"`
}

// UploadBuildContext uploads a local build-context tarball to server_api and
// returns the archive_ref to include in the subsequent DeployFromSource request.
// The tarball must be a .tar.gz file created by createBuildArchive in archive.go.
func (c *DeploymentClient) UploadBuildContext(ctx context.Context, tarPath string) (string, error) {
	slog.Info("uploading build context", "path", tarPath)

	data, err := os.ReadFile(tarPath)
	if err != nil {
		return "", fmt.Errorf("failed to read archive %s: %w", tarPath, err)
	}

	baseURL, ok := c.api.config.Get("api.base_url")
	if !ok || baseURL == nil {
		return "", fmt.Errorf("api.base_url not configured")
	}

	uploadURL := baseURL.(string) + "/deployments/upload-context"
	fileName := filepath.Base(tarPath)

	// Capture the raw response so we can parse the archive_ref JSON.
	// We temporarily override the normal POSTMultipartToURL which discards the body.
	// Instead, we build the request manually using the same auth plumbing.
	var rawResp []byte
	if err := c.api.POSTMultipartAndDecode(ctx, uploadURL, nil, "file", fileName, data, &rawResp); err != nil {
		return "", fmt.Errorf("build-context upload failed: %w", err)
	}

	var uploadResp UploadArchiveResponse
	if err := json.Unmarshal(rawResp, &uploadResp); err != nil {
		return "", fmt.Errorf("failed to parse upload response: %w", err)
	}

	slog.Info("build context uploaded", "archive_ref", uploadResp.ArchiveRef)
	return uploadResp.ArchiveRef, nil
}

// AppDeployment mirrors the server_api AppDeployment type for CLI use.
type AppDeployment struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	State     string `json:"state"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Source    *struct {
		Type  string `json:"type"`
		Owner string `json:"owner"`
		Repo  string `json:"repo"`
		Ref   string `json:"ref"`
	} `json:"source,omitempty"`
}

// QueryDeploymentsResponse mirrors server_api's QueryAppDeploymentsResponse.
type QueryDeploymentsResponse struct {
	Results    []AppDeployment `json:"results"`
	TotalCount int             `json:"total_count"`
	Count      int             `json:"count"`
}

// ListDeployments fetches all live deployments for the configured org.
func (c *DeploymentClient) ListDeployments(ctx context.Context, limit, offset int) (*QueryDeploymentsResponse, error) {
	path := fmt.Sprintf("/deployments?limit=%d&offset=%d", limit, offset)
	var resp QueryDeploymentsResponse
	if err := c.api.GET(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}
	return &resp, nil
}

// GetDeployment fetches a single deployment by ID.
func (c *DeploymentClient) GetDeployment(ctx context.Context, id string) (*AppDeployment, error) {
	var dep AppDeployment
	if err := c.api.GET(ctx, "/deployments/"+id, &dep); err != nil {
		return nil, fmt.Errorf("failed to get deployment %q: %w", id, err)
	}
	return &dep, nil
}

// DeleteDeployment deletes a deployment and cascades to its exposures.
func (c *DeploymentClient) DeleteDeployment(ctx context.Context, id string) error {
	if err := c.api.DELETE(ctx, "/deployments/"+id, nil); err != nil {
		return fmt.Errorf("failed to delete deployment %q: %w", id, err)
	}
	return nil
}
