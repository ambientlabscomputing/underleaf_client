package agent

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/ambientlabscomputing/event_bus_client"
	"github.com/ambientlabscomputing/underleaf_client/internal/bus"
	"github.com/ambientlabscomputing/underleaf_client/internal/config_manager"
	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
)

// Dependencies holds all agent dependencies
type Dependencies struct {
	Config        config_manager.ConfigClient
	ConfigManager config_manager.ConfigManager
	Server        *Server
}

// WireAgent sets up all agent dependencies
func WireAgent(ctx context.Context, port int) (*Dependencies, error) {
	// Initialize simple config to get credentials
	simpleConfig := config_manager.NewCLIConfigClient()
	var configClient config_manager.ConfigClient = simpleConfig

	// Get server ID from local metadata
	serverID, ok := configClient.Get("local.server_id")
	if !ok {
		slog.Warn("no server ID configured, config manager will not sync")
		return &Dependencies{
			Config: configClient,
			Server: NewServer(port),
		}, nil
	}

	// Check if we have token
	token, ok := configClient.Get("local.auth.token")
	if !ok || token == "" {
		slog.Warn("no auth token configured, config manager will not sync")
		return &Dependencies{
			Config: configClient,
			Server: NewServer(port),
		}, nil
	}

	// Initialize control plane client for config fetching
	httpClient := http.DefaultClient
	cplaneClient := controlplane.NewCPlaneClient(&configClient, httpClient)
	cpConfigAdapter := config_manager.NewControlPlaneConfigAdapter(cplaneClient.Config)

	// Initialize event bus client for push updates (if configured)
	var eventBusAdapter *config_manager.EventBusAdapter
	if endpoint, ok := configClient.Get("local.event_bus.endpoint"); ok && endpoint != "" {
		commitInterval, _ := configClient.Get("local.event_bus.commit_interval")
		if commitInterval == "" {
			commitInterval = "5s"
		}

		ebClient := event_bus_client.NewEventClient(event_bus_client.EventClientOpts{
			Endpoint:       endpoint.(string),
			CommitInterval: commitInterval.(string),
			AuthToken:      token.(string),
			GroupID:        serverID.(string),
		})

		busClient, err := bus.NewClient(ebClient)
		if err != nil {
			slog.Warn("failed to create event bus client", "error", err)
		} else {
			if err := busClient.Start(ctx, serverID.(string)); err != nil {
				slog.Warn("failed to start event bus client", "error", err)
			} else {
				eventBusAdapter = config_manager.NewEventBusAdapter(busClient)
			}
		}
	}

	// Initialize config store
	basePath := config_manager.GetBasePath(true) // true = agent
	store := config_manager.NewStore(basePath, true)

	// Create snapshot config manager
	snapshotManager := config_manager.NewSnapshotConfigManager(config_manager.SnapshotConfigManagerConfig{
		Store:             store,
		ControlPlane:      cpConfigAdapter,
		EventBus:          eventBusAdapter,
		ServerID:          serverID.(string),
		ReconcileInterval: 0, // use defaults
		MaxAge:            0, // use defaults
	})

	// Start the config manager
	if err := snapshotManager.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start config manager: %w", err)
	}

	// Create snapshot config client
	snapshotClient := config_manager.NewSnapshotConfigClientWithManager(snapshotManager, store)

	// Initialize server
	server := NewServer(port)
	server.SetDependencies(snapshotManager, snapshotClient)

	return &Dependencies{
		Config:        snapshotClient,
		ConfigManager: snapshotManager,
		Server:        server,
	}, nil
}
