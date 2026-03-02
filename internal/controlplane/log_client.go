package controlplane

import (
	"context"
	"fmt"
	"log/slog"
)

// LogClient handles log archive uploads to the control plane.
type LogClient struct {
	api *APIClient
}

// NewLogClient creates a new LogClient.
func NewLogClient(api *APIClient) *LogClient {
	return &LogClient{api: api}
}

// UploadLogs uploads a zipped log archive to the server API using the full
// upload URL provided in the log collection command.  The URL is constructed
// by the server API and sent to the client via Mycelium Spine, so we post
// directly to it rather than building from a base URL.
func (c *LogClient) UploadLogs(ctx context.Context, serverID, traceID string, zipData []byte) error {
	// Build the upload path — UploadLogs receives the full URL injected by the
	// LogCollectionCommand, but the LogClient is designed to be called through
	// the logcollector.Uploader interface which only carries serverID and
	// traceID.  We derive the upload URL from the API base URL here, matching
	// what the server API's CollectServerLogsHandler constructed.
	baseURL, ok := c.api.config.Get("api.base_url")
	if !ok || baseURL == nil {
		return fmt.Errorf("api.base_url not configured")
	}

	uploadURL := fmt.Sprintf("%s/servers/%s/logs/upload", baseURL.(string), serverID)

	slog.Info("uploading log archive",
		"server_id", serverID,
		"trace_id", traceID,
		"url", uploadURL,
		"bytes", len(zipData),
	)

	fields := map[string]string{
		"trace_id": traceID,
	}

	if err := c.api.POSTMultipartToURL(ctx, uploadURL, fields, "file", traceID+".zip", zipData); err != nil {
		return fmt.Errorf("log archive upload failed: %w", err)
	}

	slog.Info("log archive uploaded", "trace_id", traceID)
	return nil
}
