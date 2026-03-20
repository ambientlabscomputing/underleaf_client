package secrets

import (
	"context"
)

// Context key for agent port (must match cli/root.go)
const configAgentPortContextKey = "config-agent-port"

// defaultAgentPort is the standard agent listen port.
const defaultAgentPort = 2240

var agentPort int

// getAgentPort retrieves the agent port from context or returns the default.
func getAgentPort(ctx context.Context) int {
	if port, ok := ctx.Value(configAgentPortContextKey).(int); ok && port != 0 {
		return port
	}
	if agentPort != 0 {
		return agentPort
	}
	return defaultAgentPort
}
