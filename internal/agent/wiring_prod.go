//go:build !dev

package agent

import (
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/devmode"
)

// devMMAPath is a no-op stub in production.
func devMMAPath(dc *devmode.DevConfig) (string, bool) {
	return "", false
}

// devMMAEnv is a no-op stub in production.
func devMMAEnv(dc *devmode.DevConfig, baseEnv map[string]string) map[string]string {
	return baseEnv
}

// devDeploymentEnginePath is a no-op stub in production.
func devDeploymentEnginePath(dc *devmode.DevConfig) (string, bool) {
	return "", false
}

// devResolveProvider is a no-op stub in production.
func devResolveProvider(dc *devmode.DevConfig, providerID string) *devmode.UMCOverride {
	return nil
}

// devCheckSkipSpine is a no-op stub in production.
func devCheckSkipSpine(dc *devmode.DevConfig) bool {
	return false
}

// devCheckSkipUCRSSync is a no-op stub in production.
func devCheckSkipUCRSSync(dc *devmode.DevConfig) bool {
	return false
}

// devCheckSkipMTLS is a no-op stub in production.
func devCheckSkipMTLS(dc *devmode.DevConfig) bool {
	return false
}

// devCheckCapabilityRegistryEnabled is a no-op stub in production.
func devCheckCapabilityRegistryEnabled(dc *devmode.DevConfig) *bool {
	return nil
}

// devProviderBinaryPath is a no-op stub in production.
func devProviderBinaryPath(dc *devmode.DevConfig, providerID string) (string, error) {
	return "", fmt.Errorf("dev mode not available in production build")
}
