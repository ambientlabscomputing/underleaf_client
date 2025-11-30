package exec

import (
	"time"
)

// CommandRequest represents a command to be executed on the server
// This is received via the event bus from the control plane
type CommandRequest struct {
	ServerID   string            `json:"server_id"`   // Target server ID (for filtering)
	TraceID    string            `json:"trace_id"`    // Unique job ID for tracking
	Command    string            `json:"command"`     // The command to execute
	Args       []string          `json:"args"`        // Command arguments
	Env        map[string]string `json:"env"`         // Environment variables to set
	WorkingDir string            `json:"working_dir"` // Working directory (optional)
	Timeout    int               `json:"timeout"`     // Timeout in seconds (0 = use default)
	User       string            `json:"user"`        // User to run as (optional, requires privilege)
}

// CommandResult represents the result of a command execution
// This is sent back to the control plane after execution
type CommandResult struct {
	TraceID   string    `json:"trace_id"`  // Job ID from the original request
	ServerID  string    `json:"server_id"` // This server's unique identifier
	ExitCode  int       `json:"exit_code"` // Command exit code (0 = success)
	Stdout    string    `json:"stdout"`    // Standard output from command
	Stderr    string    `json:"stderr"`    // Standard error from command
	Timestamp time.Time `json:"timestamp"` // When the command completed
	Error     string    `json:"error"`     // Error message if execution failed
	Duration  int64     `json:"duration"`  // Execution duration in milliseconds
}

// CommandSettings represents the command execution settings from server config
// These are synced from the control plane and stored in the config snapshot
type CommandSettings struct {
	AllowLiteralCommands bool     `json:"allow_literal_commands"` // Allow arbitrary commands
	Whitelist            []string `json:"whitelist"`              // Allowed command prefixes
	Blacklist            []string `json:"blacklist"`              // Blocked command patterns
	DefaultTimeout       int      `json:"default_timeout"`        // Default timeout in seconds
}

// DefaultCommandSettings returns safe default settings
func DefaultCommandSettings() CommandSettings {
	return CommandSettings{
		AllowLiteralCommands: false,
		Whitelist:            []string{},
		Blacklist:            []string{"rm -rf", "dd if=", "mkfs", ":(){:|:&};:"},
		DefaultTimeout:       300, // 5 minutes
	}
}

// Runner defines the interface for command execution
type Runner interface {
	// Execute runs a command and returns the result
	Execute(req CommandRequest) CommandResult

	// SetSettings updates the command settings (whitelist/blacklist)
	SetSettings(settings CommandSettings)

	// GetSettings returns current command settings
	GetSettings() CommandSettings
}

// Errors
type ExecError struct {
	Code    string
	Message string
}

func (e *ExecError) Error() string {
	return e.Message
}

var (
	ErrCommandBlocked    = &ExecError{Code: "command_blocked", Message: "command is blocked by blacklist"}
	ErrCommandNotAllowed = &ExecError{Code: "command_not_allowed", Message: "command is not in whitelist"}
	ErrTimeout           = &ExecError{Code: "timeout", Message: "command execution timed out"}
	ErrPermissionDenied  = &ExecError{Code: "permission_denied", Message: "permission denied to execute command"}
	ErrCommandNotFound   = &ExecError{Code: "command_not_found", Message: "command not found"}
)
