package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/types"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

const (
	// DefaultMetricsInterval is the default interval for collecting and sending metrics
	DefaultMetricsInterval = 60 * time.Second

	// MinMetricsInterval is the minimum allowed interval
	MinMetricsInterval = 30 * time.Second
)

// MetricsCollector collects system metrics and sends them to the control plane
type MetricsCollector struct {
	serverID string
	cplane   controlplane.CPlaneServerClient
	interval time.Duration
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(serverID string, cplane controlplane.CPlaneServerClient, interval time.Duration) *MetricsCollector {
	if interval < MinMetricsInterval {
		interval = DefaultMetricsInterval
	}

	return &MetricsCollector{
		serverID: serverID,
		cplane:   cplane,
		interval: interval,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// Start begins the metrics collection loop
func (m *MetricsCollector) Start(ctx context.Context) {
	slog.Info("starting metrics collector", "server_id", m.serverID, "interval", m.interval)

	// Initialize CPU stats on startup to avoid "not implemented yet" error on first call
	// This is required for gopsutil to establish baseline CPU measurements
	slog.Debug("initializing CPU stats with warmup call")
	_, _ = cpu.Percent(0, false)
	
	go m.run(ctx)
}

// Stop stops the metrics collection loop
func (m *MetricsCollector) Stop() {
	close(m.stopCh)
	<-m.doneCh
	slog.Info("metrics collector stopped")
}

func (m *MetricsCollector) run(ctx context.Context) {
	defer close(m.doneCh)

	// Collect and send metrics immediately on start
	m.collectAndSend(ctx)

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.collectAndSend(ctx)
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

func (m *MetricsCollector) collect() (*types.MetricsUpdateRequest, error) {
	slog.Debug("metrics collection starting", "step", "begin")
	
	// Collect CPU usage
	slog.Debug("collecting CPU metrics", "step", "cpu_start", "interval", 0)
	cpuPercent, err := cpu.Percent(0, false)
	if err != nil {
		slog.Error("CPU collection failed", "error", err, "error_type", fmt.Sprintf("%T", err))
		return nil, fmt.Errorf("cpu.Percent failed: %w", err)
	}
	cpuUsage := 0.0
	if len(cpuPercent) > 0 {
		cpuUsage = cpuPercent[0]
	}
	slog.Debug("CPU metrics collected", "cpu_usage", cpuUsage, "cpu_count", len(cpuPercent))

	// Collect memory usage
	slog.Debug("collecting memory metrics", "step", "mem_start")
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		slog.Error("memory collection failed", "error", err, "error_type", fmt.Sprintf("%T", err))
		return nil, fmt.Errorf("mem.VirtualMemory failed: %w", err)
	}
	memUsage := memInfo.UsedPercent
	slog.Debug("memory metrics collected", "mem_usage", memUsage)

	// Collect disk usage (root partition)
	slog.Debug("collecting disk metrics", "step", "disk_start")
	diskInfo, err := disk.Usage("/")
	if err != nil {
		slog.Error("disk collection failed", "error", err, "error_type", fmt.Sprintf("%T", err))
		return nil, fmt.Errorf("disk.Usage failed: %w", err)
	}
	diskUsage := diskInfo.UsedPercent
	slog.Debug("disk metrics collected", "disk_usage", diskUsage)

	slog.Info("metrics collection completed successfully",
		"cpu", cpuUsage,
		"memory", memUsage,
		"disk", diskUsage)

	return &types.MetricsUpdateRequest{
		CPUUsage:    cpuUsage,
		MemoryUsage: memUsage,
		DiskUsage:   diskUsage,
	}, nil
}

func (m *MetricsCollector) send(ctx context.Context, metrics *types.MetricsUpdateRequest) error {
	return m.cplane.UpdateServerMetrics(ctx, m.serverID, *metrics)
}

// CollectOnce collects metrics once without sending (useful for testing)
func (m *MetricsCollector) CollectOnce() (*types.MetricsUpdateRequest, error) {
	return m.collect()
}
