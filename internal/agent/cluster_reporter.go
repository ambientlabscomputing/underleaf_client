package agent

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/raft"
)

const (
	// DefaultClusterReportInterval is the default interval for reporting cluster status
	DefaultClusterReportInterval = 30 * time.Second

	// MinClusterReportInterval is the minimum allowed interval
	MinClusterReportInterval = 10 * time.Second
)

// ClusterStatusReporter reports cluster membership and status to the control plane
type ClusterStatusReporter struct {
	serverID       string
	cplane         controlplane.CPlaneServerClient
	raftNode       *raft.Node
	interval       time.Duration
	stopCh         chan struct{}
	doneCh         chan struct{}
	clusterID      string // Actual cluster ID from control plane
	clusterCreated bool   // Track if we've created the cluster
	memberAdded    bool   // Track if we've added ourselves as a member
}

// NewClusterStatusReporter creates a new cluster status reporter
func NewClusterStatusReporter(serverID string, clusterID string, cplane controlplane.CPlaneServerClient, raftNode *raft.Node, interval time.Duration) *ClusterStatusReporter {
	if interval < MinClusterReportInterval {
		interval = DefaultClusterReportInterval
	}

	return &ClusterStatusReporter{
		serverID:  serverID,
		clusterID: clusterID,
		cplane:    cplane,
		raftNode:  raftNode,
		interval:  interval,
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
}

// Start begins the cluster status reporting loop
func (r *ClusterStatusReporter) Start(ctx context.Context) {
	if r.raftNode == nil {
		slog.Debug("raft node not configured, cluster status reporter not started")
		return
	}

	slog.Info("starting cluster status reporter",
		"server_id", r.serverID,
		"interval", r.interval)

	go r.run(ctx)
}

// Stop stops the cluster status reporting loop
func (r *ClusterStatusReporter) Stop() {
	close(r.stopCh)
	<-r.doneCh
	slog.Info("cluster status reporter stopped")
}

func (r *ClusterStatusReporter) run(ctx context.Context) {
	defer close(r.doneCh)
	defer func() {
		if r := recover(); r != nil {
			slog.Error("cluster status reporter panic recovered",
				"panic", r,
				"stack", string(debug.Stack()))
		}
	}()

	// Report status immediately on start
	r.reportStatus(ctx)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.reportStatus(ctx)
		case <-r.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (r *ClusterStatusReporter) reportStatus(ctx context.Context) {
	// Get cluster stats
	stats, err := r.raftNode.GetStats()
	if err != nil {
		slog.Debug("failed to get raft stats", "error", err)
		return
	}

	// Use the configured cluster ID
	clusterID := r.clusterID
	if clusterID == "" {
		slog.Debug("no cluster ID configured, skipping report")
		return
	}

	// Log cluster membership on first report (using closure to track state)
	slog.Info("reporting cluster status",
		"cluster_id", clusterID,
		"node_id", r.raftNode.ID(),
		"role", stats.Role)

	// Prepare cluster status payload
	role := string(stats.Role)
	leaderID := stats.LeaderID

	// Report to control plane
	if err := r.cplane.UpdateClusterMemberStatus(ctx, clusterID, r.serverID, role, leaderID); err != nil {
		slog.Warn("failed to report cluster status",
			"cluster_id", clusterID,
			"error", err)
		return
	}

	slog.Debug("cluster status reported successfully",
		"cluster_id", clusterID,
		"role", role,
		"leader", leaderID,
		"node_count", stats.NumPeers)
}
