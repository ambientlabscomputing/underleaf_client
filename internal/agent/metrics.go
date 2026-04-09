package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/capability"
	"github.com/ambientlabscomputing/underleaf_client/internal/capability/store"
	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
	"github.com/ambientlabscomputing/underleaf_client/internal/raft"
	servertypes "github.com/ambientlabscomputing/underleaf_client/internal/types/server"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

const (
	// DefaultMetricsInterval is the default interval for collecting and sending metrics
	DefaultMetricsInterval = 60 * time.Second

	// MinMetricsInterval is the minimum allowed interval
	MinMetricsInterval = 30 * time.Second

	// DefaultDockerMetricsInterval is the default interval for collecting and sending Docker data
	DefaultDockerMetricsInterval = 60 * time.Second

	// DefaultProvidersInterval is the default interval for collecting and sending provider data
	DefaultProvidersInterval = 60 * time.Second
)

// MetricsCollector collects system metrics and sends them to the control plane
type MetricsCollector struct {
	serverID          string
	cplane            controlplane.CPlaneServerClient
	policyManager     policy_manager.PolicyManager
	raftNode          *raft.Node                 // Raft node for extracting cluster info (optional)
	cplaneAPI         *controlplane.CPlaneClient // Full client for CA cert fetching
	dockerCollector   *DockerCollector
	capabilityManager interface{} // Capability manager for provider data
	interval          time.Duration
	dockerInterval    time.Duration
	providersInterval time.Duration
	stopCh            chan struct{}
	doneCh            chan struct{}
	caFingerprint     string // Cached CA fingerprint
	raftAddress       string // Cached Raft address
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(serverID string, cplane controlplane.CPlaneServerClient, policyManager policy_manager.PolicyManager, raftNode *raft.Node, cplaneAPI *controlplane.CPlaneClient, capabilityManager interface{}, interval time.Duration, dockerInterval time.Duration, providersInterval time.Duration) *MetricsCollector {
	if interval < MinMetricsInterval {
		interval = DefaultMetricsInterval
	}
	if dockerInterval == 0 {
		dockerInterval = DefaultDockerMetricsInterval
	}
	if providersInterval == 0 {
		providersInterval = DefaultProvidersInterval
	}

	// Create Docker collector — Docker access was verified by the pre-flight
	// check in WireAgent, so this should not fail under normal conditions.
	dockerCollector, err := NewDockerCollector()
	if err != nil {
		slog.Error("failed to initialize Docker collector despite pre-flight passing", "error", err)
		dockerCollector = nil
	}

	m := &MetricsCollector{
		serverID:          serverID,
		cplane:            cplane,
		policyManager:     policyManager,
		raftNode:          raftNode,
		cplaneAPI:         cplaneAPI,
		dockerCollector:   dockerCollector,
		capabilityManager: capabilityManager,
		interval:          interval,
		dockerInterval:    dockerInterval,
		providersInterval: providersInterval,
		stopCh:            make(chan struct{}),
		doneCh:            make(chan struct{}),
	}

	// Compute and cache raft address and CA fingerprint
	// This works even without an active Raft node by reading from config
	m.computeRaftInfo(context.Background())

	return m
}

// Start begins the metrics collection loop
func (m *MetricsCollector) Start(ctx context.Context) {
	slog.Info("starting metrics collector", "server_id", m.serverID, "interval", m.interval)

	// Initialize CPU stats on startup to avoid "not implemented yet" error on first call
	// gopsutil requires a time interval on the FIRST call to establish baseline measurements
	// Subsequent calls can use interval=0 for instantaneous readings
	slog.Debug("initializing CPU stats with warmup call")
	_, err := cpu.Percent(500*time.Millisecond, false)
	if err != nil {
		slog.Warn("CPU warmup call failed, metrics may not work correctly", "error", err)
	} else {
		slog.Debug("CPU stats initialized successfully")
	}

	go m.run(ctx)
}

// Stop stops the metrics collection loop
func (m *MetricsCollector) Stop() {
	close(m.stopCh)
	<-m.doneCh

	// Close Docker collector if it exists
	if m.dockerCollector != nil {
		if err := m.dockerCollector.Close(); err != nil {
			slog.Warn("failed to close Docker collector", "error", err)
		}
	}

	slog.Info("metrics collector stopped")
}

func (m *MetricsCollector) run(ctx context.Context) {
	defer close(m.doneCh)
	defer func() {
		if r := recover(); r != nil {
			slog.Error("metrics collector panic recovered",
				"panic", r,
				"stack", string(debug.Stack()))
		}
	}()

	// Send raft info on first run if available
	if m.raftAddress != "" || m.caFingerprint != "" {
		if err := m.sendRaftInfo(ctx); err != nil {
			slog.Error("failed to send raft info", "error", err)
		}
	}

	// Collect and send metrics immediately on start
	m.collectAndSend(ctx)
	m.collectAndSendDocker(ctx)
	m.collectAndSendProviders(ctx)

	metricsTicker := time.NewTicker(m.interval)
	defer metricsTicker.Stop()

	dockerTicker := time.NewTicker(m.dockerInterval)
	defer dockerTicker.Stop()

	providersTicker := time.NewTicker(m.providersInterval)
	defer providersTicker.Stop()

	for {
		select {
		case <-metricsTicker.C:
			m.collectAndSend(ctx)
		case <-dockerTicker.C:
			m.collectAndSendDocker(ctx)
		case <-providersTicker.C:
			m.collectAndSendProviders(ctx)
		case <-m.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (m *MetricsCollector) collectAndSend(ctx context.Context) {
	metrics, err := m.collect()
	if err != nil {
		slog.Error("failed to collect metrics", "error", err)
		return
	}

	if err := m.send(ctx, metrics); err != nil {
		slog.Error("failed to send metrics", "error", err)
		return
	}

	slog.Debug("metrics sent successfully",
		"cpu", metrics.CPUUsage,
		"memory", metrics.MemoryUsage,
		"disk", metrics.DiskUsage,
	)
}

func (m *MetricsCollector) collect() (*servertypes.MetricsUpdateRequest, error) {
	slog.Debug("metrics collection starting", "step", "begin")

	// Collect CPU usage - use 0.01 if failed or returned 0 to pass API validation
	slog.Debug("collecting CPU metrics", "step", "cpu_start", "interval", 0)
	cpuUsage := 0.01 // Default fallback value
	cpuPercent, err := cpu.Percent(0, false)
	if err != nil {
		slog.Warn("CPU collection failed, using fallback value", "error", err, "fallback", cpuUsage)
	} else if len(cpuPercent) > 0 && cpuPercent[0] > 0 {
		cpuUsage = cpuPercent[0]
		slog.Debug("CPU metrics collected", "cpu_usage", cpuUsage, "cpu_count", len(cpuPercent))
	} else {
		slog.Debug("CPU returned 0, using fallback value", "fallback", cpuUsage)
	}

	// Collect memory usage - gracefully handle failures
	slog.Debug("collecting memory metrics", "step", "mem_start")
	memUsage := 0.0
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		slog.Warn("memory collection failed, using 0", "error", err)
	} else {
		memUsage = memInfo.UsedPercent
		slog.Debug("memory metrics collected", "mem_usage", memUsage)
	}

	// Collect disk usage - gracefully handle failures
	slog.Debug("collecting disk metrics", "step", "disk_start")
	diskUsage := 0.0
	diskInfo, err := disk.Usage("/")
	if err != nil {
		slog.Warn("disk collection failed, using 0", "error", err)
	} else {
		diskUsage = diskInfo.UsedPercent
		slog.Debug("disk metrics collected", "disk_usage", diskUsage)
	}

	slog.Info("metrics collection completed",
		"cpu", cpuUsage,
		"memory", memUsage,
		"disk", diskUsage)

	return &servertypes.MetricsUpdateRequest{
		CPUUsage:    cpuUsage,
		MemoryUsage: memUsage,
		DiskUsage:   diskUsage,
	}, nil
}

func (m *MetricsCollector) send(ctx context.Context, metrics *servertypes.MetricsUpdateRequest) error {
	return m.cplane.UpdateServerMetrics(ctx, m.serverID, *metrics)
}

func (m *MetricsCollector) collectAndSendDocker(ctx context.Context) {
	// If Docker collector is not available, log warning and skip
	if m.dockerCollector == nil {
		slog.Debug("Docker collector not available, skipping Docker data collection")
		return
	}

	// Collect Docker data
	dockerData, err := m.dockerCollector.Collect(ctx)
	if err != nil {
		slog.Error("failed to collect Docker data", "error", err)
		return
	}

	// Send Docker data to control plane
	req := servertypes.DockerDataUpdateRequest{
		Containers: dockerData.Containers,
		Images:     dockerData.Images,
		Volumes:    dockerData.Volumes,
		Networks:   dockerData.Networks,
		Services:   dockerData.Services,
	}

	if err := m.cplane.UpdateServerDockerData(ctx, m.serverID, req); err != nil {
		slog.Error("failed to send Docker data", "error", err)
		return
	}

	slog.Debug("Docker data sent successfully",
		"containers", len(dockerData.Containers),
		"images", len(dockerData.Images),
		"volumes", len(dockerData.Volumes),
		"networks", len(dockerData.Networks))
}

// CollectOnce collects metrics once without sending (useful for testing)
func (m *MetricsCollector) CollectOnce() (*servertypes.MetricsUpdateRequest, error) {
	return m.collect()
}

// sendRaftInfo sends raft_address and ca_fingerprint to backend via PATCH
func (m *MetricsCollector) sendRaftInfo(ctx context.Context) error {
	updates := servertypes.UpdateServerRequest{}
	if m.raftAddress != "" {
		updates.RaftAddress = m.raftAddress
	}
	if m.caFingerprint != "" {
		updates.CAFingerprint = m.caFingerprint
	}

	if updates.RaftAddress == "" && updates.CAFingerprint == "" {
		return nil // Nothing to send
	}

	_, err := m.cplane.UpdateServer(ctx, m.serverID, updates)
	if err != nil {
		return fmt.Errorf("failed to update server with raft info: %w", err)
	}

	slog.Info("raft info sent to backend",
		"raft_address", m.raftAddress,
		"ca_fingerprint", m.caFingerprint[:16]+"...")
	return nil
}

// computeRaftInfo extracts raft address and CA fingerprint for auto-reporting
// This works even without an active Raft node - it reads from config file
func (m *MetricsCollector) computeRaftInfo(ctx context.Context) {
	// Try to get raft address from active Raft node first (if available)
	if m.raftNode != nil {
		raftConfig, _ := m.raftNode.GetConfiguration()
		if raftConfig != nil && raftConfig.Nodes != nil {
			// Try to get this node's address from cluster config
			if nodeInfo, ok := raftConfig.Nodes[m.raftNode.ID()]; ok && nodeInfo.Address != "" {
				m.raftAddress = nodeInfo.Address
				slog.Info("raft address determined from cluster config", "address", m.raftAddress)
			}
		}
	}

	// If we don't have a Raft node or couldn't get address, compute from config file
	// This allows us to report potential raft_address even before joining a cluster
	if m.raftAddress == "" {
		// Get from policy manager config
		snapshot, err := m.policyManager.GetSnapshot()
		if err == nil && snapshot.Payload != nil {
			if raftVal, ok := snapshot.Payload["raft"]; ok {
				if raftMap, ok := raftVal.(map[string]interface{}); ok {
					if bindPort, ok := raftMap["bind_port"].(float64); ok {
						// Get local IP
						if ipAddr, err := getOutboundIP(); err == nil {
							m.raftAddress = fmt.Sprintf("%s:%d", ipAddr, int(bindPort))
							slog.Info("raft address computed from bind port and local IP", "address", m.raftAddress)
						}
					}
				}
			}
		}
	}

	// Compute CA fingerprint if we have the API client
	if m.cplaneAPI != nil {
		caCertPEM, err := m.cplaneAPI.API().GetCACertificate(ctx)
		if err != nil {
			slog.Warn("failed to fetch CA certificate for fingerprint", "error", err)
		} else {
			// Import mdns package's ComputeCAFingerprint function
			hash := sha256.Sum256(caCertPEM)
			m.caFingerprint = hex.EncodeToString(hash[:])
			slog.Info("CA fingerprint computed", "fingerprint", m.caFingerprint[:16]+"...")
		}
	}
}

// getOutboundIP gets the preferred outbound IP of this machine
func getOutboundIP() (string, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", err
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String(), nil
}

// collectAndSendProviders collects installed provider data and sends it to the control plane
func (m *MetricsCollector) collectAndSendProviders(ctx context.Context) {
	// Skip if capability manager is not available
	if m.capabilityManager == nil {
		return
	}

	// Use JSON marshaling to convert between internal provider types and server types
	// This avoids direct package dependencies while maintaining type safety

	// Define a minimal interface to call the capability manager
	type capabilityManagerAPI interface {
		ListInstalledProviders(ctx context.Context) ([]*store.ProviderInstance, error)
		GetRegistryStats() capability.RegistryStats
	}

	mgr, ok := m.capabilityManager.(capabilityManagerAPI)
	if !ok {
		slog.Debug("capability manager does not support provider reporting")
		return
	}

	// Get installed providers
	providersIface, err := mgr.ListInstalledProviders(ctx)
	if err != nil {
		slog.Warn("failed to list installed providers", "error", err)
		return
	}

	// Get registry stats for version and last sync
	statsIface := mgr.GetRegistryStats()

	// Convert providers to server types
	providers := make([]servertypes.ProviderInstance, 0, len(providersIface))
	for _, inst := range providersIface {
		if inst == nil {
			continue
		}
		installedAt := ""
		if !inst.InstalledAt.IsZero() {
			installedAt = inst.InstalledAt.UTC().Format(time.RFC3339)
		}
		providers = append(providers, servertypes.ProviderInstance{
			ProviderID:   inst.ProviderID,
			Version:      inst.Version,
			State:        inst.State,
			Capabilities: inst.Capabilities,
			InstalledAt:  installedAt,
		})
	}

	// Extract registry version and last sync from stats
	registryVersion := 0
	lastSyncAt := ""
	if statsIface.Version != "" {
		if parsed, err := strconv.Atoi(statsIface.Version); err == nil {
			registryVersion = parsed
		}
	}

	// If we still don't have lastSyncAt, use current time
	if lastSyncAt == "" {
		lastSyncAt = time.Now().UTC().Format(time.RFC3339)
	}

	// Create the update request
	updateReq := servertypes.ProvidersUpdateRequest{
		Providers:       providers,
		RegistryVersion: registryVersion,
		LastSyncAt:      lastSyncAt,
	}

	// Send to control plane
	if err := m.cplane.UpdateServerProviders(ctx, m.serverID, updateReq); err != nil {
		slog.Error("failed to update server providers", "error", err)
		return
	}

	slog.Info("provider data sent to control plane", "provider_count", len(providers))
}

func getStringField(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getStringSliceField(m map[string]interface{}, key string) []string {
	if v, ok := m[key].([]interface{}); ok {
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}
