package policy_manager

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ambientlabscomputing/underleaf_client/pkg/defaults"
	"github.com/spf13/viper"
)

// GetConfigPath returns the full path to the config file
func GetConfigPath(isAgent bool) string {
	basePath := GetBasePath(isAgent)
	return filepath.Join(basePath, "config.yaml")
}

const (
	ConfigClientTypeCLI   = "cli"
	ConfigClientTypeAgent = "agent"
)

type ConfigKey struct{}

type Configuration struct {
	Version int
	Payload map[string]interface{}
}

type ConfigClient interface {
	Get(key string) (interface{}, bool)
	Set(key string, value interface{}) error
	Delete(key string) error
	Config() Configuration
	ConfigClientInfo() map[string]interface{}
}

func NewConfigClient(configType string) ConfigClient {
	switch configType {
	case ConfigClientTypeCLI:
		return NewCLIConfigClient()
	case ConfigClientTypeAgent:
		// For agent, try to use snapshot client if available
		// Fall back to CLI client if not
		return NewCLIConfigClient() // Will be replaced when manager is running
	default:
		return NewDefaultConfigClient()
	}
}

// NewConfigClientWithSnapshot creates a config client that uses snapshot manager
// when token is available, falls back to simple viper otherwise
func NewConfigClientWithSnapshot(ctx context.Context, isAgent bool) ConfigClient {
	// Try simple CLI client first to check if we have token
	cliClient := NewCLIConfigClient()

	if token, ok := cliClient.Get("auth.token"); !ok || token == "" {
		// No token, use simple CLI client
		slog.Debug("no auth token found, using simple config client")
		return cliClient
	}

	serverID, ok := cliClient.Get("server.id")
	if !ok || serverID == "" {
		slog.Debug("no server ID found, using simple config client")
		return cliClient
	}

	// We have token and server ID, try to use snapshot client
	basePath := GetBasePath(isAgent)
	snapshotClient, err := NewSnapshotPolicyClient(ctx, serverID.(string), basePath, isAgent)
	if err != nil {
		slog.Warn("failed to create snapshot client, falling back to simple client", "error", err)
		return cliClient
	}

	slog.Info("using snapshot-based config client", "server_id", serverID)
	return snapshotClient
}

func NewDefaultConfigClient() ConfigClient {
	return NewCLIConfigClient()
}

// NewCLIConfigClientFromPath creates a CLIConfigClient backed by a specific config
// file path. The directory must already exist. Used primarily for testing and
// special bootstrapping scenarios.
func NewCLIConfigClientFromPath(configPath string) *CLIConfigClient {
	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")
	// Best-effort read; start with an empty config if the file doesn't exist yet.
	_ = v.ReadInConfig()
	v.Set("local.config_path", configPath)
	return &CLIConfigClient{
		viper:      v,
		configPath: configPath,
	}
}

func (c *CLIConfigClient) ConfigClientInfo() map[string]interface{} {
	configPath := c.viper.ConfigFileUsed()
	if configPath == "" {
		configPath = "in-memory (no config file)"
	}
	return map[string]interface{}{
		"type": "CLIConfigManager",
		"path": configPath,
	}
}

func NewConfigClientInCtx(ctx context.Context, configType string) (context.Context, ConfigClient) {
	client := NewConfigClient(configType)
	ctx = context.WithValue(ctx, ConfigKey{}, client)
	return ctx, client
}

func GetConfig(ctx context.Context) ConfigClient {
	if client, ok := ctx.Value(ConfigKey{}).(ConfigClient); ok {
		return client
	}
	return NewDefaultConfigClient()
}

// CLIConfigClient is simple and meant for temporary or CLI use cases
type CLIConfigClient struct {
	viper      *viper.Viper
	configPath string // Canonical path to config file
}

