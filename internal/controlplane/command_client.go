package controlplane

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/exec"
)

// CommandClient handles command result reporting to the control plane
type CommandClient struct {
	api *APIClient
}

// NewCommandClient creates a new command client
func NewCommandClient(api *APIClient) *CommandClient {
	return &CommandClient{
		api: api,
	}
}

// CommandResultRequest is the request body for reporting command results
// Must match the API schema exactly: exit_code, server_id, stderr, stdout, timestamp, trace_id
type CommandResultRequest struct {
	ExitCode  int    `json:"exit_code"`
	ServerID  string `json:"server_id"`
	Stderr    string `json:"stderr"`
	Stdout    string `json:"stdout"`
	Timestamp string `json:"timestamp"`
	TraceID   string `json:"trace_id"`
}

// CommandResultResponse is the response from reporting command results
type CommandResultResponse struct {
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// ReportResult reports a command execution result to the control plane
func (c *CommandClient) ReportResult(ctx context.Context, result exec.CommandResult) error {
	// Include error message in stderr if present
	stderr := result.Stderr
	if result.Error != "" {
		if stderr != "" {
			stderr = stderr + "\n" + result.Error
		} else {
			stderr = result.Error
		}
	}

	req := CommandResultRequest{
		ExitCode:  result.ExitCode,
		ServerID:  result.ServerID,
		Stderr:    stderr,
		Stdout:    result.Stdout,
		Timestamp: result.Timestamp.Format(time.RFC3339),
		TraceID:   result.TraceID,
	}

	slog.Info("reporting command result",
		"trace_id", result.TraceID,
		"server_id", result.ServerID,
		"exit_code", result.ExitCode,
	)

	var response CommandResultResponse
	if err := c.api.POST(ctx, "/commands/results", req, &response); err != nil {
		return fmt.Errorf("failed to report command result: %w", err)
	}

	slog.Debug("command result reported successfully",
		"trace_id", result.TraceID,
		"response", response.Message,
	)

	return nil
}

// DispatchCommandRequest is the request body for dispatching a command to servers
type DispatchCommandRequest struct {
	// core command fields
	Command string            `json:"command"`
	WorkDir string            `json:"work_dir,omitempty"`
	EnvVars map[string]string `json:"env_vars,omitempty"`
	Timeout int               `json:"timeout,omitempty"` // in seconds
	TraceID string            `json:"trace_id,omitempty"`

	// server targeting fields
	AllServers bool              `json:"all_servers,omitempty"`
	ServerIDs  []string          `json:"server_ids,omitempty"`
	Tags       map[string]string `json:"tags,omitempty"`
}

// DispatchCommandResponse is the response from dispatching a command
type DispatchCommandResponse struct {
	JobID         string   `json:"job_id"`
	TraceID       string   `json:"trace_id"` // alias for job_id
	Message       string   `json:"message"`
	TargetServers []string `json:"target_servers"`
	Timestamp     string   `json:"timestamp"`
}

// DispatchCommand sends a command to be executed on selected servers
func (c *CommandClient) DispatchCommand(ctx context.Context, req DispatchCommandRequest) (*DispatchCommandResponse, error) {
	slog.Info("dispatching command",
		"command", req.Command,
		"server_ids", req.ServerIDs,
		"all_servers", req.AllServers,
	)

	var response DispatchCommandResponse
	if err := c.api.POST(ctx, "/commands/run", req, &response); err != nil {
		return nil, fmt.Errorf("failed to dispatch command: %w", err)
	}

	// Normalize: use JobID as TraceID if TraceID is empty
	if response.TraceID == "" && response.JobID != "" {
		response.TraceID = response.JobID
	}

	slog.Info("command dispatched",
		"trace_id", response.TraceID,
		"target_servers", len(response.TargetServers),
	)

	return &response, nil
}

// CPlaneCommandClient interface for command operations
type CPlaneCommandClient interface {
	ReportResult(ctx context.Context, result exec.CommandResult) error
	DispatchCommand(ctx context.Context, req DispatchCommandRequest) (*DispatchCommandResponse, error)
}

// Ensure CommandClient implements the interface
var _ CPlaneCommandClient = (*CommandClient)(nil)
