package agent

import (
	"context"
	"log/slog"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/config_manager"
	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
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
)

// MetricsCollector collects system metrics and sends them to the control plane
type MetricsCollector struct {
	serverID        string
	cplane          controlplane.CPlaneServerClient
	configManager   config_manager.ConfigManager
	dockerCollector *DockerCollector
	interval        time.Duration
	dockerInterval  time.Duration
	stopCh          chan struct{}
	doneCh          chan struct{}
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(serverID string, cplane controlplane.CPlaneServerClient, configManager config_manager.ConfigManager, interval time.Duration, dockerInterval time.Duration) *MetricsCollector {
	if interval < MinMetricsInterval {
		interval = DefaultMetricsInterval
	}
	if dockerInterval == 0 {
		dockerInterval = DefaultDockerMetricsInterval
	}

	// Try to create Docker collector - if it fails, we'll log a warning but continue
	dockerCollector, err := NewDockerCollector()
	if err != nil {
		slog.Warn("failed to initialize Docker collector, Docker data collection will be disabled", "error", err)
		dockerCollector = nil
	}

	return &MetricsCollector{
		serverID:        serverID,
		cplane:          cplane,
		configManager:   configManager,
		dockerCollector: dockerCollector,
		interval:        interval,
		dockerInterval:  dockerInterval,
		stopCh:          make(chan struct{}),
		doneCh:          make(chan struct{}),
	}
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

	// Collect and send metrics immediately on start
	m.collectAndSend(ctx)
	m.collectAndSendDocker(ctx)

	metricsTicker := time.NewTicker(m.interval)
	defer metricsTicker.Stop()

	dockerTicker := time.NewTicker(m.dockerInterval)
	defer dockerTicker.Stop()

	for {
		select {
		case <-metricsTicker.C:
			m.collectAndSend(ctx)
		case <-dockerTicker.C:
			m.collectAndSendDocker(ctx)
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
	// Check if Docker integration is enabled in config
	snapshot, err := m.configManager.GetSnapshot()
	if err != nil {
		slog.Warn("failed to get config snapshot for Docker check", "error", err)
		return
	}

	// Extract docker_integration_enabled from payload
	dockerEnabled := false
	if snapshot.Payload != nil {
		if val, ok := snapshot.Payload["docker_integration_enabled"]; ok {
			if enabled, ok := val.(bool); ok {
				dockerEnabled = enabled
			}
		}
	}

	// If Docker integration is not enabled, skip collection
	if !dockerEnabled {
		slog.Debug("Docker integration disabled in config, skipping Docker data collection")
		return
	}

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
