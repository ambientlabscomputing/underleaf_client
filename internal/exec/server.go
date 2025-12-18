package exec

import (
	"context"
	"encoding/json"
	"log/slog"
)

// CommandDrainer tracks in-progress commands
type CommandDrainer interface {
	Start(jobID string)
	Complete(jobID string)
	Count() int
}

// CommandHandler handles incoming command execution requests from the event bus
type CommandHandler struct {
	runner       Runner
	resultSender ResultSender
	serverID     string
	drainer      CommandDrainer
}

// ResultSender interface for sending command results back to control plane
type ResultSender interface {
	ReportResult(ctx context.Context, result CommandResult) error
}

// NewCommandHandler creates a new command handler
func NewCommandHandler(runner Runner, resultSender ResultSender, serverID string) *CommandHandler {
	return &CommandHandler{
		runner:       runner,
		resultSender: resultSender,
		serverID:     serverID,
	}
}

// SetDrainer sets the command drainer for tracking in-flight commands
func (h *CommandHandler) SetDrainer(drainer CommandDrainer) {
	h.drainer = drainer
}

// HandleCommandEvent processes a command execution event from the event bus
// This is called when a "commands.run.request" event is received
func (h *CommandHandler) HandleCommandEvent(ctx context.Context, payload []byte) {
	slog.Debug("received command event", "payload_size", len(payload))

	// Parse the command request from the event payload
	var req CommandRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		slog.Error("failed to parse command event payload", "error", err)
		return
	}

	// No need to check server ID - subscription filtering ensures we only receive our commands
	slog.Info("processing command request",
		"trace_id", req.TraceID,
		"command", req.Command,
		"server_id", req.ServerID,
	)

	// Track command execution if drainer is available
	if h.drainer != nil && req.TraceID != "" {
		h.drainer.Start(req.TraceID)
		defer h.drainer.Complete(req.TraceID)
	}

	// Execute the command locally
	result := h.runner.Execute(req)

	// Ensure server ID is set
	if result.ServerID == "" {
		result.ServerID = h.serverID
	}

	// Report result back to control plane
	if h.resultSender != nil {
		if err := h.resultSender.ReportResult(ctx, result); err != nil {
			slog.Error("failed to report command result",
				"trace_id", req.TraceID,
				"error", err,
			)
		}
	} else {
		slog.Warn("no result sender configured, command result not reported",
			"trace_id", req.TraceID,
		)
	}
}

// UpdateSettings updates the runner's command settings
func (h *CommandHandler) UpdateSettings(settings CommandSettings) {
	h.runner.SetSettings(settings)
}

// GetSettings returns the current command settings
func (h *CommandHandler) GetSettings() CommandSettings {
	return h.runner.GetSettings()
}

// ExecuteLocal executes a command locally and returns the result directly
// This is used for local HTTP API calls, not event-driven execution
func (h *CommandHandler) ExecuteLocal(req CommandRequest) CommandResult {
	result := h.runner.Execute(req)
	if result.ServerID == "" {
		result.ServerID = h.serverID
	}
	return result
}