// NewCLIConfigClient creates a new CLIConfigClient instance.
// The config file is always read from the canonical ~/.underleaf/config.yaml
// path (via GetConfigPath) so that stray config.yaml files in the working
// directory (e.g. service configs in hyphae/, server_api/) are never
// accidentally loaded.
func NewCLIConfigClient() *CLIConfigClient {
	configPath := GetConfigPath(false) // always ~/.underleaf/config.yaml

	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")
	v.AutomaticEnv()

	err := v.ReadInConfig()
	if err != nil {
		// File doesn't exist yet — seed with build-time defaults and create it.
		v.Set("api.base_url", defaults.APIBaseURL)
		v.Set("mycelium_spine.endpoint", defaults.SpineEndpoint)
		v.Set("local.config_path", configPath)

		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			slog.Warn("failed to create config directory", "error", err)
		}
		if err := v.SafeWriteConfigAs(configPath); err != nil {
			slog.Debug("failed to write initial config file", "error", err)
		}
	} else {
		// File loaded — ensure local.config_path is recorded.
		if storedPath, ok := v.Get("local.config_path").(string); !ok || storedPath == "" {
			v.Set("local.config_path", configPath)
			_ = v.WriteConfig()
		}
		// In-memory defaults for critical keys that may be absent in a
		// partially-written config file (e.g. after a migration strips extras).
		// SetDefault does NOT write these to disk; they are read-only fallbacks.
		if !v.IsSet("api.base_url") || v.GetString("api.base_url") == "" {
			v.SetDefault("api.base_url", defaults.APIBaseURL)
		}
		if !v.IsSet("mycelium_spine.endpoint") || v.GetString("mycelium_spine.endpoint") == "" {
			v.SetDefault("mycelium_spine.endpoint", defaults.SpineEndpoint)
		}
	}

	return &CLIConfigClient{
		viper:      v,
		configPath: configPath,
	}
}

// ConfigPath returns the canonical path to the config file this client manages.
func (c *CLIConfigClient) ConfigPath() string {
	return c.configPath
}

// Reload re-reads the config file from disk, picking up any changes made since
// the client was created (e.g. by the config migrator).
func (c *CLIConfigClient) Reload() error {
	return c.viper.ReadInConfig()
}

// Get retrieves a configuration value by key.
// Returns (value, true) if the key exists in the config file, was explicitly
// Set(), or has a registered default (via SetDefault). Returns (nil, false)
// only when the key is completely unknown.
func (c *CLIConfigClient) Get(key string) (interface{}, bool) {
	// viper.Get returns nil for completely unknown keys and the actual value
	// (including defaults registered with SetDefault) for known ones.
	val := c.viper.Get(key)
	if val == nil {
		return nil, false
	}
	return val, true
}

// Set sets a configuration value by key
func (c *CLIConfigClient) Set(key string, value interface{}) error {
	c.viper.Set(key, value)

	// Always write to canonical path
	err := c.viper.WriteConfigAs(c.configPath)
	if err != nil {
		slog.Debug("failed to write config", "path", c.configPath, "error", err)
		return err
	}

	return nil
}

// Delete removes a configuration value by key
func (c *CLIConfigClient) Delete(key string) error {
	// Get all settings
	allSettings := c.viper.AllSettings()

	// Delete the key from the map
	delete(allSettings, key)

	// Create new viper instance with updated settings
	v := viper.New()
	for k, val := range allSettings {
		v.Set(k, val)
	}

	// Write the updated config to canonical path
	c.viper = v
	c.viper.SetConfigFile(c.configPath)
	return c.viper.WriteConfigAs(c.configPath)
}

// Config returns the full configuration
func (c *CLIConfigClient) Config() Configuration {
	payloadBytes, err := json.Marshal(c.viper.AllSettings())
	if err != nil {
		slog.Error("failed to marshal config payload", "error", err)
	}
	var configPayload map[string]interface{}
	if err := json.Unmarshal(payloadBytes, &configPayload); err != nil {
		slog.Error("failed to unmarshal config payload", "error", err)
	}
	return Configuration{
		Version: 0, // viper configs are not versioned in this simple client
		Payload: configPayload,
	}
}
