package keymanager

import (
	"fmt"
	"os"
	"runtime"
)

// New creates a new KeyManager with the given configuration
// It will attempt to use TPM 2.0 on Linux, and fall back to software
// implementation on macOS or if TPM is unavailable
func New(cfg *Config) (KeyManager, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// Determine which backend to use
	backendType := cfg.BackendType
	if backendType == "" {
		// Auto-detect based on platform
		backendType = detectBackend()
	}

	var (
		km  KeyManager
		err error
	)

	switch backendType {
	case BackendTPM:
		// Try TPM backend (Linux only)
		if runtime.GOOS != "linux" {
			if cfg.SoftwareFallback {
				return newSoftwareKeyManager(cfg)
			}
			return nil, fmt.Errorf("%w: TPM 2.0 only available on Linux", ErrBackendNotAvailable)
		}
		km, err = newTPMKeyManager(cfg)
		if err != nil {
			if cfg.SoftwareFallback {
				return newSoftwareKeyManager(cfg)
			}
			return nil, fmt.Errorf("TPM backend failed: %w", err)
		}
		return km, nil

	case BackendSoftware:
		return newSoftwareKeyManager(cfg)

	case BackendHSM:
		// HSM support can be added later
		return nil, fmt.Errorf("%w: HSM backend not yet implemented", ErrBackendNotAvailable)

	default:
		return nil, fmt.Errorf("unknown backend type: %s", backendType)
	}
}

// detectBackend automatically detects the best available backend
func detectBackend() BackendType {
	switch runtime.GOOS {
	case "linux":
		// Check if TPM is available
		if isTPMAvailable() {
			return BackendTPM
		}
		return BackendSoftware
	case "darwin":
		// macOS doesn't have TPM, use software backend
		return BackendSoftware
	default:
		return BackendSoftware
	}
}

// isTPMAvailable checks if a TPM device is available
func isTPMAvailable() bool {
	// Try common TPM device paths
	paths := []string{"/dev/tpmrm0", "/dev/tpm0"}
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}
