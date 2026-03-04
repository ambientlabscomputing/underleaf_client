//go:build dev

package devmode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBuildConfig(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name:    "valid minimal config",
			content: `version: "1"`,
			wantErr: false,
		},
		{
			name:    "invalid version",
			content: `version: "2"`,
			wantErr: true,
		},
		{
			name:    "invalid yaml",
			content: `this: is: invalid: yaml`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "build.yaml")

			if err := os.WriteFile(configPath, []byte(tt.content), 0o644); err != nil {
				t.Fatalf("failed to write test config: %v", err)
			}

			cfg, err := LoadBuildConfig(configPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadBuildConfig() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err == nil && cfg.basePath != tmpDir {
				t.Errorf("basePath = %q, want %q", cfg.basePath, tmpDir)
			}
		})
	}
}

func TestResolveBinary(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "build.yaml")
	binaryPath := filepath.Join(tmpDir, "mock-serve")

	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\necho hello"), 0o755); err != nil {
		t.Fatalf("failed to create mock binary: %v", err)
	}

	configContent := `version: "1"
overrides:
  test-umc:
    binary: ./mock-serve`

	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := LoadBuildConfig(configPath)
	if err != nil {
		t.Fatalf("LoadBuildConfig() error = %v", err)
	}

	resolved, err := cfg.ResolveBinary("test-umc")
	if err != nil {
		t.Fatalf("ResolveBinary() error = %v", err)
	}

	if resolved != binaryPath {
		t.Errorf("ResolveBinary() = %q, want %q", resolved, binaryPath)
	}

	_, err = cfg.ResolveBinary("non-existent")
	if err == nil {
		t.Error("ResolveBinary(non-existent) expected error, got nil")
	}
}

func TestEffectiveEnv(t *testing.T) {
	cfg := &DevConfig{
		Defaults: DevDefaults{
			Env: map[string]string{
				"LOG_LEVEL": "debug",
				"PORT":      "8080",
			},
		},
		Overrides: map[string]UMCOverride{
			"test-umc": {
				Env: map[string]string{
					"PORT":   "9090",
					"CUSTOM": "value",
				},
			},
		},
	}

	baseEnv := map[string]string{
		"CUSTOM2": "value2",
		"PORT":    "7070",
	}

	result := cfg.EffectiveEnv("test-umc", baseEnv)

	if result["LOG_LEVEL"] != "debug" {
		t.Errorf("LOG_LEVEL = %q, want debug", result["LOG_LEVEL"])
	}

	if result["CUSTOM"] != "value" {
		t.Errorf("CUSTOM = %q, want value", result["CUSTOM"])
	}

	if result["CUSTOM2"] != "value2" {
		t.Errorf("CUSTOM2 = %q, want value2", result["CUSTOM2"])
	}

	if result["PORT"] != "7070" {
		t.Errorf("PORT = %q, want 7070 (from base)", result["PORT"])
	}
}

func TestNilConfig(t *testing.T) {
	var cfg *DevConfig

	if override := cfg.ResolveOverride("test"); override != nil {
		t.Error("nil config ResolveOverride returned non-nil")
	}

	if cfg.ShouldWatch("test") {
		t.Error("nil config ShouldWatch returned true")
	}

	names := cfg.OverrideNames()
	if len(names) != 0 {
		t.Errorf("nil config OverrideNames() returned %d items, want 0", len(names))
	}
}
