package config_manager

import (
	"context"
	"log/slog"

	"github.com/spf13/viper"
)

const (
	ConfigClientTypeCLI   = "cli"
	ConfigClientTypeAgent = "agent"
)

type ConfigKey struct{}

type Configuration struct {
	Version    string
	ClientType string
	Payload    map[string]interface{}
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
	default:
		return NewDefaultConfigClient()
	}
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
	if err := v.ReadInConfig(); err != nil {
		// ignore error and start with empty config
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
	return c.viper.WriteConfig()
}

// Config returns the full configuration
func (c *CLIConfigClient) Config() Configuration {
	return Configuration{
		Version:    "unversioned", // viper configs are not  versioned in this simple client
		ClientType: ConfigClientTypeCLI,
		Payload:    c.viper.AllSettings(),
	}
}
