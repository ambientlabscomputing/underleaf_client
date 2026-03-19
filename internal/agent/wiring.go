package agent

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"crypto/tls"
	"crypto/x509"

	spinesdk "github.com/ambientlabscomputing/mycelium_spine/sdk"
	"github.com/ambientlabscomputing/underleaf_client/internal/capability"
	"github.com/ambientlabscomputing/underleaf_client/internal/config"
	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/crypto/keymanager"
	"github.com/ambientlabscomputing/underleaf_client/internal/devmode"
	"github.com/ambientlabscomputing/underleaf_client/internal/logcollector"
	dockerrunner "github.com/ambientlabscomputing/underleaf_client/internal/runner"
	"github.com/ambientlabscomputing/underleaf_client/internal/spine"

	// DEPRECATED: deployment and recipe packages moved to deployment_engine UMC
	// "github.com/ambientlabscomputing/underleaf_client/internal/deployment"
	execpkg "github.com/ambientlabscomputing/underleaf_client/internal/exec"
	"github.com/ambientlabscomputing/underleaf_client/internal/kernel"
	"github.com/ambientlabscomputing/underleaf_client/internal/mdns"
	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
	"github.com/ambientlabscomputing/underleaf_client/internal/raft"

	// "github.com/ambientlabscomputing/underleaf_client/internal/recipe"
	servertypes "github.com/ambientlabscomputing/underleaf_client/internal/types/server"
	"github.com/ambientlabscomputing/underleaf_client/internal/updater"
	"github.com/ambientlabscomputing/underleaf_client/internal/utils"
	"github.com/ambientlabscomputing/underleaf_client/pkg/defaults"
	"github.com/moby/moby/client"
)

// ManagedProcess represents a managed child process with graceful shutdown support
type ManagedProcess struct {
	Name    string
	Process *exec.Cmd
	PID     int
}

