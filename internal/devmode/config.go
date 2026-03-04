//go:build dev

package devmode

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a custom type for YAML duration parsing (e.g., "10s", "1m").
type Duration time.Duration

// UnmarshalYAML parses YAML duration strings.
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return errors.New("duration must be a scalar")
	}
	dur, err := time.ParseDuration(node.Value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", node.Value, err)
	}
	*d = Duration(dur)
	return nil
}

// Duration returns the time.Duration value.
func (d Duration) Time() time.Duration {
	return time.Duration(d)
}

// DevConfig is the root development mode configuration.
type DevConfig struct {
	Version   string                 `yaml:"version"`
	Defaults  DevDefaults            `yaml:"defaults,omitempty"`
	Overrides map[string]UMCOverride `yaml:"overrides,omitempty"`
	Agent     AgentDevSettings       `yaml:"agent,omitempty"`
	basePath  string                 // resolved directory of build.yaml
}

// DevDefaults provides global defaults for all UMC overrides.
type DevDefaults struct {
	Env                map[string]string `yaml:"env,omitempty"`
	Watch              bool              `yaml:"watch"`
	SkipSignatureCheck bool              `yaml:"skip_signature_check"`
}

// UMCOverride specifies a local override for a single UMC.
type UMCOverride struct {
	Binary         string            `yaml:"binary"`
	Args           []string          `yaml:"args,omitempty"`
	Env            map[string]string `yaml:"env,omitempty"`
	Watch          *bool             `yaml:"watch,omitempty"` // nil = use default
	HealthEndpoint string            `yaml:"health_endpoint,omitempty"`
	HealthTimeout  Duration          `yaml:"health_timeout,omitempty"`
}

// AgentDevSettings provides agent-level development overrides.
type AgentDevSettings struct {
	SkipMTLS           bool              `yaml:"skip_mtls"`
	SkipSpine          bool              `yaml:"skip_spine"`
	SkipUCRSSync       bool              `yaml:"skip_ucrs_sync"`
	CapabilityRegistry CapRegDevSettings `yaml:"capability_registry,omitempty"`
}

// CapRegDevSettings controls capability registry behavior in dev mode.
type CapRegDevSettings struct {
	Enabled *bool `yaml:"enabled"` // nil = default behavior
}

// LoadBuildConfig loads and parses a build.yaml file.
// All relative paths in the configuration are resolved against the directory
// containing the build.yaml file.
func LoadBuildConfig(path string) (*DevConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read build config: %w", err)
	}

	var cfg DevConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse build config: %w", err)
	}

	// Validate version
	if cfg.Version != "1" {
		return nil, fmt.Errorf("unsupported build.yaml version: %q (expected \"1\")", cfg.Version)
	}

	// Store the base path for relative path resolution
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve build config path: %w", err)
	}
	cfg.basePath = filepath.Dir(absPath)

	// Initialize empty maps if nil
	if cfg.Defaults.Env == nil {
		cfg.Defaults.Env = make(map[string]string)
	}
	if cfg.Overrides == nil {
		cfg.Overrides = make(map[string]UMCOverride)
	}

	return &cfg, nil
}

// ResolveOverride returns the override for a given UMC name or provider ID, or nil if not found.
func (c *DevConfig) ResolveOverride(name string) *UMCOverride {
	if c == nil || c.Overrides == nil {
		return nil
	}
	if override, ok := c.Overrides[name]; ok {
		return &override
	}
	return nil
}

// ResolveBinary returns the absolute path to the UMC binary, resolving relative paths
// against the build.yaml directory. Returns an error if the resolved path does not exist or is not executable.
func (c *DevConfig) ResolveBinary(name string) (string, error) {
	override := c.ResolveOverride(name)
	if override == nil {
		return "", fmt.Errorf("no override found for %q", name)
	}

	binaryPath := override.Binary
	if !filepath.IsAbs(binaryPath) {
		// Relative path: resolve against build.yaml directory
		binaryPath = filepath.Join(c.basePath, binaryPath)
	}

	// Resolve symlinks and clean up
	absPath, err := filepath.Abs(binaryPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve binary path for %q: %w", name, err)
	}

	// Check existence
	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("binary not found for %q at %s: %w", name, absPath, err)
	}

	// Check if it's a regular file
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("binary for %q is not a regular file: %s", name, absPath)
	}

	// Check if it's executable
	if info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("binary for %q is not executable: %s (mode: %o)", name, absPath, info.Mode())
	}

	return absPath, nil
}

