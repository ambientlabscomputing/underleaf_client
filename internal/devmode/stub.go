//go:build !dev

package devmode

import (
	"fmt"
	"time"
)

// DevConfig is a no-op stub in production builds.
type DevConfig struct{}

// LoadBuildConfig returns an error in production builds.
func LoadBuildConfig(path string) (*DevConfig, error) {
	return nil, fmt.Errorf("dev mode not available in production build")
}

// ResolveOverride returns nil in production.
func (c *DevConfig) ResolveOverride(name string) *UMCOverride {
	return nil
}

// ResolveBinary returns an error in production.
func (c *DevConfig) ResolveBinary(name string) (string, error) {
	return "", fmt.Errorf("dev mode not available in production build")
}

// EffectiveEnv returns only the base environment in production.
func (c *DevConfig) EffectiveEnv(name string, baseEnv map[string]string) map[string]string {
	return baseEnv
}

// ShouldWatch returns false in production.
func (c *DevConfig) ShouldWatch(name string) bool {
	return false
}

// GetHealthEndpoint returns empty string in production.
func (c *DevConfig) GetHealthEndpoint(name string) string {
	return ""
}

// GetHealthTimeout returns default timeout in production.
func (c *DevConfig) GetHealthTimeout(name string) time.Duration {
	return 5 * time.Second
}

// GetArgs returns empty slice in production.
func (c *DevConfig) GetArgs(name string) []string {
	return []string{}
}

// BasePath returns empty string in production.
func (c *DevConfig) BasePath() string {
	return ""
}

// OverrideNames returns empty slice in production.
func (c *DevConfig) OverrideNames() []string {
	return []string{}
}

// SummaryString returns empty string in production.
func (c *DevConfig) SummaryString() string {
	return ""
}

// Validate returns nil in production (no-op).
func (c *DevConfig) Validate() error {
	return nil
}

// UMCOverride is a stub type in production.
type UMCOverride struct{}

// Duration is a stub type in production.
type Duration time.Duration
