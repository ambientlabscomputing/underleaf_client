package agent

import (
	"context"
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
	// Collect CPU usage (average over 1 second)
	cpuPercent, err := cpu.Percent(time.Second, false)
	if err != nil {
		return nil, err
	}
	cpuUsage := 0.0
	if len(cpuPercent) > 0 {
		cpuUsage = cpuPercent[0]
	}

	// Collect memory usage
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		return nil, err
	}
	memUsage := memInfo.UsedPercent

	// Collect disk usage (root partition)
	diskInfo, err := disk.Usage("/")
	if err != nil {
		return nil, err
	}
	diskUsage := diskInfo.UsedPercent

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
