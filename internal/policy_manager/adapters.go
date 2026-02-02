package policy_manager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/ambientlabscomputing/underleaf_client/internal/bus"
)

// EventBusAdapter adapts our bus.Client to the EventBusClient interface
type EventBusAdapter struct {
	client *bus.Client
}

// NewEventBusAdapter creates a new event bus adapter
func NewEventBusAdapter(client *bus.Client) *EventBusAdapter {
	return &EventBusAdapter{
		client: client,
	}
}

// Subscribe subscribes to a topic with target ID filter
func (a *EventBusAdapter) Subscribe(ctx context.Context, topic string, targetID string, handler func(payload []byte)) error {
	if a == nil || a.client == nil {
		return fmt.Errorf("event bus adapter not initialized")
	}

	selector := bus.SelectorFields{
		Topic:    topic,
		TargetID: targetID,
	}

	subscription, err := a.client.Subscribe(ctx, selector)
	if err != nil {
		return err
	}

	// Start goroutine to handle incoming messages
	go func() {
		for {
			select {
			case msg := <-subscription.HandlerChan:
				// Extract content and call handler
				content := []byte(msg.Content)
				handler(content)
			case <-ctx.Done():
				return
			}
		}
	}()

	return nil
}

// ControlPlaneConfigAdapter adapts controlplane.ConfigClient to our interface
type ControlPlaneConfigAdapter struct {
	configClient interface {
		GetServerConfig(ctx context.Context, serverID string) (map[string]interface{}, int, error)
	}
}

// NewControlPlaneConfigAdapter creates a new control plane adapter
func NewControlPlaneConfigAdapter(client interface {
	GetServerConfig(ctx context.Context, serverID string) (map[string]interface{}, int, error)
}) *ControlPlaneConfigAdapter {
	return &ControlPlaneConfigAdapter{
		configClient: client,
	}
}

// GetServerConfig fetches config from control plane
func (a *ControlPlaneConfigAdapter) GetServerConfig(ctx context.Context, serverID string) (map[string]interface{}, int, error) {
	return a.configClient.GetServerConfig(ctx, serverID)
}

// ParseServerDataUpdateEvent parses the server-data-update event payload
func ParseServerDataUpdateEvent(payload []byte) (serverID string, version int, config map[string]interface{}, err error) {
	var event struct {
		ServerID string                 `json:"server_id"`
		Version  int                    `json:"version"`
		Config   map[string]interface{} `json:"config"`
	}

	if err := json.Unmarshal(payload, &event); err != nil {
		slog.Error("failed to parse server-data-update event", "error", err)
		return "", 0, nil, err
	}

	return event.ServerID, event.Version, event.Config, nil
}
