package agent

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/ambientlabscomputing/event_bus_client"
	"github.com/ambientlabscomputing/underleaf_client/internal/bus"
	"github.com/ambientlabscomputing/underleaf_client/internal/capability"
	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/deployment"
	"github.com/ambientlabscomputing/underleaf_client/internal/exec"
	"github.com/ambientlabscomputing/underleaf_client/internal/mdns"
	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
	"github.com/ambientlabscomputing/underleaf_client/internal/raft"
	servertypes "github.com/ambientlabscomputing/underleaf_client/internal/types/server"
	"github.com/ambientlabscomputing/underleaf_client/internal/updater"
	"github.com/ambientlabscomputing/underleaf_client/internal/utils"
	"github.com/moby/moby/client"
)

// Dependencies holds all agent dependencies
type Dependencies struct {
	Config            policy_manager.ConfigClient
	PolicyManager     policy_manager.PolicyManager
	Server            *Server
	CommandHandler    *exec.CommandHandler
	MetricsCollector  *MetricsCollector
	ClusterReporter   *ClusterStatusReporter // Cluster status reporter for Raft cluster heartbeats
	BusClient         *bus.Client            // Event bus client for cleanup on shutdown
	UpdateManager     *updater.UpdateManager // Update manager for auto-updates
	CommandDrainer    *CommandDrainer        // Command drainer for graceful updates
	RaftNode          *raft.Node             // Raft cluster node for KV quorum
	EventStreamServer *EventStreamServer     // UA→MMA event stream server
	CapabilityManager interface{}            // Capability manager (type from internal/capability)
}

// getConfigValue tries to get a value with fallback to non-prefixed key for backward compatibility
func getConfigValue(config policy_manager.ConfigClient, key string) (interface{}, bool) {
	// Try with local. prefix first
	if val, ok := config.Get("local." + key); ok {
		return val, true
	}
	// Fallback to non-prefixed for backward compatibility
	return config.Get(key)
}

// getConfigValueStr gets a string value with a default
func getConfigValueStr(config policy_manager.ConfigClient, key string, defaultValue string) string {
	if val, ok := getConfigValue(config, key); ok {
		if strVal, ok := val.(string); ok {
			return strVal
		}
	}
	return defaultValue
}

// getConfigValueInt gets an int value with a default
func getConfigValueInt(config policy_manager.ConfigClient, key string, defaultValue int) int {
	if val, ok := getConfigValue(config, key); ok {
		switch v := val.(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		}
	}
	return defaultValue
}

// getConfigValueBool gets a bool value with a default
func getConfigValueBool(config policy_manager.ConfigClient, key string, defaultValue bool) bool {
	if val, ok := getConfigValue(config, key); ok {
		switch v := val.(type) {
		case bool:
			return v
		case string:
			// Handle string "true"/"false"
			return v == "true" || v == "True" || v == "TRUE"
		}
	}
	return defaultValue
}

