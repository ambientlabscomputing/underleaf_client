//go:build dev

package devmode

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a mock binary
	binaryPath := filepath.Join(tmpDir, "mock-serve")
	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\necho hello"), 0o755); err != nil {
		t.Fatalf("failed to create mock binary: %v", err)
	}

	tests := []struct {
		name    string
		cfg     *DevConfig
		wantErr bool
	}{
		{
			name:    "nil config",
			cfg:     nil,
			wantErr: false,
		},
		{
			name: "valid config with existing binary",
			cfg: &DevConfig{
				Overrides: map[string]UMCOverride{
					"test": {Binary: binaryPath},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid: binary does not exist",
			cfg: &DevConfig{
				Overrides: map[string]UMCOverride{
					"test": {Binary: filepath.Join(tmpDir, "nonexistent")},
				},
			},
			wantErr: true,
		},
		{
			name: "warning: port conflict",
			cfg: &DevConfig{
				Overrides: map[string]UMCOverride{
					"umc1": {HealthEndpoint: "http://localhost:8080/health"},
					"umc2": {HealthEndpoint: "http://localhost:8080/health"},
				},
			},
			wantErr: true, // Warnings are included as errors
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestExtractPort(t *testing.T) {
	tests := []struct {
		endpoint string
		want     string
	}{
		{"http://localhost:10081/health", "10081"},
		{"http://localhost:8080", "8080"},
		{"http://localhost:443", "443"},
		{"https://example.com:9090", "9090"},
		{"localhost:5000", "5000"},
		{"http://localhost/health", ""},
		{"invalid", ""},
		{"", ""},
		{"http://localhost:", ""},
	}

	for _, tt := range tests {
		t.Run(tt.endpoint, func(t *testing.T) {
			got := extractPort(tt.endpoint)
			if got != tt.want {
				t.Errorf("extractPort(%q) = %q, want %q", tt.endpoint, got, tt.want)
			}
		})
	}
}

func TestWatcher(t *testing.T) {
	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "test-binary")

	// Create initial binary
	if err := os.WriteFile(binaryPath, []byte("v1"), 0o755); err != nil {
		t.Fatalf("failed to create binary: %v", err)
	}

	logger := slog.Default()
	watcher := NewWatcher(100*time.Millisecond, logger) // Fast poll for testing

	// Track restarts
	restartCount := 0
	restartName := ""
	watcher.SetRestartCallback(func(name string, path string) error {
		restartCount++
		restartName = name
		return nil
	})

	if err := watcher.Watch("test-umc", binaryPath); err != nil {
		t.Fatalf("Watch() error = %v", err)
	}

	// Start watcher in background
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		watcher.Start(ctx)
		close(done)
	}()

	// Wait a bit to establish baseline
	time.Sleep(200 * time.Millisecond)

	// Modify the binary (change its content, which updates mtime)
	if err := os.WriteFile(binaryPath, []byte("v2"), 0o755); err != nil {
		t.Fatalf("failed to update binary: %v", err)
	}

	// Wait for watcher to detect the change
	time.Sleep(300 * time.Millisecond)

	// Check that restart was called
	if restartCount != 1 {
		t.Errorf("restartCount = %d, want 1", restartCount)
	}
	if restartName != "test-umc" {
		t.Errorf("restartName = %q, want \"test-umc\"", restartName)
	}

	// Modify again
	if err := os.WriteFile(binaryPath, []byte("v3"), 0o755); err != nil {
		t.Fatalf("failed to update binary again: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	if restartCount != 2 {
		t.Errorf("restartCount = %d, want 2", restartCount)
	}

	// Stop the watcher
	cancel()
	<-done
}

func TestWatcherWithNonexistentBinary(t *testing.T) {
	logger := slog.Default()
	watcher := NewWatcher(100*time.Millisecond, logger)

	// Try to watch a file that doesn't exist
	err := watcher.Watch("test", "/nonexistent/file")
	if err == nil {
		t.Error("Watch() on nonexistent file expected error, got nil")
	}
}
