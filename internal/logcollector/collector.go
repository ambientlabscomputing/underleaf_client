package logcollector

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// LogCollectionCommand matches the payload published by the server API.
type LogCollectionCommand struct {
	ServerID  string `json:"server_id"`
	TraceID   string `json:"trace_id"`
	Lines     int    `json:"lines"`
	UploadURL string `json:"upload_url"`
}

// Uploader is implemented by the control plane log client.
type Uploader interface {
	UploadLogs(ctx context.Context, serverID, traceID string, data []byte) error
}

// Collector handles incoming log collection commands.
type Collector struct {
	uploader Uploader
}

// New creates a new Collector.
func New(uploader Uploader) *Collector {
	return &Collector{uploader: uploader}
}

// HandleEvent processes a raw Mycelium Spine payload for a
// "logs.collect.server.request" event.
func (c *Collector) HandleEvent(ctx context.Context, payload []byte) {
	slog.Info("log collection event received", "payload_size", len(payload))

	var cmd LogCollectionCommand
	if err := json.Unmarshal(payload, &cmd); err != nil {
		slog.Error("failed to parse log collection command", "error", err)
		return
	}

	slog.Info("collecting logs",
		"server_id", cmd.ServerID,
		"trace_id", cmd.TraceID,
		"lines", cmd.Lines,
	)

	if err := c.collectAndUpload(ctx, cmd); err != nil {
		slog.Error("log collection failed", "error", err, "trace_id", cmd.TraceID)
	}
}

// collectAndUpload reads the structured log, zips it, and POSTs it.
func (c *Collector) collectAndUpload(ctx context.Context, cmd LogCollectionCommand) error {
	lines := cmd.Lines
	if lines <= 0 {
		lines = 500
	}

	logPath := filepath.Join(os.TempDir(), "underleaf-agent-structured.log")

	content, err := readLastNLines(logPath, lines)
	if err != nil {
		slog.Warn("structured log not found, uploading empty archive", "path", logPath, "error", err)
		content = []byte{}
	}

	zipData, err := buildZip("structured.jsonl", content)
	if err != nil {
		return fmt.Errorf("failed to create log zip: %w", err)
	}

	if err := c.uploader.UploadLogs(ctx, cmd.ServerID, cmd.TraceID, zipData); err != nil {
		return fmt.Errorf("failed to upload log archive: %w", err)
	}

	slog.Info("log archive uploaded",
		"trace_id", cmd.TraceID,
		"bytes", len(zipData),
	)
	return nil
}

// readLastNLines reads the last n lines from the file at path.
func readLastNLines(path string, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading log file: %w", err)
	}

	start := 0
	if len(lines) > n {
		start = len(lines) - n
	}
	lines = lines[start:]

	var buf bytes.Buffer
	for _, l := range lines {
		buf.WriteString(l)
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

// buildZip creates an in-memory zip archive containing a single file.
func buildZip(filename string, content []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	fw, err := zw.Create(filename)
	if err != nil {
		return nil, err
	}
	if _, err := fw.Write(content); err != nil {
		return nil, err
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
