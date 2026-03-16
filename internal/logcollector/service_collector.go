package logcollector

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/ambientlabscomputing/underleaf_client/internal/runner"
)

// ServiceLogCollectionCommand matches the payload published by the server API for
// "logs.collect.service.request" events.
type ServiceLogCollectionCommand struct {
	ServerID       string `json:"server_id"`
	TraceID        string `json:"trace_id"`
	DeploymentID   string `json:"deployment_id"`
	DeploymentSlug string `json:"deployment_slug"`
	// ServiceName restricts log collection to one container. Empty means collect all.
	ServiceName string `json:"service_name,omitempty"`
	Lines       int    `json:"lines"`
	UploadURL   string `json:"upload_url"`
}

// ServiceUploader is implemented by the control plane log client.
type ServiceUploader interface {
	UploadServiceLogs(ctx context.Context, serverID, traceID string, data []byte) error
}

// LogFetcher is the subset of *runner.Runner used by the ServiceCollector.
// Using an interface keeps the collector testable without a live Docker daemon.
type LogFetcher interface {
	GetDeploymentContainerLogs(ctx context.Context, deploymentSlug, serviceName string, lines int) ([]runner.ContainerLogEntry, error)
}

// ServiceCollector handles incoming service log collection commands.
type ServiceCollector struct {
	fetcher  LogFetcher
	uploader ServiceUploader
}

// NewServiceCollector creates a new ServiceCollector.
func NewServiceCollector(fetcher LogFetcher, uploader ServiceUploader) *ServiceCollector {
	return &ServiceCollector{fetcher: fetcher, uploader: uploader}
}

// HandleEvent processes a raw Mycelium Spine payload for a
// "logs.collect.service.request" event.
func (c *ServiceCollector) HandleEvent(ctx context.Context, payload []byte) {
	slog.Info("service log collection event received", "payload_size", len(payload))

	var cmd ServiceLogCollectionCommand
	if err := json.Unmarshal(payload, &cmd); err != nil {
		slog.Error("failed to parse service log collection command", "error", err)
		return
	}

	slog.Info("collecting service logs",
		"server_id", cmd.ServerID,
		"deployment_id", cmd.DeploymentID,
		"deployment_slug", cmd.DeploymentSlug,
		"service_name", cmd.ServiceName,
		"trace_id", cmd.TraceID,
		"lines", cmd.Lines,
	)

	if err := c.collectAndUpload(ctx, cmd); err != nil {
		slog.Error("service log collection failed", "error", err, "trace_id", cmd.TraceID)
	}
}

// collectAndUpload fetches container logs, zips them, and uploads the archive.
func (c *ServiceCollector) collectAndUpload(ctx context.Context, cmd ServiceLogCollectionCommand) error {
	lines := cmd.Lines
	if lines <= 0 {
		lines = 500
	}

	entries, err := c.fetcher.GetDeploymentContainerLogs(ctx, cmd.DeploymentSlug, cmd.ServiceName, lines)
	if err != nil {
		return fmt.Errorf("failed to fetch container logs: %w", err)
	}

	if len(entries) == 0 {
		slog.Warn("no containers found for deployment, uploading empty archive",
			"slug", cmd.DeploymentSlug,
			"service", cmd.ServiceName,
		)
	}

	zipData, err := buildServiceZip(entries)
	if err != nil {
		return fmt.Errorf("failed to create service log zip: %w", err)
	}

	if err := c.uploader.UploadServiceLogs(ctx, cmd.ServerID, cmd.TraceID, zipData); err != nil {
		return fmt.Errorf("failed to upload service log archive: %w", err)
	}

	slog.Info("service log archive uploaded",
		"trace_id", cmd.TraceID,
		"containers", len(entries),
		"bytes", len(zipData),
	)
	return nil
}

// buildServiceZip creates an in-memory zip archive with one file per container.
// Each file is named "<containerName>.log".
func buildServiceZip(entries []runner.ContainerLogEntry) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	for _, e := range entries {
		fw, err := zw.Create(e.ContainerName + ".log")
		if err != nil {
			return nil, fmt.Errorf("failed to create zip entry for %s: %w", e.ContainerName, err)
		}
		if _, err := fw.Write(e.Logs); err != nil {
			return nil, fmt.Errorf("failed to write zip entry for %s: %w", e.ContainerName, err)
		}
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
