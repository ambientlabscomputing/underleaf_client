package config_manager

import (
	"context"
)

type ConfigManager interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// AgentConfigManager is meant for long-running agent use cases
// communicates with control plane to fetch and update configuration snapshots
type AgentConfigManager struct{}
