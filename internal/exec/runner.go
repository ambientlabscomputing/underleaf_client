package exec

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// LocalRunner executes commands on the local system
type LocalRunner struct {
	serverID string
	settings CommandSettings
	mu       sync.RWMutex
}

// NewLocalRunner creates a new local command runner
func NewLocalRunner(serverID string, settings CommandSettings) *LocalRunner {
	return &LocalRunner{
		serverID: serverID,
		settings: settings,
	}
}

// Execute runs a command and returns the result
func (r *LocalRunner) Execute(req CommandRequest) CommandResult {
	start := time.Now()
	result := CommandResult{
		TraceID:  req.TraceID,
		ServerID: r.serverID,
	}

	slog.Info("executing command",
		"trace_id", req.TraceID,
		"command", req.Command,
		"args", req.Args,
	)

	// Validate command against whitelist/blacklist
	if err := r.validateCommand(req.Command, req.Args); err != nil {
		result.Error = err.Error()
		result.ExitCode = 126 // Command cannot execute
		result.Timestamp = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		slog.Warn("command blocked", "trace_id", req.TraceID, "error", err)
		return result
	}

	// Determine timeout
	timeout := req.Timeout
	if timeout <= 0 {
		r.mu.RLock()
		timeout = r.settings.DefaultTimeout
		r.mu.RUnlock()
	}
	if timeout <= 0 {
		timeout = 300 // fallback to 5 minutes
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	// Build command
	var cmd *exec.Cmd
	if len(req.Args) > 0 {
		cmd = exec.CommandContext(ctx, req.Command, req.Args...)
	} else {
		// If no args, treat command as a shell command
		cmd = exec.CommandContext(ctx, "sh", "-c", req.Command)
	}

	// Configure platform-specific process attributes
	configureCommand(cmd)

	// Set working directory
	if req.WorkingDir != "" {
		cmd.Dir = req.WorkingDir
	}

	// Set environment variables
	if len(req.Env) > 0 {
		cmd.Env = os.Environ() // Start with current environment
		for k, v := range req.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	// Capture stdout and stderr
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Start the command
	if err := cmd.Start(); err != nil {
		result.Error = err.Error()
		result.ExitCode = 1
		result.Timestamp = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		slog.Warn("command failed to start", "trace_id", req.TraceID, "error", err)
		return result
	}

	// Wait for command to complete or context to timeout
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		// Context timeout - kill the process (and process group on Unix)
		killCommand(cmd)
		<-done // Wait for Wait() to finish
		result.Error = ErrTimeout.Message
		result.ExitCode = 124 // Standard timeout exit code
		result.Stdout = stdout.String()
		result.Stderr = stderr.String()
		result.Timestamp = time.Now()
		result.Duration = time.Since(start).Milliseconds()
		slog.Warn("command timed out", "trace_id", req.TraceID, "timeout", timeout)
		return result
	case err := <-done:
		// Command completed
		result.Stdout = stdout.String()
		result.Stderr = stderr.String()
		result.Timestamp = time.Now()
		result.Duration = time.Since(start).Milliseconds()

		if err != nil {
			// Get exit code from error
			if exitError, ok := err.(*exec.ExitError); ok {
				result.ExitCode = exitError.ExitCode()
			} else {
				result.ExitCode = 1
				result.Error = err.Error()
			}

			slog.Info("command failed",
				"trace_id", req.TraceID,
				"exit_code", result.ExitCode,
				"error", err,
			)
		} else {
			result.ExitCode = 0
			slog.Info("command completed successfully",
				"trace_id", req.TraceID,
				"duration_ms", result.Duration,
			)
		}

		return result
	}
}

// SetSettings updates the command settings
func (r *LocalRunner) SetSettings(settings CommandSettings) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.settings = settings
	slog.Info("command settings updated",
		"whitelist_count", len(settings.Whitelist),
		"blacklist_count", len(settings.Blacklist),
		"allow_literal", settings.AllowLiteralCommands,
	)
}

// GetSettings returns current command settings
func (r *LocalRunner) GetSettings() CommandSettings {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.settings
}

// validateCommand checks if a command is allowed to execute
func (r *LocalRunner) validateCommand(command string, args []string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Build full command string for pattern matching
	fullCommand := command
	if len(args) > 0 {
		fullCommand = command + " " + strings.Join(args, " ")
	}

	// Check blacklist first (highest priority)
	// If blacklist is set, whitelist must be empty (mutual exclusivity)
	if len(r.settings.Blacklist) > 0 {
		for _, pattern := range r.settings.Blacklist {
			if strings.Contains(fullCommand, pattern) {
				return fmt.Errorf("%s: matches blacklist pattern '%s'", ErrCommandBlocked.Message, pattern)
			}
		}
		// Blacklist is set but command not blocked, allow it
		return nil
	}

	// Check whitelist (only if blacklist is empty)
	// If whitelist is empty, allow all commands
	if len(r.settings.Whitelist) == 0 {
		return nil
	}

	// Whitelist is set, check if command matches
	for _, pattern := range r.settings.Whitelist {
		if strings.HasPrefix(command, pattern) {
			return nil
		}
	}

	return fmt.Errorf("%s: command '%s' not in whitelist", ErrCommandNotAllowed.Message, command)
}
