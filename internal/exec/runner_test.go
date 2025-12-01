package exec

import (
	"strings"
	"testing"
	"time"
)

func TestLocalRunner_Execute_SimpleCommand(t *testing.T) {
	settings := DefaultCommandSettings()
	settings.AllowLiteralCommands = true
	runner := NewLocalRunner("test-server", settings)

	req := CommandRequest{
		Command: "echo test",
		TraceID: "test-123",
	}

	result := runner.Execute(req)

	if result.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %d (stderr: %s)", result.ExitCode, result.Stderr)
	}

	if !strings.Contains(result.Stdout, "test") {
		t.Errorf("Expected stdout to contain 'test', got: %s", result.Stdout)
	}

	if result.Error != "" {
		t.Errorf("Expected no error, got: %s", result.Error)
	}
}

func TestLocalRunner_Execute_WithTimeout(t *testing.T) {
	settings := DefaultCommandSettings()
	settings.AllowLiteralCommands = true
	runner := NewLocalRunner("test-server", settings)

	req := CommandRequest{
		Command: "sleep 5",
		Timeout: 1, // 1 second timeout
		TraceID: "test-timeout",
	}

	start := time.Now()
	result := runner.Execute(req)
	elapsed := time.Since(start)

	// Should timeout within ~1 second (allow some buffer)
	if elapsed > 3*time.Second {
		t.Errorf("Command took too long to timeout: %v", elapsed)
	}

	// Exit code should be non-zero (killed by timeout)
	if result.ExitCode == 0 {
		t.Error("Expected non-zero exit code for timeout")
	}
}

func TestLocalRunner_Execute_WithEnvironment(t *testing.T) {
	settings := DefaultCommandSettings()
	settings.AllowLiteralCommands = true
	runner := NewLocalRunner("test-server", settings)

	req := CommandRequest{
		Command: "echo $TEST_VAR",
		Env:     map[string]string{"TEST_VAR": "hello"},
		TraceID: "test-env",
	}

	result := runner.Execute(req)

	if result.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", result.ExitCode)
	}

	if !strings.Contains(result.Stdout, "hello") {
		t.Errorf("Expected stdout to contain 'hello', got: %s", result.Stdout)
	}
}

func TestLocalRunner_Execute_FailingCommand(t *testing.T) {
	settings := DefaultCommandSettings()
	settings.AllowLiteralCommands = true
	runner := NewLocalRunner("test-server", settings)

	req := CommandRequest{
		Command: "exit 42",
		TraceID: "test-fail",
	}

	result := runner.Execute(req)

	if result.ExitCode != 42 {
		t.Errorf("Expected exit code 42, got %d", result.ExitCode)
	}
}

func TestDefaultCommandSettings(t *testing.T) {
	settings := DefaultCommandSettings()

	if settings.AllowLiteralCommands {
		t.Error("Expected AllowLiteralCommands to be false by default")
	}

	if settings.DefaultTimeout != 300 {
		t.Errorf("Expected default timeout of 300, got %d", settings.DefaultTimeout)
	}

	if len(settings.Blacklist) == 0 {
		t.Error("Expected blacklist to have default dangerous commands")
	}
}

func TestLocalRunner_WithDifferentSettings(t *testing.T) {
	newSettings := CommandSettings{
		AllowLiteralCommands: true,
		DefaultTimeout:       600,
		Whitelist:            []string{"echo", "ls"},
		Blacklist:            []string{},
	}

	runner := NewLocalRunner("test-server", newSettings)

	// Test that custom timeout is used
	req := CommandRequest{
		Command: "echo hello",
		Timeout: 0, // Will use default
		TraceID: "test-settings",
	}

	result := runner.Execute(req)

	if result.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", result.ExitCode)
	}
}
