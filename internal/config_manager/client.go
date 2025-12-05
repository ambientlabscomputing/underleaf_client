package config_manager

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/ambientlabscomputing/underleaf_client/pkg/defaults"
	"github.com/spf13/viper"
)

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
	snapshotClient, err := NewSnapshotConfigClient(ctx, serverID.(string), basePath, isAgent)
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
	viper *viper.Viper
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

	// for CLI, start a new empty config if no config file found
	_ = v.ReadInConfig() // ignore error and start with empty config
	
	// Set build-time defaults if not already configured
	if !v.IsSet("api.base_url") {
		v.Set("api.base_url", defaults.APIBaseURL)
	}
	if !v.IsSet("event_bus.endpoint") {
		v.Set("event_bus.endpoint", defaults.EventBusEndpoint)
	}
	
	if err := v.WriteConfigAs("./config.yaml"); err != nil {
		slog.Error("failed to write config file")
	}
	return &CLIConfigClient{
		viper: v,
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

	// Try WriteConfig first (writes to existing file)
	err := c.viper.WriteConfig()
	if err != nil {
		// If WriteConfig fails (e.g., no config file set), try SafeWriteConfig
		slog.Debug("WriteConfig failed, trying SafeWriteConfig", "error", err)
		err = c.viper.SafeWriteConfigAs("./config.yaml")
		if err != nil {
			// If SafeWriteConfig also fails (file exists), use WriteConfigAs to overwrite
			slog.Debug("SafeWriteConfig failed, using WriteConfigAs", "error", err)
			return c.viper.WriteConfigAs("./config.yaml")
		}
	}

	return nil
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
