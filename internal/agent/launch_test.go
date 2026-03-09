package agent

import (
	"testing"
	"time"
)

// TestNewLauncher_HealthTimeoutDefaults verifies that NewLauncher applies the
// 120s default when HealthTimeout is not set (zero value), and preserves any
// explicitly provided value — including negative values used as the "disabled"
// sentinel by CLI commands.
func TestNewLauncher_HealthTimeoutDefaults(t *testing.T) {
	tests := []struct {
		name            string
		configTimeout   time.Duration
		expectedTimeout time.Duration
	}{
		{
			name:            "zero defaults to 120s",
			configTimeout:   0,
			expectedTimeout: 120 * time.Second,
		},
		{
			name:            "explicit 5m is preserved",
			configTimeout:   5 * time.Minute,
			expectedTimeout: 5 * time.Minute,
		},
		{
			name:            "explicit 30s is preserved",
			configTimeout:   30 * time.Second,
			expectedTimeout: 30 * time.Second,
		},
		{
			name:            "negative sentinel (disabled) is preserved",
			configTimeout:   -1 * time.Nanosecond,
			expectedTimeout: -1 * time.Nanosecond,
		},
		{
			name:            "large timeout is preserved",
			configTimeout:   10 * time.Minute,
			expectedTimeout: 10 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := NewLauncher(LauncherConfig{
				Mode:          ModeDaemon,
				Port:          9999,
				HealthTimeout: tt.configTimeout,
			})
			if l.healthTimeout != tt.expectedTimeout {
				t.Errorf("healthTimeout = %v, want %v", l.healthTimeout, tt.expectedTimeout)
			}
		})
	}
}

// TestNewLauncher_OtherDefaults verifies that unset Port and file paths get
// sensible defaults.
func TestNewLauncher_OtherDefaults(t *testing.T) {
	l := NewLauncher(LauncherConfig{Mode: ModeDev})

	if l.port != 8080 {
		t.Errorf("default port = %d, want 8080", l.port)
	}
	if l.pidFile == "" {
		t.Error("pidFile should not be empty")
	}
	if l.logFile == "" {
		t.Error("logFile should not be empty")
	}
}

// TestNewLauncher_ExplicitPort verifies that an explicitly set port is not
// overridden by the default.
func TestNewLauncher_ExplicitPort(t *testing.T) {
	l := NewLauncher(LauncherConfig{Mode: ModeDaemon, Port: 7777})
	if l.port != 7777 {
		t.Errorf("port = %d, want 7777", l.port)
	}
}

// TestHealthTimeoutDisabledSentinel documents and verifies the contract between
// the CLI layer and the launcher: passing --health-timeout 0 on the CLI maps to
// -1*time.Nanosecond in LauncherConfig so that NewLauncher's zero-default does
// not override the user's intent to disable health checking.
func TestHealthTimeoutDisabledSentinel(t *testing.T) {
	// Simulate what start.go and agent.go do when the user passes --health-timeout 0
	cliValue := time.Duration(0) // cobra returns 0 for --health-timeout 0

	launcherHealthTimeout := cliValue
	if cliValue == 0 {
		launcherHealthTimeout = -1 * time.Nanosecond
	}

	l := NewLauncher(LauncherConfig{
		Mode:          ModeDaemon,
		Port:          8080,
		HealthTimeout: launcherHealthTimeout,
	})

	// The launcher must store a negative value so startDaemon skips the poll loop.
	if l.healthTimeout >= 0 {
		t.Errorf("expected negative healthTimeout (disabled sentinel), got %v", l.healthTimeout)
	}
}
