//go:build dev

package agent

import (
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/devmode"
)

// devMMAPath resolves the MMA binary path from dev config.
// Returns (path, true) if a dev override exists, or ("", false) if not.
func devMMAPath(dc *devmode.DevConfig) (string, bool) {
	if dc == nil {
		return "", false
	}
	path, err := dc.ResolveBinary("mma")
	if err != nil {
		return "", false
	}
	return path, true
}

// devMMAEnv returns the merged environment variables for MMA from dev config.
func devMMAEnv(dc *devmode.DevConfig, baseEnv map[string]string) map[string]string {
	if dc == nil {
		return baseEnv
	}
	return dc.EffectiveEnv("mma", baseEnv)
}

// devDeploymentEnginePath resolves the deployment engine binary path from dev config.
// Returns (path, true) if a dev override exists, or ("", false) if not.
func devDeploymentEnginePath(dc *devmode.DevConfig) (string, bool) {
	if dc == nil {
		return "", false
	}
	path, err := dc.ResolveBinary("deployment-engine")
	if err != nil {
		return "", false
	}
	return path, true
}

// devResolveProvider resolves a provider override from dev config.
// Returns the override if found, or nil if not.
func devResolveProvider(dc *devmode.DevConfig, providerID string) *devmode.UMCOverride {
	if dc == nil {
		return nil
	}
	return dc.ResolveOverride(providerID)
}

// devCheckSkipSpine returns true if Spine connection should be skipped in dev mode.
func devCheckSkipSpine(dc *devmode.DevConfig) bool {
	if dc == nil {
		return false
	}
	return dc.Agent.SkipSpine
}

// devCheckSkipUCRSSync returns true if UCRS sync should be skipped in dev mode.
func devCheckSkipUCRSSync(dc *devmode.DevConfig) bool {
	if dc == nil {
		return false
	}
	return dc.Agent.SkipUCRSSync
}

// devCheckSkipMTLS returns true if mTLS should be skipped in dev mode.
func devCheckSkipMTLS(dc *devmode.DevConfig) bool {
	if dc == nil {
		return false
	}
	return dc.Agent.SkipMTLS
}

// devCheckCapabilityRegistryEnabled returns whether the capability registry should be enabled.
// Returns nil if the config doesn't specify (use default), or the specified boolean value.
func devCheckCapabilityRegistryEnabled(dc *devmode.DevConfig) *bool {
	if dc == nil {
		return nil
	}
	return dc.Agent.CapabilityRegistry.Enabled
}

// devProviderBinaryPath returns the resolved binary path for a provider override.
// Returns error if the path doesn't exist or isn't executable.
func devProviderBinaryPath(dc *devmode.DevConfig, providerID string) (string, error) {
	if dc == nil {
		return "", fmt.Errorf("dev config is nil")
	}
	return dc.ResolveBinary(providerID)
}