// Dependencies holds all agent dependencies
type Dependencies struct {
	Config            policy_manager.ConfigClient
	PolicyManager     policy_manager.PolicyManager
	Server            *Server
	CommandHandler    *execpkg.CommandHandler
	MetricsCollector  *MetricsCollector
	ClusterReporter   *ClusterStatusReporter // Cluster status reporter for Raft cluster heartbeats
	SpineClient       *spine.Client          // Mycelium Spine client for cleanup on shutdown
	UpdateManager     *updater.UpdateManager // Update manager for auto-updates
	CommandDrainer    *CommandDrainer        // Command drainer for graceful updates
	RaftNode          *raft.Node             // Raft cluster node for KV quorum
	EventStreamServer *EventStreamServer     // UA→MMA event stream server
	SyscallServer     *kernel.SyscallServer  // Kernel syscall server for UMCs
	KeyManager        keymanager.KeyManager  // Key manager for crypto operations
	CapabilityManager interface{}            // Capability manager (type from internal/capability)
	DeploymentEngine  *ManagedProcess        // Deployment engine UMC process
	CronEngine        *ManagedProcess        // Cron engine UMC process (managed by deployment engine supervisor)
	MMA               *ManagedProcess        // Mycelium Mesh Agent UMC process (dev mode)
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

// expandTilde expands a leading ~ in a path to the user's home directory.
// Go does not expand ~ in paths, so config values like ~/.underleaf must be resolved.
func expandTilde(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
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

// getConfigValuePath gets a path string value with a default, expanding ~ to $HOME.
func getConfigValuePath(config policy_manager.ConfigClient, key string, defaultValue string) string {
	if val, ok := getConfigValue(config, key); ok {
		if strVal, ok := val.(string); ok {
			return expandTilde(strVal)
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

// getConfigValueWithFallback tries primary config (snapshot) first, then fallback (local), then default
func getConfigValueWithFallback(primary, fallback policy_manager.ConfigClient, key string, defaultValue string) string {
	if val := getConfigValueStr(primary, key, ""); val != "" {
		return val
	}
	return getConfigValueStr(fallback, key, defaultValue)
}

// getConfigPathWithFallback tries primary config (snapshot) first, then fallback (local), then default
func getConfigPathWithFallback(primary, fallback policy_manager.ConfigClient, key string, defaultValue string) string {
	if val := getConfigValuePath(primary, key, ""); val != "" {
		return val
	}
	return getConfigValuePath(fallback, key, defaultValue)
}

// getConfigIntWithFallback tries primary config (snapshot) first, then fallback (local), then default
func getConfigIntWithFallback(primary, fallback policy_manager.ConfigClient, key string, defaultValue int) int {
	if val, ok := getConfigValue(primary, key); ok {
		switch v := val.(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		}
	}
	return getConfigValueInt(fallback, key, defaultValue)
}

// WireAgent sets up all agent dependencies
func WireAgent(ctx context.Context, port int, devConfig *devmode.DevConfig) (*Dependencies, error) {
	// Initialize simple config to get credentials
	simpleConfig := policy_manager.NewCLIConfigClient()

	// Run config migration on the local config file before any subsystem reads it.
	// This is the primary hook for schema migrations: it detects the file's
	// config_version, applies all pending Up() (or Down() on rollback) migrations
	// atomically, then reloads Viper so the rest of startup sees the new schema.
	{
		migrator := config.NewMigrator(config.DefaultRegistry)
		if applied, err := migrator.MigrateLocalConfig(simpleConfig.ConfigPath()); err != nil {
			// Config migration failure is fatal: starting with an unknown or
			// partially-migrated config risks data corruption or misbehaviour.
			panic(fmt.Sprintf("config migration failed: %v", err))
		} else if applied {
			// Reload Viper so it picks up any changes written by the migrator.
			if err := simpleConfig.Reload(); err != nil {
				panic(fmt.Sprintf("config reload after migration failed: %v", err))
			}
		}
	}

	var configClient policy_manager.ConfigClient = simpleConfig

	// Create the HTTP server immediately so the /health endpoint is reachable
	// while the rest of initialization runs.  ufctl's daemon-mode health check
	// polls /health — without an early listener the check times out and kills
	// the agent process.
	server := NewServer(port)
	if err := server.ListenEarly(); err != nil {
		return nil, fmt.Errorf("failed to start early HTTP listener: %w", err)
	}
	slog.Info("HTTP listener started (health check available)", "port", port)

	// Get server ID from local metadata (with backward compatibility)
	serverID, ok := getConfigValue(configClient, "server_id")
	if !ok {
		slog.Warn("no server ID configured, config manager will not sync")
		return &Dependencies{
			Config: configClient,
			Server: server,
		}, nil
	}

	// Check if we have token (with backward compatibility)
	token, ok := getConfigValue(configClient, "auth.token")
	if !ok || token == "" {
		slog.Warn("no auth token configured, config manager will not sync")
		return &Dependencies{
			Config: configClient,
			Server: server,
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

	// Initialize Mycelium Spine client for push updates and command events.
	var spineClient *spine.Client

	// DEV MODE: Check if Spine should be skipped
	if devCheckSkipSpine(devConfig) {
		slog.Warn("DEV MODE: skipping Spine connection per build.yaml")
	} else {
		spineEndpoint, hasSpineEndpoint := getConfigValue(configClient, "mycelium_spine.endpoint")
		if hasSpineEndpoint && spineEndpoint != "" {
			slog.Info("initializing Mycelium Spine client", "endpoint", spineEndpoint, "server_id", serverID)

			// Build TLS config by fetching the server_api CA dynamically.
			// The spine server cert is signed by server_api's CA — same trust anchor
			// the agent already uses for mTLS. This mirrors the mDNS fingerprint pattern.
			var spineTLS *tls.Config
			spineAPIClient := controlplane.NewAPIClient(configClient, http.DefaultClient)
			caCertPEM, err := spineAPIClient.GetCACertificate(ctx)
			if err != nil {
				slog.Warn("failed to fetch CA certificate for Spine TLS, spine will connect insecure", "error", err)
			} else {
				pool := x509.NewCertPool()
				pool.AppendCertsFromPEM(caCertPEM)
				spineTLS = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}

				// Present the agent's mTLS client certificate to the spine.
				// The spine requires mutual TLS (client_auth: require_and_verify)
				// so we reuse the same cert/key the agent uses for control-plane mTLS.
				if hasCert && hasKey && certPath != "" && keyPath != "" {
					clientCert, err := tls.LoadX509KeyPair(certPath.(string), keyPath.(string))
					if err != nil {
						slog.Warn("failed to load agent mTLS cert for Spine, continuing without client cert", "error", err)
					} else {
						spineTLS.Certificates = []tls.Certificate{clientCert}
						slog.Info("Spine TLS configured with agent mTLS client certificate")
					}
				}

				slog.Info("Mycelium Spine TLS configured with server_api CA")
			}

			// Get org ID for spine subscription (needed early, before policyManager exists)
			spineOrgID := getConfigValueStr(configClient, "local.organization_id", "")

			sdkCfg := spinesdk.ClientConfig{
				ServerID:      serverID.(string),
				OrgID:         spineOrgID,
				TLSConfig:     spineTLS,
				AutoReconnect: true,
				OnReconnect: func() {
					slog.Info("Mycelium Spine reconnected")
				},
			}
			sdkClient, err := spinesdk.NewClient(spineEndpoint.(string), sdkCfg)
			if err != nil {
				slog.Warn("failed to create Spine SDK client", "error", err)
			} else {
				var publisherOpts []spinesdk.PublisherOption
				if spineTLS != nil {
					publisherOpts = append(publisherOpts, spinesdk.WithTLS(spineTLS))
				}
				publisher, err := spinesdk.NewPublisher(spineEndpoint.(string), publisherOpts...)
				if err != nil {
					slog.Warn("failed to create Spine publisher", "error", err)
				} else {
					spineClient = spine.NewClient(sdkClient, publisher, serverID.(string), spineOrgID)
				}
			}
		} else {
			slog.Info("mycelium_spine not configured, push updates disabled")
		}
	} // end devCheckSkipSpine

	// Initialize config store
	basePath := policy_manager.GetBasePath(true) // true = agent
	store := policy_manager.NewStore(basePath, true)

	// Migrate the snapshot file before the policy manager loads it from disk.
	// This handles snapshot schema changes across binary versions, including
	// automatic rollback when a binary downgrades (Down() migrations run when
	// config_version in the file exceeds the binary's ExpectedConfigVersion).
	{
		migrator := config.NewMigrator(config.DefaultRegistry)
		if _, err := migrator.MigrateSnapshot(store.SnapshotPath()); err != nil {
			// Non-fatal: if the snapshot can't be migrated we log and continue;
			// the policy manager will re-fetch a fresh snapshot from the control plane.
			slog.Error("snapshot migration failed, policy manager will re-sync from control plane",
				"error", err)
		}
	}

	// Ensure local metadata is populated from CLI config
	// This syncs values from config.yaml into the snapshot's localmeta
	if err := ensureLocalMetadata(store, simpleConfig, serverID.(string)); err != nil {
		slog.Warn("failed to ensure local metadata", "error", err)
	}

	// Create snapshot config manager
	policyManager := policy_manager.NewSnapshotPolicyManager(policy_manager.SnapshotPolicyManagerConfig{
		Store:             store,
		ControlPlane:      cpConfigAdapter,
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
	runner := execpkg.NewLocalRunner(serverID.(string), commandSettings)
	commandHandler := execpkg.NewCommandHandler(runner, cplaneClient.Commands, serverID.(string))
	commandHandler.SetDrainer(drainer)

	// Initialize log collector — reads the structured agent log and uploads to server API
	logCollector := logcollector.New(cplaneClient.Logs)

	// Initialize service log collector — fetches Docker container logs and uploads to server API.
	// Creates its own Runner (and Docker client) since the deployment runner is managed separately
	// by the deployment_engine UMC. Falls back gracefully if Docker is unavailable.
	var serviceLogCollector *logcollector.ServiceCollector
	{
		dockerRunner, err := dockerrunner.NewRunner("/tmp/deployment-reports")
		if err != nil {
			slog.Warn("service log collector disabled: could not create Docker runner", "error", err)
		} else {
			serviceLogCollector = logcollector.NewServiceCollector(dockerRunner, cplaneClient.Logs)
		}
	}

	// Declare Raft and EventStream variables early for use in Spine handler closures
	// These will be initialized later in the code, but closures capture them by reference
	var raftNode *raft.Node
	var eventStreamServer *EventStreamServer

	// DEPRECATED: Deployment handler has been moved to deployment_engine UMC
	// Deployments are now handled by the deployment_engine UMC via syscalls
	// See: umcs/deployment_engine/ and internal/deployment/DEPRECATED.md
	// deploymentHandler := deployment.NewDeploymentHandler(serverID.(string), cplaneClient.Deployments)
	// deploymentHandler.SetDrainer(drainer)

	// Register Mycelium Spine handlers (replaces event bus subscriptions)
	if spineClient != nil {
		// Handle server-data-update (push config updates from control plane)
		spineClient.Register("server-data-update", func(ctx context.Context, msg spine.Message) {
			policyManager.HandlePushUpdate(ctx, msg.Payload)
		})

		// Handle command run requests from server API
		spineClient.Register("commands.run.server.request", func(ctx context.Context, msg spine.Message) {
			commandHandler.HandleCommandEvent(ctx, msg.Payload)
		})

		// Handle log collection requests from server API
		spineClient.Register("logs.collect.server.request", func(ctx context.Context, msg spine.Message) {
			logCollector.HandleEvent(ctx, msg.Payload)
		})

		// Handle service/container log collection requests from server API
		spineClient.Register("logs.collect.service.request", func(ctx context.Context, msg spine.Message) {
			if serviceLogCollector == nil {
				slog.Warn("service log collection request received but collector is not available (Docker unavailable?)")
				return
			}
			serviceLogCollector.HandleEvent(ctx, msg.Payload)
		})

		// Handle cluster membership change events — force an immediate config reconcile
		spineClient.Register("cluster.membership.changed", func(ctx context.Context, msg spine.Message) {
			slog.Info("cluster membership changed, triggering config reconcile",
				"envelope_id", msg.EnvelopeID)
			if err := policyManager.Reconcile(ctx); err != nil {
				slog.Warn("config reconcile after membership change failed", "error", err)
			}
		})

		// Handle exposure bind requests from server API
		// These are forwarded to MMA via UA events for tunnel provisioning
		spineClient.Register("exposure.bind.request", func(ctx context.Context, msg spine.Message) {
			if err := HandleExposureBindRequested(ctx, msg, raftNode, eventStreamServer); err != nil {
				slog.Warn("failed to handle exposure bind request", "error", err)
			}
		})

		// Handle exposure unbind requests from server API
		spineClient.Register("exposure.unbind.request", func(ctx context.Context, msg spine.Message) {
			if err := HandleExposureUnbindRequested(ctx, msg, raftNode, eventStreamServer); err != nil {
				slog.Warn("failed to handle exposure unbind request", "error", err)
			}
		})

		// Handle exposure bind completion events emitted by MMA via kernel
		spineClient.Register("exposure.bind.completed", func(ctx context.Context, msg spine.Message) {
			if err := HandleExposureBindCompleted(ctx, msg, raftNode, cplaneClient.Exposures); err != nil {
				slog.Warn("failed to handle exposure bind completed", "error", err)
			}
		})

		// Handle exposure unbind completion events emitted by MMA via kernel
		spineClient.Register("exposure.unbind.completed", func(ctx context.Context, msg spine.Message) {
			if err := HandleExposureUnbindCompleted(ctx, msg, raftNode); err != nil {
				slog.Warn("failed to handle exposure unbind completed", "error", err)
			}
		})

		// Handle tunnel bind requests from server_api (remote agent: user created tunnel with --server)
		spineClient.Register("tunnel.bind.request", func(ctx context.Context, msg spine.Message) {
			if err := HandleTunnelBindRequested(ctx, msg, eventStreamServer); err != nil {
				slog.Warn("failed to handle tunnel bind request", "error", err)
			}
		})

		// Handle tunnel bind completion events emitted by MMA via kernel
		spineClient.Register("tunnel.bind.completed", func(ctx context.Context, msg spine.Message) {
			if err := HandleTunnelBindCompleted(ctx, msg, server, cplaneClient.Tunnels); err != nil {
				slog.Warn("failed to handle tunnel bind completed", "error", err)
			}
		})

		// Handle channel bind requests from server_api (UNDF-111 peer-to-peer relay)
		spineClient.Register("channel.bind.request", func(ctx context.Context, msg spine.Message) {
			if err := HandleChannelBindRequested(ctx, msg, eventStreamServer); err != nil {
				slog.Warn("failed to handle channel bind request", "error", err)
			}
		})

		// Handle channel bind completed events from MMA (UNDF-111)
		spineClient.Register("channel.bind.completed", func(ctx context.Context, msg spine.Message) {
			if err := HandleChannelBindCompleted(ctx, msg, serverID.(string), cplaneClient.Channels); err != nil {
				slog.Warn("failed to handle channel bind completed", "error", err)
			}
		})

		// Start the spine client (connects and begins dispatch loop)
		if err := spineClient.Start(ctx); err != nil {
			slog.Warn("failed to start Mycelium Spine client", "error", err)
			spineClient = nil
		}
	}

	// Watch for config updates to refresh command settings
	safeGo("watchConfigForCommandSettings", func() {
		watchConfigForCommandSettings(ctx, policyManager, commandHandler)
	})

	// Publish hostname and IP address to control plane
	if err := publishNetworkInfo(ctx, cplaneClient.Servers, serverID.(string)); err != nil {
		slog.Warn("failed to publish network info", "error", err)
		// Don't fail agent startup if network info publish fails
	}

	// Wire dependencies into the already-listening server
	server.SetDependencies(policyManager, snapshotClient)
	server.SetCommandHandler(commandHandler)
	server.SetSpineClient(spineClient) // exposes Spine health via /api/v1/status

	// Initialize Raft node if configured (raftNode already declared earlier for Spine handler closures)
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
		raftNode, err = initializeRaftNode(ctx, raftConfig, serverID.(string), nil, nil, clusterID)
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
			safeGo("watchConfigForRaftUpdates", func() {
				watchConfigForRaftUpdates(ctx, policyManager, raftNode, nil)
			})

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

				// Note: syscallServer and keyManager haven't been created yet at this point
				// SecretStore will be created later in the synchronous path after syscallServer setup
				raftNode, err = initializeRaftNode(ctx, raftConfig, serverID.(string), nil, nil, clusterID)
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
					safeGo("watchConfigForRaftUpdates", func() {
						watchConfigForRaftUpdates(ctx, policyManager, raftNode, nil)
					})
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
				// Note: syscallServer and keyManager will be passed as nil since they don't exist yet
				// The watcher will initialize raft when config appears, and SecretStore will be
				// created in the async path when leadership is ready
				safeGo("watchConfigForRaftInitialization", func() {
					watchConfigForRaftInitialization(ctx, policyManager, server, port, cplaneClient, serverID.(string), nil, nil)
				})
			}
		} else {
			slog.Info("could not check current snapshot, will monitor for cluster assignment")
			// Watch for Raft configuration to appear (when server is added to cluster)
			safeGo("watchConfigForRaftInitialization", func() {
				watchConfigForRaftInitialization(ctx, policyManager, server, port, cplaneClient, serverID.(string), nil, nil)
			})
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
		safeGo("watchConfigForSoftwareUpdates", func() {
			watchConfigForSoftwareUpdates(ctx, policyManager, updateManager)
		})
	}

	// Initialize UA→MMA event stream server
	// (eventStreamServer already declared earlier for Spine handler closures)
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
		// DEPRECATED: Deployment handler moved to deployment_engine UMC
		// Wire event stream into deployment handler for capability event publishing
		// deploymentHandler.SetEventPublisher(eventStreamServer)
		// slog.Info("event publisher wired into deployment handler")
	}

	// Initialize kernel syscall server for UMC communication
	var syscallServer *kernel.SyscallServer
	syscallSocketPath := "/tmp/ua_kernel.sock"

	// Create KeyManager for identity/signing operations
	keyManagerCfg := keymanager.DefaultConfig()
	keyManagerCfg.SoftwareFallback = true // Enable fallback if TPM unavailable
	keyManager, err := keymanager.New(keyManagerCfg)
	if err != nil {
		slog.Warn("failed to create key manager", "error", err)
		keyManager = nil
	}

	// Create SecretStore for secret management if Raft is available
	var secretStore *raft.SecretStore
	if raftNode != nil {
		// SecretStore requires a SealManager, which requires KeyManager
		if keyManager != nil {
			// Create SecretStore with auto-init/unseal
			// Note: Initialize() may fail if raft leadership isn't ready yet
			// In that case, the leader-election goroutine will retry after leader is elected
			secretStore, err = initAndUnsealSecretStore(ctx, raftNode, clusterID, nodeID, keyManager, slog.Default())
			if err != nil {
				slog.Warn("failed to create secret store in synchronous path", "error", err)
				// Don't fail agent startup - secret store can be initialized later
				// The async path (after leader election) will retry
				secretStore = nil
			}
		} else {
			slog.Warn("cannot create secret store: keyManager is nil")
		}
	} else {
		slog.Info("raft node not available, secret store disabled")
	}

	// Get organization ID from config
	orgID := getConfigValueStr(snapshotClient, "local.organization_id", "")
	if orgID == "" {
		// Fallback to simple config
		orgID = getConfigValueStr(configClient, "local.organization_id", "")
	}

	syscallCfg := kernel.Config{
		SocketPath:       syscallSocketPath,
		KeyManager:       keyManager,
		RaftNode:         raftNode,
		SecretStore:      secretStore,
		ExecRunner:       runner,
		SpineClient:      spineClient,
		LifecycleManager: nil, // Will be set when capability manager is created
		Supervisor:       nil, // Will be set when capability manager is created
		NodeID:           nodeID,
		OrgID:            orgID,
		ClusterID:        clusterID,
		APIClient:        cplaneClient.API(),
		ServerID:         serverID.(string),
		Logger:           slog.Default(),
	}

	syscallServer = kernel.NewSyscallServer(syscallCfg)
	if err := syscallServer.Start(); err != nil {
		slog.Warn("failed to start kernel syscall server", "error", err)
		syscallServer = nil
	} else {
		slog.Info("kernel syscall server started", "socket", syscallSocketPath)
		// If raftNode exists but secretStore is still nil (synchronous creation failed),
		// retry asynchronously after a delay to allow leadership to stabilize
		if raftNode != nil && secretStore == nil && keyManager != nil {
			slog.Info("raft node exists but secret store is nil, will retry after leader election")
			go func() {
				// Wait for leader election
				if err := raftNode.WaitForLeader(30 * time.Second); err != nil {
					slog.Warn("leader election timeout while waiting to retry secret store creation", "error", err)
					return
				}
				// Retry secret store creation with auto-init/unseal
				slog.Info("retrying secret store creation after leader election")
				retryCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				store, err := initAndUnsealSecretStore(retryCtx, raftNode, clusterID, nodeID, keyManager, slog.Default())
				if err != nil {
					slog.Warn("failed to create secret store in async retry", "error", err)
				} else {
					syscallServer.UpdateSecretStore(store)
					slog.Info("secret store created and wired in async retry after leader election")
				}
			}()
		}
	}

	// Initialize and start deployment engine UMC
	var deploymentEngine *ManagedProcess
	if syscallServer != nil {
		var exePath string

		// DEV MODE: Check for local binary override
		if devPath, ok := devDeploymentEnginePath(devConfig); ok {
			exePath = devPath
			slog.Warn("DEV MODE: using local deployment engine", "path", exePath)
		} else {
			// Standard resolution path
			homeDir, _ := os.UserHomeDir()
			// Deployment engine is installed in ~/.underleaf/bin/
			deploymentEngineExe := filepath.Join(homeDir, ".underleaf", "bin", "deployment-engine-serve")
			// Fallback paths in case standard location doesn't work
			fallbackPaths := []string{
				"/usr/local/bin/deployment-engine-serve",
				filepath.Join(filepath.Dir(os.Args[0]), "..", "deployment_engine", "serve"),
			}

			if info, err := os.Stat(deploymentEngineExe); err == nil && !info.IsDir() {
				exePath = deploymentEngineExe
			} else {
				// Try fallback paths
				for _, p := range fallbackPaths {
					if info, err := os.Stat(p); err == nil && !info.IsDir() {
						exePath = p
						break
					}
				}
			}
		}

		if exePath != "" {
			cmd := exec.Command(exePath)
			cmd.Env = append(os.Environ(),
				"KERNEL_SOCKET="+syscallSocketPath,
				"LOG_LEVEL=info",
			)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			if err := cmd.Start(); err != nil {
				slog.Warn("failed to start deployment engine", "error", err, "path", exePath)
			} else {
				deploymentEngine = &ManagedProcess{
					Name:    "deployment-engine",
					Process: cmd,
					PID:     cmd.Process.Pid,
				}
				slog.Info("deployment engine started", "pid", cmd.Process.Pid, "socket", syscallSocketPath)

				// Wait for deployment engine to be ready and verify it can communicate
				go func() {
					healthURL := "http://localhost:8080/health"
					maxRetries := 10
					for i := 0; i < maxRetries; i++ {
						time.Sleep(500 * time.Millisecond)
						resp, err := http.Get(healthURL)
						if err == nil && resp.StatusCode == http.StatusOK {
							resp.Body.Close()
							slog.Info("deployment engine health check passed", "url", healthURL)
							return
						}
						if resp != nil {
							resp.Body.Close()
						}
					}
					slog.Warn("deployment engine health check failed after retries", "url", healthURL)
				}()
			}
		} else {
			slog.Warn("deployment engine executable not found, skipping deployment engine startup")
		}
	} else {
		slog.Warn("syscall server not started, skipping deployment engine startup")
	}

	// Note: Cron engine is now managed by deployment engine supervisor
	// instead of being started directly here

	// Initialize and start MMA UMC in DEV MODE
	// In production, MMA is managed by the capability manager.
	// In dev mode with capability_registry disabled, we start it as a managed process.
	var mma *ManagedProcess
	if mmaPath, ok := devMMAPath(devConfig); ok {
		slog.Warn("DEV MODE: starting local MMA binary", "path", mmaPath)

		mmaCmd := exec.Command(mmaPath)
		// Build env: start with KERNEL_SOCKET and LOG_LEVEL, then merge dev config env vars.
		// Hyphae config (HYPHAE_ENABLED, HYPHAE_TUNNEL_ADDR) comes from the platform snapshot
		// config (hyphae_tunnel_addr delivered by server_api) or dev config as fallback.
		// File-path env vars (HYPHAE_CA_CERT_PATH etc.) are NOT injected — MMA uses
		// the kernel cert bootstrap path (IssueLocalCertificate → server_api CSR) instead.
		baseEnv := map[string]string{
			"KERNEL_SOCKET": syscallSocketPath,
			"LOG_LEVEL":     "debug",
		}
		// Inject Hyphae tunnel addr if available from platform config
		devHyphaeTunnelAddr := getConfigValueStr(snapshotClient, "hyphae_tunnel_addr", "")
		if devHyphaeTunnelAddr == "" {
			devHyphaeTunnelAddr = getConfigValueStr(simpleConfig, "hyphae.tunnel_addr", "")
		}
		if devHyphaeTunnelAddr != "" {
			baseEnv["HYPHAE_ENABLED"] = "true"
			baseEnv["HYPHAE_TUNNEL_ADDR"] = devHyphaeTunnelAddr
		}
		mergedEnv := devMMAEnv(devConfig, baseEnv)

		mmaCmd.Env = os.Environ()
		for k, v := range mergedEnv {
			mmaCmd.Env = append(mmaCmd.Env, k+"="+v)
		}
		mmaCmd.Stdout = os.Stdout
		mmaCmd.Stderr = os.Stderr

		if err := mmaCmd.Start(); err != nil {
			slog.Warn("DEV MODE: failed to start MMA", "error", err, "path", mmaPath)
		} else {
			mma = &ManagedProcess{
				Name:    "mma",
				Process: mmaCmd,
				PID:     mmaCmd.Process.Pid,
			}
			slog.Info("DEV MODE: MMA started", "pid", mmaCmd.Process.Pid)
		}
	}

	// Initialize capability manager if enabled
	// Read from snapshotClient (synced from Server API) with fallback to simpleConfig (local config.yaml)
	var capabilityManager interface{}

	// DEV MODE: Check if capability registry should be disabled
	capEnabled := getConfigValueBool(snapshotClient, "capability_registry.enabled", false)
	if !capEnabled {
		// Fallback: check local config in case snapshot hasn't synced yet
		capEnabled = getConfigValueBool(simpleConfig, "capability_registry.enabled", false)
	}

	// DEV MODE: Override with dev config if specified
	if devConfigCapReg := devCheckCapabilityRegistryEnabled(devConfig); devConfigCapReg != nil {
		capEnabled = *devConfigCapReg
		if !capEnabled {
			slog.Warn("DEV MODE: disabling capability registry per build.yaml")
		}
	}

	slog.Info("checking capability_registry config", "enabled", capEnabled)
	if capEnabled {
		slog.Info("initializing capability manager")

		// Initialize Docker client
		dockerClient, err := client.NewClientWithOpts(client.FromEnv)
		if err != nil {
			slog.Warn("failed to initialize Docker client for capability manager", "error", err)
		} else {
			// Build capability configuration from synced snapshot, falling back to local config
			homeDir, _ := os.UserHomeDir()
			capConfig := capability.Config{
				UCRSBaseURL:   getConfigValueWithFallback(snapshotClient, simpleConfig, "capability_registry.ucrs_base_url", defaults.UCRSBaseURL),
				PublicKeyPath: getConfigPathWithFallback(snapshotClient, simpleConfig, "capability_registry.public_key_path", "/etc/underleaf/ucrs_public_key.pem"),
				CacheDir:      getConfigPathWithFallback(snapshotClient, simpleConfig, "capability_registry.cache_dir", filepath.Join(homeDir, ".underleaf", "capability_cache")),
				ProviderDir:   getConfigPathWithFallback(snapshotClient, simpleConfig, "capability_registry.provider_dir", filepath.Join(homeDir, ".underleaf", "providers")),
				SyncInterval:  time.Duration(getConfigIntWithFallback(snapshotClient, simpleConfig, "capability_registry.sync_interval_seconds", 600)) * time.Second,
				Network:       getConfigValueWithFallback(snapshotClient, simpleConfig, "provider_defaults.network", "underleaf-providers"),
				MemoryLimit:   getConfigValueWithFallback(snapshotClient, simpleConfig, "provider_defaults.memory_limit", "512m"),
				CPULimit:      getConfigValueWithFallback(snapshotClient, simpleConfig, "provider_defaults.cpu_limit", "1.0"),
				TrustTier:     getConfigValueWithFallback(snapshotClient, simpleConfig, "provider_defaults.trust_tier_constraint", "certified+"),
			}

			// Create capability manager
			mgr, err := capability.NewManager(dockerClient, capConfig)
			if err != nil {
				slog.Warn("failed to create capability manager", "error", err)
			} else {
				// DEV MODE: Skip UCRS sync if requested
				var skipUCRSSyncStart bool
				if devCheckSkipUCRSSync(devConfig) {
					slog.Warn("DEV MODE: skipping UCRS sync per build.yaml")
					skipUCRSSyncStart = true
				}

				// Start capability manager (unless skipping UCRS sync)
				if !skipUCRSSyncStart {
					if err := mgr.Start(ctx); err != nil {
						slog.Warn("failed to start capability manager", "error", err)
					} else {
						slog.Info("capability manager started successfully")
						capabilityManager = mgr
						server.SetCapabilityManager(capabilityManager)

						// Wire lifecycle manager and supervisor into syscall server
						if syscallServer != nil {
							syscallServer.WireCapabilityManager(mgr)
							slog.Info("capability manager wired into kernel syscall server")
						}

						// DEPRECATED: Recipe reconciler and deployment handler moved to deployment_engine UMC
						// Capability operations are now requested via UA-K syscalls
						// Wire the recipe reconciler into the deployment handler
						// so deployments with capability_requirements are handled
						// recipeDataDir := filepath.Join(homeDir, ".underleaf")
						// recipeReconciler := recipe.NewReconciler(capabilityManager.(*capability.Manager), recipeDataDir)
						// deploymentHandler.SetRecipeReconciler(recipeReconciler)
						// slog.Info("recipe reconciler wired into deployment handler")

						// Auto-install MMA if enabled and not already installed
						autoInstall := getConfigValueBool(snapshotClient, "capability_registry.auto_install_mma", false)
						if !autoInstall {
							autoInstall = getConfigValueBool(simpleConfig, "capability_registry.auto_install_mma", true)
						}
						if autoInstall {
							mmaProviderID := getConfigValueWithFallback(snapshotClient, simpleConfig, "capability_registry.mma_provider_id", "underleaf.mma")

							// Inject Hyphae config from platform into MMA's env before starting.
							// The tunnel address comes from server_api's configuration payload
							// (hyphae_tunnel_addr field), not from scripts or local config files.
							// No file-path env vars are injected — MMA bootstraps its cert via
							// the kernel IssueLocalCertificate path (UA-K → server_api CSR endpoint).
							hyphaeTunnelAddr := getConfigValueStr(snapshotClient, "hyphae_tunnel_addr", "")
							if hyphaeTunnelAddr == "" {
								hyphaeTunnelAddr = getConfigValueStr(simpleConfig, "hyphae.tunnel_addr", "")
							}
							if hyphaeTunnelAddr != "" {
								mgr.SetProviderEnvOverride(mmaProviderID, map[string]string{
									"HYPHAE_ENABLED":     "true",
									"HYPHAE_TUNNEL_ADDR": hyphaeTunnelAddr,
								})
								slog.Info("Hyphae env configured for MMA", "provider_id", mmaProviderID, "tunnel_addr", hyphaeTunnelAddr)
							} else {
								slog.Warn("hyphae_tunnel_addr not found in platform config; MMA will start with Hyphae disabled",
									"hint", "ensure server_api config has hyphae.tunnel_endpoint set")
							}

							go ensureMMAInstalled(ctx, mgr, mmaProviderID)
						}

						// Auto-install Deployment Engine if enabled and not already installed
						autoInstallDE := getConfigValueBool(snapshotClient, "capability_registry.auto_install_deployment_engine", false)
						if !autoInstallDE {
							autoInstallDE = getConfigValueBool(simpleConfig, "capability_registry.auto_install_deployment_engine", true)
						}
						if autoInstallDE {
							deProviderID := getConfigValueWithFallback(snapshotClient, simpleConfig, "capability_registry.deployment_engine_provider_id", "underleaf.deployment-engine")
							go ensureProviderInstalled(ctx, mgr, deProviderID, "Deployment Engine")
						}

						// Auto-install Cron Engine if enabled and not already installed
						autoInstallCE := getConfigValueBool(snapshotClient, "capability_registry.auto_install_cron_engine", false)
						if !autoInstallCE {
							autoInstallCE = getConfigValueBool(simpleConfig, "capability_registry.auto_install_cron_engine", true)
						}
						if autoInstallCE {
							ceProviderID := getConfigValueWithFallback(snapshotClient, simpleConfig, "capability_registry.cron_engine_provider_id", "underleaf.cron-engine")
							go ensureProviderInstalled(ctx, mgr, ceProviderID, "Cron Engine")
						}

						// Auto-install MCP Server if enabled.
						// launch_mode: on-demand in UCRS means InstallProviderByID places the binary
						// on disk but does NOT start it as a daemon — the IDE invokes it via stdio.
						autoInstallMCP := getConfigValueBool(snapshotClient, "capability_registry.auto_install_mcp_server", false)
						if !autoInstallMCP {
							autoInstallMCP = getConfigValueBool(simpleConfig, "capability_registry.auto_install_mcp_server", true)
						}
						if autoInstallMCP {
							mcpProviderID := getConfigValueWithFallback(snapshotClient, simpleConfig, "capability_registry.mcp_server_provider_id", "underleaf.mcp-server")
							go ensureProviderInstalled(ctx, mgr, mcpProviderID, "MCP Server")
						}
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
		SpineClient:       spineClient, // Store spine client for cleanup
		UpdateManager:     updateManager,
		CommandDrainer:    drainer,
		RaftNode:          raftNode,
		SyscallServer:     syscallServer,
		KeyManager:        keyManager,
		EventStreamServer: eventStreamServer,
		CapabilityManager: capabilityManager,
		DeploymentEngine:  deploymentEngine,
		CronEngine:        nil, // Managed by deployment engine supervisor
		MMA:               mma,
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

	if spineEndpoint, ok := cliConfig.Get("mycelium_spine.endpoint"); ok && spineEndpoint != "" {
		meta.MyceliumSpine.Endpoint = spineEndpoint.(string)
	}

	// Save updated metadata
	return store.SaveLocalMeta(meta)
}

// getCommandSettingsFromConfig extracts command settings from config snapshot
func getCommandSettingsFromConfig(config policy_manager.ConfigClient) execpkg.CommandSettings {
	settings := execpkg.DefaultCommandSettings()

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

// watchConfigForCommandSettings watches for config updates and refreshes command settings
func watchConfigForCommandSettings(ctx context.Context, manager policy_manager.PolicyManager, handler *execpkg.CommandHandler) {
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
func extractCommandSettingsFromSnapshot(snapshot *policy_manager.PolicySnapshot) execpkg.CommandSettings {
	settings := execpkg.DefaultCommandSettings()

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

// initAndUnsealSecretStore creates a SecretStore with automatic initialization and unsealing
func initAndUnsealSecretStore(ctx context.Context, raftNode *raft.Node, clusterID, nodeID string, keyManager keymanager.KeyManager, logger *slog.Logger) (*raft.SecretStore, error) {
	if raftNode == nil {
		logger.Warn("cannot create secret store: raftNode is nil")
		return nil, fmt.Errorf("raftNode is nil")
	}
	if keyManager == nil {
		logger.Warn("cannot create secret store: keyManager is nil")
		return nil, fmt.Errorf("keyManager is nil")
	}

	logger.Info("creating secret store", "clusterID", clusterID, "nodeID", nodeID)

	// Create SealManager with default config (includes RandomReader)
	kmConfig := keymanager.DefaultConfig()
	kmConfig.BackendType = keymanager.BackendSoftware
	kmConfig.SoftwareFallback = true
	sealManager, err := raft.NewSealManager(raftNode, clusterID, nodeID, kmConfig)
	if err != nil {
		logger.Warn("failed to create seal manager", "error", err)
		return nil, fmt.Errorf("failed to create seal manager: %w", err)
	}

	// Auto-initialize if not already initialized (fresh cluster)
	if !sealManager.IsInitialized() {
		logger.Info("seal manager not initialized, initializing now")
		// Initialize with timeout to handle raft leadership wait
		initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if _, err := sealManager.Initialize(initCtx); err != nil {
			logger.Warn("failed to initialize seal manager", "error", err)
			// Don't return error - seal manager exists but is sealed/uninitialized
			// Can be initialized later via HTTP API or when leadership is ready
		} else {
			logger.Info("seal manager initialized successfully")
		}
	}

	// Auto-unseal if sealed (returning node or post-initialization)
	if sealManager.IsSealed() {
		logger.Info("seal manager is sealed, attempting auto-unseal")
		unsealCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := sealManager.AutoUnseal(unsealCtx); err != nil {
			logger.Warn("failed to auto-unseal seal manager", "error", err)
			// Don't return error - secret store can still be created
			// Operations will fail with ErrSealed/ErrNotInitialized until unsealed
		} else {
			logger.Info("seal manager auto-unsealed successfully")
		}
	}

	// Create SecretStore
	secretStore := raft.NewSecretStore(raftNode, sealManager)
	logger.Info("secret store created successfully",
		"initialized", sealManager.IsInitialized(),
		"sealed", sealManager.IsSealed())

	return secretStore, nil
}

// initializeRaftNode creates and starts a Raft cluster node
func initializeRaftNode(ctx context.Context, config *raft.NodeConfig, serverIDStr string, syscallServer *kernel.SyscallServer, keyManager keymanager.KeyManager, clusterID string) (*raft.Node, error) {
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

			// Create SecretStore now that raft is ready (with init/unseal)
			if syscallServer != nil && keyManager != nil {
				secretStore, err := initAndUnsealSecretStore(context.Background(), node, clusterID, serverIDStr, keyManager, logger)
				if err != nil {
					logger.Warn("failed to create secret store after leader election", "error", err)
				} else {
					syscallServer.UpdateSecretStore(secretStore)
					logger.Info("secret store initialized and wired after leader election")
				}
			} else {
				logger.Warn("cannot create secret store after leader election: syscallServer or keyManager is nil",
					"syscallServer_nil", syscallServer == nil,
					"keyManager_nil", keyManager == nil)
			}
		}
	}()

	return node, nil
}

// watchConfigForRaftInitialization monitors config changes and initializes Raft when server is added to cluster
func watchConfigForRaftInitialization(ctx context.Context, manager policy_manager.PolicyManager, server *Server, port int, cplaneClient *controlplane.CPlaneClient, serverID string, syscallServer *kernel.SyscallServer, keyManager keymanager.KeyManager) {
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

			// Extract cluster_id for raft initialization
			var clusterID string
			if raftVal, ok := snapshot.Payload["raft"]; ok {
				if raftMap, ok := raftVal.(map[string]interface{}); ok {
					clusterID, _ = raftMap["cluster_id"].(string)
				}
			}

			// Initialize Raft node (with syscallServer and keyManager for async SecretStore creation)
			raftNode, err := initializeRaftNode(ctx, raftConfig, serverID, syscallServer, keyManager, clusterID)
			if err != nil {
				logger.Warn("failed to initialize raft node after cluster assignment", "error", err)
				continue
			}

			logger.Info("raft node initialized successfully after cluster assignment", "node_id", raftConfig.NodeID)

			// Wire Raft node into server
			server.SetRaftNode(raftNode)

			// Note: SecretStore creation is handled in initializeRaftNode's leader-election goroutine
			// It will auto-initialize and unseal when leadership is ready

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
			safeGo("watchConfigForRaftUpdates", func() {
				watchConfigForRaftUpdates(ctx, manager, raftNode, logger)
			})

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
	slog.Info("ensuring MMA is installed and running", "provider_id", providerID)

	// InstallProviderByID is idempotent and handles all cases:
	// - Already installed and running -> no-op
	// - Already installed but not running -> starts it
	// - Not installed -> installs and starts it
	endpoint, err := mgr.InstallProviderByID(ctx, providerID)
	if err != nil {
		slog.Error("failed to ensure MMA is running", "provider_id", providerID, "error", err)
		return
	}

	slog.Info("MMA is running", "provider_id", providerID, "state", endpoint.State)
}

// ensureProviderInstalled ensures a provider is installed and running.
// This is a generic version that works for any provider (deployment_engine, cron_engine, etc.)
func ensureProviderInstalled(ctx context.Context, mgr *capability.Manager, providerID, friendlyName string) {
	if providerID == "" {
		slog.Warn("empty provider ID, skipping", "friendly_name", friendlyName)
		return
	}
	slog.Info("ensuring provider is installed and running", "provider_id", providerID, "name", friendlyName)

	// InstallProviderByID is idempotent and handles all cases:
	// - Already installed and running -> no-op
	// - Already installed but not running -> starts it
	// - Not installed -> installs and starts it
	endpoint, err := mgr.InstallProviderByID(ctx, providerID)
	if err != nil {
		slog.Error("failed to ensure provider is running", "provider_id", providerID, "name", friendlyName, "error", err)
		return
	}

	slog.Info("provider is running", "provider_id", providerID, "name", friendlyName, "state", endpoint.State)
}
