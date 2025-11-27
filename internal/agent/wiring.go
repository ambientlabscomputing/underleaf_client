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

// getConfigValue tries to get a value with fallback to non-prefixed key for backward compatibility
func getConfigValue(config config_manager.ConfigClient, key string) (interface{}, bool) {
	// Try with local. prefix first
	if val, ok := config.Get("local." + key); ok {
		return val, true
	}
	// Fallback to non-prefixed for backward compatibility
	return config.Get(key)
}

// WireAgent sets up all agent dependencies
func WireAgent(ctx context.Context, port int) (*Dependencies, error) {
	// Initialize simple config to get credentials
	simpleConfig := config_manager.NewCLIConfigClient()
	var configClient config_manager.ConfigClient = simpleConfig

	// Get server ID from local metadata (with backward compatibility)
	serverID, ok := getConfigValue(configClient, "server_id")
	if !ok {
		slog.Warn("no server ID configured, config manager will not sync")
		return &Dependencies{
			Config: configClient,
			Server: NewServer(port),
		}, nil
	}

	// Check if we have token (with backward compatibility)
	token, ok := getConfigValue(configClient, "auth.token")
	if !ok || token == "" {
		slog.Warn("no auth token configured, config manager will not sync")
		return &Dependencies{
			Config: configClient,
			Server: NewServer(port),
		}, nil
	}

	slog.Info("initializing agent with config", "server_id", serverID, "has_token", true)

	// Initialize control plane client for config fetching
	httpClient := http.DefaultClient
	cplaneClient := controlplane.NewCPlaneClient(&configClient, httpClient)
	cpConfigAdapter := config_manager.NewControlPlaneConfigAdapter(cplaneClient.Config)

	// Initialize event bus client for push updates (if configured)
	var eventBusAdapter *config_manager.EventBusAdapter
	endpoint, hasEndpoint := getConfigValue(configClient, "event_bus.endpoint")
	if hasEndpoint && endpoint != "" {
		commitInterval, _ := getConfigValue(configClient, "event_bus.commit_interval")
		if commitInterval == "" || commitInterval == nil {
			commitInterval = "5s"
		}

		slog.Info("initializing event bus client", "endpoint", endpoint, "server_id", serverID)

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
			slog.Info("starting event bus client")
			if err := busClient.Start(ctx, serverID.(string)); err != nil {
				slog.Warn("failed to start event bus client", "error", err)
			} else {
				slog.Info("event bus client started successfully")
				eventBusAdapter = config_manager.NewEventBusAdapter(busClient)
			}
		}
	} else {
		slog.Info("event bus not configured, push updates disabled")
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
	slog.Info("starting config manager", "server_id", serverID)
	if err := snapshotManager.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start config manager: %w", err)
	}
	slog.Info("config manager started successfully")

	// Create snapshot config client
	snapshotClient := config_manager.NewSnapshotConfigClientWithManager(snapshotManager, store)

	// Initialize server
	server := NewServer(port)
	server.SetDependencies(snapshotManager, snapshotClient)

	slog.Info("agent wired successfully", "port", port)

	return &Dependencies{
		Config:        snapshotClient,
		ConfigManager: snapshotManager,
		Server:        server,
	}, nil
}