// WireAgent sets up all agent dependencies
func WireAgent(ctx context.Context, port int) (*Dependencies, error) {
	// Initialize simple config to get credentials
	simpleConfig := policy_manager.NewCLIConfigClient()
	var configClient policy_manager.ConfigClient = simpleConfig

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
	// Configure HTTP client with mTLS transport if certificate is available
	var httpClient *http.Client
	certPath, hasCert := getConfigValue(configClient, "mtls.certificate_path")
	keyPath, hasKey := getConfigValue(configClient, "mtls.private_key_path")

	if hasCert && hasKey && certPath != "" && keyPath != "" {
		// Create mTLS transport with certificate
		transport, err := controlplane.NewMTLSTransport(
			http.DefaultTransport,
			keyPath.(string),
			certPath.(string),
		)
		if err != nil {
			// Certificate loading failed, fall back to JWT auth
			slog.Warn("failed to load mTLS certificate, using JWT auth", "error", err)
			httpClient = http.DefaultClient
		} else {
			slog.Info("mTLS transport enabled for agent")
			httpClient = &http.Client{Transport: transport}
		}
	} else {
		// No certificate available, use default HTTP client (JWT auth)
		httpClient = http.DefaultClient
	}

	cplaneClient := controlplane.NewCPlaneClient(&configClient, httpClient)
	cpConfigAdapter := policy_manager.NewControlPlaneConfigAdapter(cplaneClient.Config)

	// Initialize event bus client for push updates and command events
	var eventBusAdapter *policy_manager.EventBusAdapter
	var busClient *bus.Client
	endpoint, hasEndpoint := getConfigValue(configClient, "event_bus.endpoint")
	if hasEndpoint && endpoint != "" {
		commitInterval, _ := getConfigValue(configClient, "event_bus.commit_interval")
		if commitInterval == "" || commitInterval == nil {
			commitInterval = "5s"
		}

		slog.Info("initializing event bus client", "endpoint", endpoint, "server_id", serverID)

		// Prepare event bus client options
		ebOpts := event_bus_client.EventClientOpts{
			Endpoint:       endpoint.(string),
			CommitInterval: commitInterval.(string),
			GroupID:        serverID.(string),
		}

		// Use mTLS if certificate is available, otherwise use JWT token
		if hasCert && hasKey && certPath != "" && keyPath != "" {
			slog.Info("configuring event bus client with mTLS authentication")
			ebOpts.CertPath = certPath.(string)
			ebOpts.KeyPath = keyPath.(string)
		} else {
			slog.Info("configuring event bus client with JWT authentication")
			ebOpts.AuthToken = token.(string)
		}

		ebClient, err := event_bus_client.NewEventClient(ebOpts)
		if err != nil {
			slog.Warn("failed to create event bus client", "error", err)
		} else {
			// Enable verbose mode to debug WebSocket communication
			ebClient.SetVerbose(true)

			var busErr error
			busClient, busErr = bus.NewClient(ebClient)
			if busErr != nil {
				slog.Warn("failed to create bus client wrapper", "error", busErr)
			} else {
				slog.Info("starting event bus client in background")

				// Use background context for event bus - it should live for entire agent process
				// Not tied to the Start command context which may be cancelled
				busCtx := context.Background()

				// Start event bus client in background - don't block agent startup
				go func() {
					if err := busClient.Start(busCtx, serverID.(string)); err != nil {
						slog.Warn("failed to start event bus client", "error", err)
					} else {
						slog.Info("event bus client started successfully")
					}
				}()

				// Set up event bus adapter immediately - it will work once connection is established
				eventBusAdapter = policy_manager.NewEventBusAdapter(busClient)
			}
		}
	} else {
		slog.Info("event bus not configured, push updates disabled")
	}

	// Initialize config store
	basePath := policy_manager.GetBasePath(true) // true = agent
	store := policy_manager.NewStore(basePath, true)

	// Ensure local metadata is populated from CLI config
	// This syncs values from config.yaml into the snapshot's localmeta
	if err := ensureLocalMetadata(store, simpleConfig, serverID.(string)); err != nil {
		slog.Warn("failed to ensure local metadata", "error", err)
	}

	// Create snapshot config manager
	policyManager := policy_manager.NewSnapshotPolicyManager(policy_manager.SnapshotPolicyManagerConfig{
		Store:             store,
		ControlPlane:      cpConfigAdapter,
		EventBus:          eventBusAdapter,
		ServerID:          serverID.(string),
		ReconcileInterval: 0, // use defaults
		MaxAge:            0, // use defaults
	})

	// Start the config manager
	if err := policyManager.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start config manager: %w", err)
	}

	// Create snapshot config client
	snapshotClient := policy_manager.NewSnapshotPolicyClientWithManager(policyManager, store)

	// Initialize command drainer for graceful updates
	drainer := NewCommandDrainer()

	// Initialize command execution system
	commandSettings := getCommandSettingsFromConfig(snapshotClient)
	runner := exec.NewLocalRunner(serverID.(string), commandSettings)
	commandHandler := exec.NewCommandHandler(runner, cplaneClient.Commands, serverID.(string))
	commandHandler.SetDrainer(drainer)

	// Initialize deployment handler
	deploymentHandler := deployment.NewDeploymentHandler(serverID.(string), cplaneClient.Deployments)
	deploymentHandler.SetDrainer(drainer)

	// Subscribe to command events if event bus is available
	if busClient != nil {
		go subscribeToCommandEvents(ctx, busClient, commandHandler, serverID.(string))
		go subscribeToDeploymentEvents(ctx, busClient, deploymentHandler, serverID.(string))
		go subscribeToClusterMembershipEvents(ctx, busClient, policyManager, serverID.(string))
	}

	// Watch for config updates to refresh command settings
	go watchConfigForCommandSettings(ctx, policyManager, commandHandler)

	// Publish hostname and IP address to control plane
	if err := publishNetworkInfo(ctx, cplaneClient.Servers, serverID.(string)); err != nil {
		slog.Warn("failed to publish network info", "error", err)
		// Don't fail agent startup if network info publish fails
	}

	// Initialize server
	server := NewServer(port)
	server.SetDependencies(policyManager, snapshotClient)
	server.SetCommandHandler(commandHandler)

	// Initialize Raft node if configured
	var raftNode *raft.Node
	var clusterReporter *ClusterStatusReporter
	// Try snapshot config first, fall back to simple config for local-only testing
	raftConfig := getRaftConfigFromSnapshot(snapshotClient)
	if raftConfig == nil {
		raftConfig = getRaftConfigFromSnapshot(configClient)
	}

	if raftConfig != nil {
		slog.Info("initializing raft cluster node", "node_id", raftConfig.NodeID)

		// Extract cluster_id from policy manager snapshot (it's not stored in raftConfig struct)
		var clusterID string
		if snapshot, err := policyManager.GetSnapshot(); err == nil && snapshot != nil && snapshot.Payload != nil {
			if raftVal, ok := snapshot.Payload["raft"]; ok {
				if raftMap, ok := raftVal.(map[string]interface{}); ok {
					clusterID, _ = raftMap["cluster_id"].(string)
				}
			}
		}

		var err error
		raftNode, err = initializeRaftNode(ctx, raftConfig)
		if err != nil {
			slog.Warn("failed to initialize raft node", "error", err)
			// Don't fail agent startup if Raft fails - it's optional
		} else {
			slog.Info("raft node initialized successfully", "node_id", raftConfig.NodeID)

			// Wire Raft node into server
			server.SetRaftNode(raftNode)

			// Initialize mDNS coordinator if enabled (pass raftConfig for NodeID)
			// Try snapshot config first, fall back to simple config
			mdnsCoordinator := initializeMDNSCoordinator(ctx, snapshotClient, cplaneClient, port, raftConfig)
			if mdnsCoordinator == nil {
				mdnsCoordinator = initializeMDNSCoordinator(ctx, configClient, cplaneClient, port, raftConfig)
			}

			if mdnsCoordinator != nil {
				// Register mDNS coordinator with Raft leadership callbacks
				raftNode.RegisterLeaderChangeCallback(mdnsCoordinator.OnLeadershipChange)
				slog.Info("mDNS coordinator registered with Raft node")

				// Start node-specific mDNS announcement (always running)
				if err := mdnsCoordinator.Start(); err != nil {
					slog.Warn("failed to start mDNS coordinator", "error", err)
				} else {
				}
			} else {
				slog.Warn("DEBUG: mDNS coordinator is nil, not starting")
			}

			// Watch for Raft configuration changes (peer updates)
			go watchConfigForRaftUpdates(ctx, policyManager, raftNode, nil)

			// Initialize and start cluster status reporter
			clusterReporter = NewClusterStatusReporter(
				serverID.(string),
				clusterID,
				cplaneClient.Servers,
				raftNode,
				DefaultClusterReportInterval,
			)
			clusterReporter.Start(ctx)
			slog.Info("cluster status reporter started")
		}
	} else {
		slog.Info("raft cluster not configured on startup, checking current snapshot")

		// Check if current snapshot already has raft config that wasn't detected
		snapshot, err := policyManager.GetSnapshot()
		if err == nil && snapshot != nil && snapshot.Payload != nil {
			// Extract both raftConfig and cluster_id
			raftConfig := getRaftConfigFromPayload(snapshot.Payload)
			var clusterID string
			if raftVal, ok := snapshot.Payload["raft"]; ok {
				if raftMap, ok := raftVal.(map[string]interface{}); ok {
					clusterID, _ = raftMap["cluster_id"].(string)
				}
			}
			if raftConfig != nil {
				slog.Info("found raft config in current snapshot, initializing now", "node_id", raftConfig.NodeID)

				raftNode, err := initializeRaftNode(ctx, raftConfig)
				if err != nil {
					slog.Warn("failed to initialize raft node from snapshot", "error", err)
				} else {
					slog.Info("raft node initialized successfully from current snapshot", "node_id", raftConfig.NodeID)
					server.SetRaftNode(raftNode)

					// Initialize mDNS coordinator if enabled
					mdnsCoordinator := initializeMDNSCoordinator(ctx, snapshotClient, cplaneClient, port, raftConfig)
					if mdnsCoordinator != nil {
						raftNode.RegisterLeaderChangeCallback(mdnsCoordinator.OnLeadershipChange)
						if err := mdnsCoordinator.Start(); err != nil {
							slog.Warn("failed to start mDNS coordinator", "error", err)
						}
					}

					// Watch for peer updates
					go watchConfigForRaftUpdates(ctx, policyManager, raftNode, nil)

					// Initialize cluster reporter
					clusterReporter = NewClusterStatusReporter(
						serverID.(string),
						clusterID,
						cplaneClient.Servers,
						raftNode,
						DefaultClusterReportInterval,
					)
					clusterReporter.Start(ctx)
					slog.Info("cluster status reporter started")
				}
			} else {
				slog.Info("no raft config in current snapshot, will monitor for cluster assignment")
				// Watch for Raft configuration to appear (when server is added to cluster)
				go watchConfigForRaftInitialization(ctx, policyManager, server, port, cplaneClient)
			}
		} else {
			slog.Info("could not check current snapshot, will monitor for cluster assignment")
			// Watch for Raft configuration to appear (when server is added to cluster)
			go watchConfigForRaftInitialization(ctx, policyManager, server, port, cplaneClient)
		}
	}

	// Initialize update manager
	updateBasePath := filepath.Join(policy_manager.GetBasePath(true), "updates")
	updateStore := updater.NewStore(updateBasePath)
	updateInstaller := updater.NewInstaller(updateStore, nil, drainer) // Restarter will be set later
	updateManager := updater.NewUpdateManager(updater.UpdateManagerConfig{
		Store:     updateStore,
		Installer: updateInstaller,
	})

	// Start update manager
	if err := updateManager.Start(ctx); err != nil {
		slog.Warn("failed to start update manager", "error", err)
		// Don't fail agent startup if update manager fails
	} else {
		slog.Info("update manager started successfully")
		// Watch for software_version changes in config
		go watchConfigForSoftwareUpdates(ctx, policyManager, updateManager)
	}

	// Initialize UA→MMA event stream server
	var eventStreamServer *EventStreamServer
	// Get cluster ID from raft config if available
	clusterID := "default-cluster"
	nodeID := serverID.(string)
	if snapshot, err := policyManager.GetSnapshot(); err == nil && snapshot != nil && snapshot.Payload != nil {
		if raftVal, ok := snapshot.Payload["raft"]; ok {
			if raftMap, ok := raftVal.(map[string]interface{}); ok {
				if cid, ok := raftMap["cluster_id"].(string); ok && cid != "" {
					clusterID = cid
				}
				if nid, ok := raftMap["node_id"].(string); ok && nid != "" {
					nodeID = nid
				}
			}
		}
	}

	// Use Unix socket for UA→MMA communication
	socketPath := "/tmp/ua_mma.sock"
	eventStreamServer = NewEventStreamServer(socketPath, clusterID, nodeID, slog.Default())
	if err := eventStreamServer.Start(ctx); err != nil {
		slog.Warn("failed to start UA event stream server", "error", err)
		// Don't fail agent startup if event stream fails - it's optional
		eventStreamServer = nil
	} else {
		slog.Info("UA event stream server started", "socket", socketPath, "cluster_id", clusterID, "node_id", nodeID)
		// Inject event stream server into server for HTTP handlers to use
		server.SetEventStreamServer(eventStreamServer)
	}

	// Initialize capability manager if enabled (use simpleConfig for local settings)
	var capabilityManager interface{}
	capEnabled := getConfigValueBool(simpleConfig, "capability_registry.enabled", false)
	slog.Info("checking capability_registry config", "enabled", capEnabled)
	if capEnabled {
		slog.Info("initializing capability manager")

		// Initialize Docker client
		dockerClient, err := client.NewClientWithOpts(client.FromEnv)
		if err != nil {
			slog.Warn("failed to initialize Docker client for capability manager", "error", err)
		} else {
			// Build capability configuration (all from local config)
			homeDir, _ := os.UserHomeDir()
			capConfig := capability.Config{
				UCRSBaseURL:   getConfigValueStr(simpleConfig, "capability_registry.ucrs_base_url", "https://registry.underleaf.io"),
				PublicKeyPath: getConfigValueStr(simpleConfig, "capability_registry.public_key_path", "/etc/underleaf/ucrs_public_key.pem"),
				CacheDir:      getConfigValueStr(simpleConfig, "capability_registry.cache_dir", filepath.Join(homeDir, ".underleaf", "capability_cache")),
				ProviderDir:   getConfigValueStr(simpleConfig, "capability_registry.provider_dir", filepath.Join(homeDir, ".underleaf", "providers")),
				SyncInterval:  time.Duration(getConfigValueInt(simpleConfig, "capability_registry.sync_interval_seconds", 600)) * time.Second,
				Network:       getConfigValueStr(simpleConfig, "provider_defaults.network", "underleaf-providers"),
				MemoryLimit:   getConfigValueStr(simpleConfig, "provider_defaults.memory_limit", "512m"),
				CPULimit:      getConfigValueStr(simpleConfig, "provider_defaults.cpu_limit", "1.0"),
				TrustTier:     getConfigValueStr(simpleConfig, "provider_defaults.trust_tier_constraint", "certified+"),
			}

			// Create capability manager
			mgr, err := capability.NewManager(dockerClient, capConfig)
			if err != nil {
				slog.Warn("failed to create capability manager", "error", err)
			} else {
				// Start capability manager
				if err := mgr.Start(ctx); err != nil {
					slog.Warn("failed to start capability manager", "error", err)
				} else {
					slog.Info("capability manager started successfully")
					capabilityManager = mgr
					server.SetCapabilityManager(capabilityManager)

					// Auto-install MMA if enabled and not already installed
					if getConfigValueBool(simpleConfig, "capability_registry.auto_install_mma", true) {
						mmaProviderID := getConfigValueStr(simpleConfig, "capability_registry.mma_provider_id", "underleaf.mma")
						go ensureMMAInstalled(ctx, mgr, mmaProviderID)
					}
				}
			}
		}
	}

	// Initialize and start metrics collector AFTER capability manager (so we can pass capability manager reference)
	metricsCollector := NewMetricsCollector(
		serverID.(string),
		cplaneClient.Servers,
		policyManager,
		raftNode,
		cplaneClient,
		capabilityManager, // Pass capability manager for provider reporting
		DefaultMetricsInterval,
		DefaultDockerMetricsInterval,
		DefaultProvidersInterval,
	)
	metricsCollector.Start(ctx)

	slog.Info("agent wired successfully", "port", port)

	return &Dependencies{
		Config:            snapshotClient,
		PolicyManager:     policyManager,
		Server:            server,
		CommandHandler:    commandHandler,
		MetricsCollector:  metricsCollector,
		ClusterReporter:   clusterReporter,
		BusClient:         busClient, // Store bus client for cleanup
		UpdateManager:     updateManager,
		CommandDrainer:    drainer,
		RaftNode:          raftNode,
		EventStreamServer: eventStreamServer,
		CapabilityManager: capabilityManager,
	}, nil
}

