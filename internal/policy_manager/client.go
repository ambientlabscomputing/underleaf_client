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

// NewCLIConfigClient creates a new CLIConfigClient instance
// use viper for simple config management
func NewCLIConfigClient() *CLIConfigClient {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath("./")
	v.AddConfigPath("$HOME/.underleaf")
	v.AutomaticEnv()

	var configPath string

	// for CLI, start a new empty config if no config file found
	err := v.ReadInConfig()
	if err != nil {
		// Set build-time defaults if not already configured
		v.Set("api.base_url", defaults.APIBaseURL)
		v.Set("mycelium_spine.endpoint", defaults.SpineEndpoint)

		// Determine canonical config path from existing config or create in ~/.underleaf
		if existingPath := v.ConfigFileUsed(); existingPath != "" {
			configPath = existingPath
		} else {
			// Check if config exists in ~/.underleaf
			homeConfigPath := GetConfigPath(false) // false = not agent
			if _, err := os.Stat(homeConfigPath); err == nil {
				configPath = homeConfigPath
			} else {
				// Create in ~/.underleaf as canonical location
				configPath = homeConfigPath
				if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
					slog.Warn("failed to create config directory, using current dir", "error", err)
					configPath = "./config.yaml"
				}
			}
		}

		// Store the canonical path in the config itself
		v.Set("local.config_path", configPath)

		// Create the config file at canonical location
		if err := v.SafeWriteConfigAs(configPath); err != nil {
			slog.Debug("failed to write config file", "error", err)
			configPath = "./config.yaml" // Fallback
		}
	} else {
		// Config exists, get its path
		configPath = v.ConfigFileUsed()

		// Check if canonical path is stored in config
		if storedPath, ok := v.Get("local.config_path").(string); ok && storedPath != "" {
			// Verify the stored path matches current path
			if storedPath != configPath {
				slog.Warn("config path mismatch, using stored canonical path",
					"stored", storedPath,
					"current", configPath)
				configPath = storedPath
			}
		} else {
			// Store canonical path for future use
			v.Set("local.config_path", configPath)
			v.WriteConfig()
		}
	}

	// Explicitly set the config file so WriteConfig() knows where to write
	v.SetConfigFile(configPath)

	return &CLIConfigClient{
		viper:      v,
		configPath: configPath,
	}
}

// Get retrieves a configuration value by key
func (c *CLIConfigClient) Get(key string) (interface{}, bool) {
	if !c.viper.IsSet(key) {
		return nil, false
	}
	return c.viper.Get(key), true
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