// EffectiveEnv merges defaults, override-specific, and base environment variables.
// Base environment variables take precedence, allowing them to override dev settings.
func (c *DevConfig) EffectiveEnv(name string, baseEnv map[string]string) map[string]string {
	result := make(map[string]string)

	// Start with defaults
	for k, v := range c.Defaults.Env {
		result[k] = v
	}

	// Merge override-specific env
	override := c.ResolveOverride(name)
	if override != nil {
		for k, v := range override.Env {
			result[k] = v
		}
	}

	// Merge base env (takes precedence)
	for k, v := range baseEnv {
		result[k] = v
	}

	return result
}

// ShouldWatch returns whether file-watching is enabled for a UMC.
// If the override specifies a Watch value, that is used; otherwise the default is used.
func (c *DevConfig) ShouldWatch(name string) bool {
	if c == nil {
		return false
	}
	override := c.ResolveOverride(name)
	if override != nil && override.Watch != nil {
		return *override.Watch
	}
	return c.Defaults.Watch
}

// GetHealthEndpoint returns the health endpoint for a UMC override, or an empty string if not specified.
func (c *DevConfig) GetHealthEndpoint(name string) string {
	override := c.ResolveOverride(name)
	if override != nil {
		return override.HealthEndpoint
	}
	return ""
}

// GetHealthTimeout returns the health check timeout for a UMC override.
// If not specified, returns a sensible default (5s).
func (c *DevConfig) GetHealthTimeout(name string) time.Duration {
	override := c.ResolveOverride(name)
	if override != nil && override.HealthTimeout > 0 {
		return override.HealthTimeout.Time()
	}
	return 5 * time.Second
}

// GetArgs returns the CLI arguments to pass to a UMC, or an empty slice if not specified.
func (c *DevConfig) GetArgs(name string) []string {
	override := c.ResolveOverride(name)
	if override != nil && len(override.Args) > 0 {
		return override.Args
	}
	return []string{}
}

// BasePath returns the directory containing the loaded build.yaml file.
func (c *DevConfig) BasePath() string {
	return c.basePath
}

// OverrideNames returns a sorted list of all override names in the configuration.
func (c *DevConfig) OverrideNames() []string {
	if c == nil || c.Overrides == nil {
		return []string{}
	}
	names := make([]string, 0, len(c.Overrides))
	for name := range c.Overrides {
		names = append(names, name)
	}
	// Simple sort (not performance-critical)
	for i := 0; i < len(names)-1; i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	return names
}

// SummaryString returns a human-readable summary of the dev config.
func (c *DevConfig) SummaryString() string {
	var sb strings.Builder
	sb.WriteString("Dev Mode Configuration:\n")
	sb.WriteString(fmt.Sprintf("  Base Path: %s\n", c.BasePath()))
	sb.WriteString(fmt.Sprintf("  Overrides: %d UMCs\n", len(c.Overrides)))
	for _, name := range c.OverrideNames() {
		override := c.ResolveOverride(name)
		sb.WriteString(fmt.Sprintf("    - %s: %s (watch: %v)\n", name, override.Binary, c.ShouldWatch(name)))
	}
	if c.Agent.SkipSpine {
		sb.WriteString("  Agent: Skip Spine connection\n")
	}
	if c.Agent.SkipMTLS {
		sb.WriteString("  Agent: Skip mTLS\n")
	}
	if c.Agent.SkipUCRSSync {
		sb.WriteString("  Agent: Skip UCRS sync\n")
	}
	if c.Agent.CapabilityRegistry.Enabled != nil && !*c.Agent.CapabilityRegistry.Enabled {
		sb.WriteString("  Agent: Disable capability registry\n")
	}
	return sb.String()
}
