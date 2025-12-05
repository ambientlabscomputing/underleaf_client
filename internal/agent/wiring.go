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
	"github.com/ambientlabscomputing/underleaf_client/internal/exec"
)

// Dependencies holds all agent dependencies
type Dependencies struct {
	Config           config_manager.ConfigClient
	ConfigManager    config_manager.ConfigManager
	Server           *Server
	CommandHandler   *exec.CommandHandler
	MetricsCollector *MetricsCollector
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

	// Initialize control plane client for config fetching and command results
	httpClient := http.DefaultClient
	cplaneClient := controlplane.NewCPlaneClient(&configClient, httpClient)
	cpConfigAdapter := config_manager.NewControlPlaneConfigAdapter(cplaneClient.Config)

	// Initialize event bus client for push updates and command events
	var eventBusAdapter *config_manager.EventBusAdapter
	var busClient *bus.Client
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

		// Enable verbose mode to debug WebSocket communication
		ebClient.SetVerbose(true)

		var err error
		busClient, err = bus.NewClient(ebClient)
		if err != nil {
			slog.Warn("failed to create event bus client", "error", err)
		} else {
			slog.Info("starting event bus client")
			if err := busClient.Start(ctx, serverID.(string)); err != nil {
				slog.Warn("failed to start event bus client", "error", err)
				busClient = nil
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

	// Ensure local metadata is populated from CLI config
	// This syncs values from config.yaml into the snapshot's localmeta
	if err := ensureLocalMetadata(store, simpleConfig, serverID.(string)); err != nil {
		slog.Warn("failed to ensure local metadata", "error", err)
	}

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

	// Initialize command execution system
	commandSettings := getCommandSettingsFromConfig(snapshotClient)
	runner := exec.NewLocalRunner(serverID.(string), commandSettings)
	commandHandler := exec.NewCommandHandler(runner, cplaneClient.Commands, serverID.(string))

	// Subscribe to command events if event bus is available
	if busClient != nil {
		go subscribeToCommandEvents(ctx, busClient, commandHandler, serverID.(string))
	}

	// Watch for config updates to refresh command settings
	go watchConfigForCommandSettings(ctx, snapshotManager, commandHandler)

	// Initialize and start metrics collector
	metricsCollector := NewMetricsCollector(serverID.(string), cplaneClient.Servers, DefaultMetricsInterval)
	metricsCollector.Start(ctx)

	// Initialize server
	server := NewServer(port)
	server.SetDependencies(snapshotManager, snapshotClient)
	server.SetCommandHandler(commandHandler)

	slog.Info("agent wired successfully", "port", port)

	return &Dependencies{
		Config:           snapshotClient,
		ConfigManager:    snapshotManager,
		Server:           server,
		CommandHandler:   commandHandler,
		MetricsCollector: metricsCollector,
	}, nil
}

// ensureLocalMetadata ensures local metadata in snapshot store matches CLI config
func ensureLocalMetadata(store *config_manager.Store, cliConfig config_manager.ConfigClient, serverID string) error {
	// Load existing metadata or create new
	meta, err := store.LoadLocalMeta()
	if err != nil {
		meta = &config_manager.LocalMetadata{
			Extra: make(map[string]interface{}),
		}
	}

	// Sync values from CLI config to local metadata
	meta.ServerID = serverID

	if serverName, ok := cliConfig.Get("local.server_name"); ok && serverName != "" {
		meta.ServerName = serverName.(string)
	}

	if token, ok := cliConfig.Get("auth.token"); ok && token != "" {
		meta.AuthToken = token.(string)
	}

	if apiURL, ok := cliConfig.Get("api.base_url"); ok && apiURL != "" {
		meta.APIBaseURL = apiURL.(string)
	}

	if ebEndpoint, ok := cliConfig.Get("event_bus.endpoint"); ok && ebEndpoint != "" {
		meta.EventBus.Endpoint = ebEndpoint.(string)
	}

	if ebCommit, ok := cliConfig.Get("event_bus.commit_interval"); ok && ebCommit != "" {
		meta.EventBus.CommitInterval = ebCommit.(string)
	}

	// Save updated metadata
	return store.SaveLocalMeta(meta)
}

// getCommandSettingsFromConfig extracts command settings from config snapshot
func getCommandSettingsFromConfig(config config_manager.ConfigClient) exec.CommandSettings {
	settings := exec.DefaultCommandSettings()

	// Try to get commands config from payload
	if commands, ok := config.Get("commands"); ok {
		if commandsMap, ok := commands.(map[string]interface{}); ok {
			if whitelist, ok := commandsMap["whitelist"].([]interface{}); ok {
				settings.Whitelist = make([]string, 0, len(whitelist))
				for _, v := range whitelist {
					if s, ok := v.(string); ok {
						settings.Whitelist = append(settings.Whitelist, s)
					}
				}
			}
			if blacklist, ok := commandsMap["blacklist"].([]interface{}); ok {
				settings.Blacklist = make([]string, 0, len(blacklist))
				for _, v := range blacklist {
					if s, ok := v.(string); ok {
						settings.Blacklist = append(settings.Blacklist, s)
					}
				}
			}
			if allowLiteral, ok := commandsMap["allow_literal_commands"].(bool); ok {
				settings.AllowLiteralCommands = allowLiteral
			}
			if timeout, ok := commandsMap["default_timeout"].(float64); ok {
				settings.DefaultTimeout = int(timeout)
			}
		}
	}

	slog.Info("loaded command settings",
		"whitelist_count", len(settings.Whitelist),
		"blacklist_count", len(settings.Blacklist),
		"allow_literal", settings.AllowLiteralCommands,
		"default_timeout", settings.DefaultTimeout,
	)

	return settings
}

// subscribeToCommandEvents subscribes to command execution events
func subscribeToCommandEvents(ctx context.Context, busClient *bus.Client, handler *exec.CommandHandler, serverID string) {
	slog.Info("subscribing to command events", "server_id", serverID)

	// Subscribe to command run requests - topic only, filter by server_id in handler
	// Note: server_id is in message content, not in message metadata (TargetID)
	subscription, err := busClient.Subscribe(ctx, bus.SelectorFields{
		Topic: bus.CommandsRunRequest,
		// Don't filter by TargetID here - it's in the message payload, not metadata
	})
	if err != nil {
		slog.Error("failed to subscribe to command events", "error", err)
		return
	}

	slog.Info("subscribed to command events successfully", "topic", bus.CommandsRunRequest)

	// Handle incoming command messages
	for {
		select {
		case msg := <-subscription.HandlerChan:
			slog.Debug("received command event message")
			handler.HandleCommandEvent(ctx, []byte(msg.Content))
		case <-ctx.Done():
			slog.Info("stopping command event subscription")
			return
		}
	}
}

// watchConfigForCommandSettings watches for config updates and refreshes command settings
func watchConfigForCommandSettings(ctx context.Context, manager config_manager.ConfigManager, handler *exec.CommandHandler) {
	watchChan := manager.Watch()

	for {
		select {
		case snapshot := <-watchChan:
			if snapshot == nil {
				continue
			}

			// Extract and update command settings from new snapshot
			settings := extractCommandSettingsFromSnapshot(snapshot)
			handler.UpdateSettings(settings)
			slog.Info("updated command settings from config snapshot", "version", snapshot.Version)

		case <-ctx.Done():
			return
		}
	}
}

// extractCommandSettingsFromSnapshot extracts command settings from a config snapshot
func extractCommandSettingsFromSnapshot(snapshot *config_manager.ConfigSnapshot) exec.CommandSettings {
	settings := exec.DefaultCommandSettings()

	if snapshot == nil || snapshot.Payload == nil {
		return settings
	}

	if commands, ok := snapshot.Payload["commands"]; ok {
		if commandsMap, ok := commands.(map[string]interface{}); ok {
			if whitelist, ok := commandsMap["whitelist"].([]interface{}); ok {
				settings.Whitelist = make([]string, 0, len(whitelist))
				for _, v := range whitelist {
					if s, ok := v.(string); ok {
						settings.Whitelist = append(settings.Whitelist, s)
					}
				}
			}
			if blacklist, ok := commandsMap["blacklist"].([]interface{}); ok {
				settings.Blacklist = make([]string, 0, len(blacklist))
				for _, v := range blacklist {
					if s, ok := v.(string); ok {
						settings.Blacklist = append(settings.Blacklist, s)
					}
				}
			}
			if allowLiteral, ok := commandsMap["allow_literal_commands"].(bool); ok {
				settings.AllowLiteralCommands = allowLiteral
			}
			if timeout, ok := commandsMap["default_timeout"].(float64); ok {
				settings.DefaultTimeout = int(timeout)
			}
		}
	}

	return settings
}
