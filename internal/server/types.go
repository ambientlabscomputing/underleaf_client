package server

import (
	"encoding/json"
	"log/slog"

	"github.com/ambientlabscomputing/underleaf_client/internal/config_manager"
)

const (
	ServerOSDarwin  = "darwin"
	ServerOSLinux   = "linux"
	ServerOSWindows = "windows"
	ServerOSUnknown = "unknown"

	ServerArchAMD64   = "amd64"
	ServerArchARM64   = "arm64"
	ServerArchUnknown = "unknown"
)

type ServerCommandSettings struct {
	AllowLiteralCommands bool `json:"allow_literal_commands"`
}

type ServerPlatform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

type ConfigurationPayload struct {
	Commands ServerCommandSettings `json:"commands"`
	Platform ServerPlatform        `json:"platform"`
}

type Configuration struct {
	Version int
	Payload ConfigurationPayload
}

func (c Configuration) FromConfigManagerConfig(cfg config_manager.Configuration) Configuration {
	pBytes, err := json.Marshal(cfg.Payload)
	if err != nil {
		slog.Error("failed to marshal configuration payload", "error", err)
		return Configuration{}
	}
	var payload ConfigurationPayload
	if err := json.Unmarshal(pBytes, &payload); err != nil {
		slog.Error("failed to unmarshal configuration payload", "error", err)
		return Configuration{}
	}
	return Configuration{
		Version: cfg.Version,
		Payload: payload,
	}
}

// Server represents a server
type Server struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Tags   map[string]string `json:"tags,omitempty"`
	Config Configuration     `json:"configuration"`
}