// ensureLocalMetadata ensures local metadata in snapshot store matches CLI config
func ensureLocalMetadata(store *policy_manager.Store, cliConfig policy_manager.ConfigClient, serverID string) error {
	// Load existing metadata or create new
	meta, err := store.LoadLocalMeta()
	if err != nil {
		meta = &policy_manager.LocalMetadata{
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
func getCommandSettingsFromConfig(config policy_manager.ConfigClient) exec.CommandSettings {
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

	// Subscribe to command run requests with TargetType and TargetID filters
	// Server API publishes with TargetType="server" and TargetID=serverID
	subscription, err := busClient.Subscribe(ctx, bus.SelectorFields{
		Topic:      bus.CommandsRunRequest,
		TargetType: "server",
		TargetID:   serverID,
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

// subscribeToClusterMembershipEvents subscribes to cluster membership change events
func subscribeToClusterMembershipEvents(ctx context.Context, busClient *bus.Client, policyManager policy_manager.PolicyManager, serverID string) {
	slog.Info("subscribing to cluster membership events", "server_id", serverID)

	// Subscribe to cluster membership change notifications
	subscription, err := busClient.Subscribe(ctx, bus.SelectorFields{
		Topic:      bus.ClusterMembershipChanged,
		TargetType: "server",
		TargetID:   serverID,
	})
	if err != nil {
		slog.Error("failed to subscribe to cluster membership events", "error", err)
		return
	}

	slog.Info("subscribed to cluster membership events successfully", "topic", bus.ClusterMembershipChanged)

	// Handle incoming membership change messages
	for {
		select {
		case msg := <-subscription.HandlerChan:
			// Safely handle content preview
			contentPreview := msg.Content
			if len(contentPreview) > 100 {
				contentPreview = contentPreview[:100] + "..."
			}
			slog.Info("received cluster membership change event",
				"topic", msg.Topic,
				"content_preview", contentPreview)

			// Config sync will happen automatically via the config version bump
			// The backend bumps config version for all cluster members when membership changes
			// The watchConfigForRaftUpdates handler will detect changes and update Raft
			// This event serves as immediate notification that changes are coming

		case <-ctx.Done():
			slog.Info("stopping cluster membership event subscription")
			return
		}
	}
}

// watchConfigForCommandSettings watches for config updates and refreshes command settings
func watchConfigForCommandSettings(ctx context.Context, manager policy_manager.PolicyManager, handler *exec.CommandHandler) {
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
func extractCommandSettingsFromSnapshot(snapshot *policy_manager.PolicySnapshot) exec.CommandSettings {
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

// publishNetworkInfo detects and publishes the hostname and IPv4 address to the control plane
func publishNetworkInfo(ctx context.Context, serverClient controlplane.CPlaneServerClient, serverID string) error {
	// Detect network information
	netInfo, err := utils.GetNetworkInfo()
	if err != nil {
		return fmt.Errorf("failed to detect network info: %w", err)
	}

	slog.Info("detected network information",
		"hostname", netInfo.Hostname,
		"ipv4", netInfo.IPv4,
		"interface", netInfo.Interface,
	)

	// Prepare update request
	updateReq := servertypes.UpdateServerRequest{
		Hostname:  netInfo.Hostname,
		IPAddress: netInfo.IPv4,
	}

	// Send update to control plane
	if _, err := serverClient.UpdateServer(ctx, serverID, updateReq); err != nil {
		return fmt.Errorf("failed to update server network info: %w", err)
	}

	slog.Info("successfully published network info to control plane",
		"server_id", serverID,
		"hostname", netInfo.Hostname,
		"ipv4", netInfo.IPv4,
	)

	return nil
}

// watchConfigForSoftwareUpdates monitors the config manager for software_version changes
// and triggers the update manager to check and download new versions.
func watchConfigForSoftwareUpdates(ctx context.Context, policyManager *policy_manager.SnapshotPolicyManager, updateManager *updater.UpdateManager) {
	snapshotChan := policyManager.Watch()
	versionChan := make(chan string, 10)

	// Wire the version channel to the update manager
	go updateManager.WatchConfigVersion(versionChan)

	// Give the watcher goroutine time to start before sending initial version
	time.Sleep(100 * time.Millisecond)

	// Load initial snapshot from disk to set the desired version at startup
	if snapshot, err := policyManager.GetSnapshot(); err == nil && snapshot != nil && snapshot.Payload != nil {
		if versionRaw, ok := snapshot.Payload["software_version"]; ok {
			if version, ok := versionRaw.(string); ok && version != "" {
				slog.Info("loaded initial software_version from snapshot",
					"version", version,
					"config_version", snapshot.Version,
				)
				versionChan <- version
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			slog.Info("stopping software update watcher")
			close(versionChan)
			return
		case snapshot := <-snapshotChan:
			if snapshot == nil || snapshot.Payload == nil {
				continue
			}

			// Extract software_version from config
			if versionRaw, ok := snapshot.Payload["software_version"]; ok {
				if version, ok := versionRaw.(string); ok && version != "" {
					slog.Debug("detected software_version in config",
						"version", version,
						"config_version", snapshot.Version,
					)
					versionChan <- version
				}
			}
		}
	}
}

// subscribeToDeploymentEvents subscribes to deployment events from the event bus
func subscribeToDeploymentEvents(ctx context.Context, busClient *bus.Client, handler *deployment.DeploymentHandler, serverID string) {
	slog.Info("subscribing to deployment events", "server_id", serverID)

	subscription, err := busClient.Subscribe(ctx, bus.SelectorFields{
		Topic:      bus.DeploymentsApplyRequest,
		TargetType: "server",
		TargetID:   serverID,
	})
	if err != nil {
		slog.Error("failed to subscribe to deployment events", "error", err)
		return
	}

	for {
		select {
		case msg := <-subscription.HandlerChan:
			slog.Debug("received deployment event", "topic", msg.Topic)
			handler.HandleDeploymentEvent(ctx, []byte(msg.Content))
		case <-ctx.Done():
			slog.Info("stopping deployment event subscription")
			return
		}
	}
}

// getRaftConfigFromSnapshot extracts Raft configuration from config snapshot
// getRaftConfigFromSnapshot extracts Raft configuration from server-provided config
// Config is populated by the control plane when a server is assigned to a cluster
func getRaftConfigFromSnapshot(config policy_manager.ConfigClient) *raft.NodeConfig {
	// Get raft section from config payload (provided by control plane)
	raftVal, ok := config.Get("raft")
	if !ok {
		return nil
	}

	raftMap, ok := raftVal.(map[string]interface{})
	if !ok {
		slog.Warn("raft config is not a map", "type", fmt.Sprintf("%T", raftVal))
		return nil
	}

	return getRaftConfigFromMap(raftMap)
}

// getRaftConfigFromPayload extracts Raft configuration from a payload map
func getRaftConfigFromPayload(payload map[string]interface{}) *raft.NodeConfig {
	raftVal, ok := payload["raft"]
	if !ok {
		return nil
	}

	raftMap, ok := raftVal.(map[string]interface{})
	if !ok {
		return nil
	}

	return getRaftConfigFromMap(raftMap)
}

// getRaftConfigFromMap extracts Raft configuration from a map
func getRaftConfigFromMap(raftMap map[string]interface{}) *raft.NodeConfig {

	// Check if enabled
	enabled, ok := raftMap["enabled"].(bool)
	if !ok || !enabled {
		return nil
	}

	// Extract required fields
	clusterID, ok := raftMap["cluster_id"].(string)
	if !ok || clusterID == "" {
		slog.Warn("raft enabled but missing cluster_id")
		return nil
	}

	nodeID, ok := raftMap["node_id"].(string)
	if !ok || nodeID == "" {
		slog.Warn("raft enabled but missing node_id")
		return nil
	}

	bindPort, ok := raftMap["bind_port"].(float64) // JSON numbers are float64
	if !ok {
		bindPort = 7001 // Default port
	}

	bootstrap, ok := raftMap["bootstrap"].(bool)
	if !ok {
		bootstrap = false
	}

	// Build bind address (bind to all interfaces, use configured port)
	bindAddr := fmt.Sprintf("0.0.0.0:%d", int(bindPort))

	// Get data directory (default to ~/.underleaf/raft)
	homeDir, _ := os.UserHomeDir()
	dataDir := filepath.Join(homeDir, ".underleaf", "raft")
	if dir, ok := raftMap["data_dir"].(string); ok && dir != "" {
		dataDir = dir
	}

	// Extract bootstrap peers first to determine cluster size
	var bootstrapPeers []string
	if peersVal, ok := raftMap["bootstrap_peers"]; ok {
		if peersList, ok := peersVal.([]interface{}); ok {
			for _, peerVal := range peersList {
				if peerMap, ok := peerVal.(map[string]interface{}); ok {
					if raftAddr, ok := peerMap["raft_address"].(string); ok && raftAddr != "" {
						bootstrapPeers = append(bootstrapPeers, raftAddr)
					}
				}
			}
		}
	}

	// Handle existing raft.db - behavior depends on cluster type
	raftDBPath := filepath.Join(dataDir, "raft.db")
	if _, err := os.Stat(raftDBPath); err == nil {
		// Raft DB exists
		if bootstrap && len(bootstrapPeers) == 0 {
			// Single-node cluster wants to bootstrap - delete old state and start fresh
			// This allows the node to become leader
			slog.Warn("removing stale raft data for single-node cluster bootstrap",
				"data_dir", dataDir)
			os.RemoveAll(dataDir)
			os.MkdirAll(dataDir, 0755)
		} else {
			// Multi-node cluster or joining existing cluster - keep existing state
			slog.Info("found existing raft data, disabling bootstrap mode", "data_dir", dataDir)
			bootstrap = false
		}
	}

	cfg := &raft.NodeConfig{
		NodeID:         nodeID,
		BindAddr:       bindAddr,
		DataDir:        dataDir,
		Bootstrap:      bootstrap,
		BootstrapPeers: bootstrapPeers,
	}

	// Apply defaults for timeouts/thresholds
	defaults := raft.DefaultNodeConfig()
	cfg.HeartbeatTimeout = defaults.HeartbeatTimeout
	cfg.ElectionTimeout = defaults.ElectionTimeout
	cfg.LeaderLeaseTimeout = defaults.LeaderLeaseTimeout
	cfg.SnapshotInterval = defaults.SnapshotInterval
	cfg.SnapshotThreshold = defaults.SnapshotThreshold
	cfg.MaxValueSize = defaults.MaxValueSize
	cfg.MaxStorageSize = defaults.MaxStorageSize

	slog.Info("extracted raft config from control plane",
		"cluster_id", clusterID,
		"node_id", nodeID,
		"bind_addr", bindAddr,
		"bootstrap", bootstrap,
		"peer_count", len(cfg.BootstrapPeers))

	return cfg
}

// initializeRaftNode creates and starts a Raft cluster node
func initializeRaftNode(ctx context.Context, config *raft.NodeConfig) (*raft.Node, error) {
	// Get logger from context - it's the best configured context
	logger := slog.Default().With("component", "raft", "node_id", config.NodeID)

	// Create Raft node
	node, err := raft.NewNode(config, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create raft node: %w", err)
	}

	// Start Raft node
	if err := node.Start(); err != nil {
		return nil, fmt.Errorf("failed to start raft node: %w", err)
	}

	// Wait for leader election in background (non-blocking)
	// Don't block agent startup waiting for Raft cluster to form
	go func() {
		logger.Info("waiting for raft leader election", "node_id", config.NodeID)
		if err := node.WaitForLeader(30 * time.Second); err != nil {
			logger.Warn("raft leader election timeout", "error", err)
			// Don't fail - node can still participate in cluster
		} else {
			leader, _ := node.GetLeader()
			logger.Info("raft leader elected", "leader", leader, "is_leader", node.IsLeader())
		}
	}()

	return node, nil
}

// watchConfigForRaftInitialization monitors config changes and initializes Raft when server is added to cluster
func watchConfigForRaftInitialization(ctx context.Context, manager policy_manager.PolicyManager, server *Server, port int, cplaneClient *controlplane.CPlaneClient) {
	logger := slog.Default().With("component", "raft-init-watcher")
	watchChan := manager.Watch()

	for {
		select {
		case snapshot := <-watchChan:
			if snapshot == nil || snapshot.Payload == nil {
				continue
			}

			// Check if Raft configuration has appeared
			raftVal, ok := snapshot.Payload["raft"]
			if !ok {
				continue
			}

			raftMap, ok := raftVal.(map[string]interface{})
			if !ok {
				continue
			}

			enabled, ok := raftMap["enabled"].(bool)
			if !ok || !enabled {
				continue
			}

			logger.Info("detected server added to cluster, initializing raft")

			// Extract Raft config
			raftConfig := getRaftConfigFromPayload(snapshot.Payload)
			if raftConfig == nil {
				logger.Warn("failed to extract raft config from payload")
				continue
			}

			// Initialize Raft node
			raftNode, err := initializeRaftNode(ctx, raftConfig)
			if err != nil {
				logger.Warn("failed to initialize raft node after cluster assignment", "error", err)
				continue
			}

			logger.Info("raft node initialized successfully after cluster assignment", "node_id", raftConfig.NodeID)

			// Wire Raft node into server
			server.SetRaftNode(raftNode)

			// Initialize mDNS coordinator if enabled
			mdnsCoordinator := initializeMDNSCoordinator(ctx, manager.(policy_manager.ConfigClient), cplaneClient, port, raftConfig)
			if mdnsCoordinator != nil {
				raftNode.RegisterLeaderChangeCallback(mdnsCoordinator.OnLeadershipChange)
				logger.Info("mDNS coordinator registered with Raft node")
				if err := mdnsCoordinator.Start(); err != nil {
					logger.Warn("failed to start mDNS coordinator", "error", err)
				}
			}

			// Start watching for peer updates
			go watchConfigForRaftUpdates(ctx, manager, raftNode, logger)

			// Exit this watcher - Raft is now initialized
			logger.Info("raft initialization watcher exiting, peer update watcher started")
			return

		case <-ctx.Done():
			logger.Info("stopping raft initialization watcher")
			return
		}
	}
}

// watchConfigForRaftUpdates monitors config changes and updates Raft cluster configuration
func watchConfigForRaftUpdates(ctx context.Context, manager policy_manager.PolicyManager, node *raft.Node, existingLogger *slog.Logger) {
	// Use logger from context - it's the best configured context
	var logger *slog.Logger
	if existingLogger != nil {
		logger = existingLogger
	} else {
		logger = slog.Default().With("component", "raft-config-watcher", "node_id", node.ID())
	}
	watchChan := manager.Watch()

	// Track last known cluster config to detect changes
	var lastKnownPeers []string

	for {
		select {
		case snapshot := <-watchChan:
			if snapshot == nil || snapshot.Payload == nil {
				continue
			}

			// Get Raft config from payload
			raftVal, ok := snapshot.Payload["raft"]
			if !ok {
				continue
			}

			raftMap, ok := raftVal.(map[string]interface{})
			if !ok {
				continue
			}

			// Check if Raft is still enabled
			enabled, ok := raftMap["enabled"].(bool)
			if !ok || !enabled {
				logger.Warn("raft disabled in config update - node will continue running")
				continue
			}

			// Extract bootstrap_peers list
			peersVal, ok := raftMap["bootstrap_peers"]
			if !ok {
				// No peers configured yet
				continue
			}

			peersList, ok := peersVal.([]interface{})
			if !ok {
				continue
			}

			// Build list of peer addresses
			var currentPeers []string
			for _, peerVal := range peersList {
				if peerMap, ok := peerVal.(map[string]interface{}); ok {
					if raftAddr, ok := peerMap["raft_address"].(string); ok && raftAddr != "" {
						currentPeers = append(currentPeers, raftAddr)
					}
				}
			}

			// Check if peer list has changed
			if !peersListEqual(lastKnownPeers, currentPeers) {
				logger.Info("detected raft peer list change",
					"old_peers", lastKnownPeers,
					"new_peers", currentPeers)

				// Only the leader should handle dynamic reconfiguration
				// Non-leaders will learn about new members through Raft replication
				if node.IsLeader() {
					if err := reconcileRaftMembership(ctx, node, currentPeers, nil); err != nil {
						logger.Warn("failed to reconcile raft membership", "error", err)
					}
				} else {
					logger.Info("not leader - waiting for leader to handle membership changes")
				}

				lastKnownPeers = currentPeers
			}

		case <-ctx.Done():
			logger.Info("stopping raft config watcher")
			return
		}
	}
}

// peersListEqual checks if two peer lists are equal (order-independent)
func peersListEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	// Create maps for quick lookup
	aMap := make(map[string]bool, len(a))
	for _, peer := range a {
		aMap[peer] = true
	}

	for _, peer := range b {
		if !aMap[peer] {
			return false
		}
	}

	return true
}

// reconcileRaftMembership ensures the Raft cluster membership matches the config
func reconcileRaftMembership(ctx context.Context, node *raft.Node, expectedPeers []string, eventPublisher raft.EventPublisher) error {
	// Use logger from context - it's the best configured context
	logger := slog.Default().With("component", "raft-reconciliation", "node_id", node.ID())

	// Get current Raft configuration
	currentConfig, err := node.GetConfiguration()
	if err != nil {
		return fmt.Errorf("failed to get current configuration: %w", err)
	}

	// Build map of current nodes
	currentNodes := make(map[string]bool)
	for nodeID := range currentConfig.Nodes {
		currentNodes[nodeID] = true
	}

	// Build map of expected peer addresses to node IDs
	// Note: We derive node ID from the address since the control plane provides addresses
	expectedNodes := make(map[string]string) // address -> nodeID
	for _, peerAddr := range expectedPeers {
		// Use address as node ID for now - this matches the control plane's approach
		expectedNodes[peerAddr] = peerAddr
	}

	// Add missing nodes
	membership := raft.NewMembership(node, eventPublisher)
	for addr, nodeID := range expectedNodes {
		if !currentNodes[nodeID] {
			logger.Info("adding new raft peer", "node_id", nodeID, "address", addr)
			if err := membership.AddNode(nodeID, addr); err != nil {
				logger.Warn("failed to add raft peer", "node_id", nodeID, "address", addr, "error", err)
				// Continue with other nodes even if one fails
			}
		}
	}

	// Note: We don't automatically remove nodes that aren't in the config
	// Node removal should be done explicitly through the cluster management API
	// to avoid accidentally removing nodes due to transient config issues

	return nil
}

// initializeMDNSCoordinator creates and initializes the mDNS coordinator for leader discovery
func initializeMDNSCoordinator(ctx context.Context, config policy_manager.ConfigClient, cplaneClient *controlplane.CPlaneClient, port int, raftConfig *raft.NodeConfig) *mdns.Coordinator {
	// Helper function to try both local. prefix and without
	getConfigVal := func(key string) (interface{}, bool) {
		if val, ok := config.Get("local." + key); ok {
			return val, true
		}
		return config.Get(key)
	}

	// Check if mDNS is enabled
	enabled, hasEnabled := getConfigVal("mdns.enabled")
	if !hasEnabled || enabled != true {
		slog.Info("mDNS service discovery not enabled")
		return nil
	}

	// Extract mDNS configuration
	clusterID, hasClusterID := getConfigVal("mdns.cluster_id")
	if !hasClusterID || clusterID == "" {
		slog.Warn("mDNS enabled but cluster_id not configured - mDNS disabled")
		return nil
	}

	// Get NodeID from Raft config
	nodeID := ""
	if raftConfig != nil {
		nodeID = raftConfig.NodeID
	}
	if nodeID == "" {
		slog.Warn("mDNS enabled but no NodeID available from Raft config - per-node discovery disabled")
	}

	// Get optional configuration with defaults
	mdnsPort := port
	if p, ok := getConfigVal("mdns.port"); ok {
		if portInt, ok := p.(int); ok {
			mdnsPort = portInt
		} else if portFloat, ok := p.(float64); ok {
			mdnsPort = int(portFloat)
		}
	}

	ttl := 2 * time.Second
	if t, ok := getConfigVal("mdns.ttl"); ok {
		if ttlInt, ok := t.(int); ok {
			ttl = time.Duration(ttlInt) * time.Second
		} else if ttlFloat, ok := t.(float64); ok {
			ttl = time.Duration(ttlFloat) * time.Second
		}
	}

	iface := ""
	if i, ok := getConfigVal("mdns.interface"); ok {
		if ifaceStr, ok := i.(string); ok {
			iface = ifaceStr
		}
	}

	version := ""
	if v, ok := getConfigVal("mdns.version"); ok {
		if vStr, ok := v.(string); ok {
			version = vStr
		}
	}
	if version == "" {
		// Default to build version
		version = "1.0.0"
	}

	// Fetch CA certificate and compute fingerprint
	caFingerprint := ""
	// Create a temporary API client to fetch CA cert
	apiClient := controlplane.NewAPIClient(config, nil)
	caCertPEM, err := apiClient.GetCACertificate(ctx)
	if err != nil {
		slog.Warn("failed to fetch CA certificate for mDNS fingerprint", "error", err)
		// Continue without fingerprint - validation will be skipped
	} else {
		caFingerprint = mdns.ComputeCAFingerprint(caCertPEM)
		slog.Info("CA certificate fingerprint computed for mDNS", "fingerprint", caFingerprint[:16]+"...")
	}

	// Create mDNS configuration
	mdnsConfig := &mdns.Config{
		Enabled:       true,
		NodeID:        nodeID,
		Port:          mdnsPort,
		TTL:           ttl,
		Interface:     iface,
		ClusterID:     clusterID.(string),
		CAFingerprint: caFingerprint,
		Version:       version,
	}

	// Create coordinator
	coordinator, err := mdns.NewCoordinator(mdnsConfig, slog.Default())
	if err != nil {
		slog.Error("failed to create mDNS coordinator", "error", err)
		return nil
	}

	slog.Info("mDNS coordinator initialized",
		"node_id", nodeID,
		"cluster_id", clusterID,
		"port", mdnsPort,
		"ttl", ttl,
		"interface", iface)

	return coordinator
}

// ensureMMAInstalled checks if Mycelium Mesh Agent is installed and starts it.
// If not installed, it logs a message. In production, MMA will be auto-installed
// via UCRS when it's registered as a provider.
func ensureMMAInstalled(ctx context.Context, mgr *capability.Manager, providerID string) {
	if providerID == "" {
		providerID = "underleaf.mma"
	}
	slog.Info("checking MMA installation status", "provider_id", providerID)

	// Check if MMA is already installed
	installed, err := mgr.ListInstalledProviders(ctx)
	if err != nil {
		slog.Warn("failed to list installed providers", "error", err)
		return
	}

	// Check if MMA is in the list
	for _, provider := range installed {
		if provider.ProviderID == providerID {
			slog.Info("MMA is installed",
				"provider_id", providerID,
				"version", provider.Version,
				"state", provider.State)
			return
		}
	}

	// MMA not installed
	slog.Info("MMA not installed, attempting auto-install", "provider_id", providerID)
	endpoint, err := mgr.InstallProviderByID(ctx, providerID)
	if err != nil {
		slog.Error("failed to auto-install MMA", "provider_id", providerID, "error", err)
		return
	}

	slog.Info("MMA auto-install completed", "provider_id", providerID, "state", endpoint.State)
}
