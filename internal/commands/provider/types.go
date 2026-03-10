package provider

import (
	"context"
	"time"
)

// Context key for agent port (must match cli/root.go)
const configAgentPortContextKey = "config-agent-port"

// ProviderInfo represents a provider instance
type ProviderInfo struct {
	ProviderID   string            `json:"provider_id"`
	Version      string            `json:"version"`
	State        ProviderState     `json:"state"`
	Capabilities []string          `json:"capabilities"`
	Endpoint     string            `json:"endpoint"`
	InstalledAt  time.Time         `json:"installed_at"`
	RuntimeID    string            `json:"runtime_id"`
	Metadata     map[string]string `json:"metadata"`
}

// ProviderState represents the state of a provider
type ProviderState string

const (
	ProviderStateStopped  ProviderState = "stopped"
	ProviderStateStarting ProviderState = "starting"
	ProviderStateRunning  ProviderState = "running"
	ProviderStateStopping ProviderState = "stopping"
	ProviderStateFailed   ProviderState = "failed"
)

// getAgentPort retrieves the agent port from context or returns default
func getAgentPort(ctx context.Context) int {
	if port, ok := ctx.Value(configAgentPortContextKey).(int); ok && port != 0 {
		return port
	}
	return 2240 // default agent port
}
