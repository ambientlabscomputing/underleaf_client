package policy_manager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"
)

// PolicyManager interface for managing policy snapshots
type PolicyManager interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	GetSnapshot() (*PolicySnapshot, error)
	GetLocalMeta() (*LocalMetadata, error)
	Watch() <-chan *PolicySnapshot
	// Reconcile triggers an immediate pull-based sync from the control plane.
	// Safe to call concurrently; useful to force a refresh on external events
	// such as cluster membership changes.
	Reconcile(ctx context.Context) error
}

// SnapshotPolicyManager maintains a validated local policy snapshot from control plane
// Syncs via push (Mycelium Spine) and pull (periodic reconciliation).
type SnapshotPolicyManager struct {
	store             *Store
	controlPlane      ControlPlanePolicyClient
	serverID          string
	reconcileInterval time.Duration
	maxAge            time.Duration

	// Runtime state
	currentSnapshot *PolicySnapshot
	currentMeta     *LocalMetadata
	snapshotMu      sync.RWMutex

	// Watch channels
	watchChans []chan *PolicySnapshot
	watchMu    sync.Mutex

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// ControlPlanePolicyClient interface for fetching policy from control plane
type ControlPlanePolicyClient interface {
	GetServerConfig(ctx context.Context, serverID string) (map[string]interface{}, int, error)
}

// SnapshotPolicyManagerConfig configures the manager
type SnapshotPolicyManagerConfig struct {
	Store             *Store
	ControlPlane      ControlPlanePolicyClient
	ServerID          string
	ReconcileInterval time.Duration
	MaxAge            time.Duration
}

// NewSnapshotPolicyManager creates a new snapshot-based policy manager
func NewSnapshotPolicyManager(config SnapshotPolicyManagerConfig) *SnapshotPolicyManager {
	if config.ReconcileInterval == 0 {
		config.ReconcileInterval = 5 * time.Minute
	}
	if config.MaxAge == 0 {
		config.MaxAge = 24 * time.Hour
	}

	return &SnapshotPolicyManager{
		store:             config.Store,
		controlPlane:      config.ControlPlane,
		serverID:          config.ServerID,
		reconcileInterval: config.ReconcileInterval,
		maxAge:            config.MaxAge,
		watchChans:        make([]chan *PolicySnapshot, 0),
	}
}

// Start begins the config sync lifecycle
func (m *SnapshotPolicyManager) Start(ctx context.Context) error {
	m.ctx, m.cancel = context.WithCancel(ctx)

	// Load existing snapshot from disk
	if err := m.loadFromDisk(); err != nil {
		slog.Warn("failed to load snapshot from disk", "error", err)
	}

	// Initial sync
	if err := m.reconcile(m.ctx); err != nil {
		slog.Warn("initial reconciliation failed", "error", err)
	}

	// Start periodic reconciliation (pull updates)
	m.wg.Add(1)
	go m.periodicReconcile()

	slog.Info("config manager started", "server_id", m.serverID, "reconcile_interval", m.reconcileInterval)
	return nil
}

// Stop gracefully stops the config manager
func (m *SnapshotPolicyManager) Stop(ctx context.Context) error {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()

	// Close all watch channels
	m.watchMu.Lock()
	for _, ch := range m.watchChans {
		close(ch)
	}
	m.watchChans = nil
	m.watchMu.Unlock()

	slog.Info("config manager stopped", "server_id", m.serverID)
	return nil
}

// GetSnapshot returns the current config snapshot
func (m *SnapshotPolicyManager) GetSnapshot() (*PolicySnapshot, error) {
	m.snapshotMu.RLock()
	defer m.snapshotMu.RUnlock()

	if m.currentSnapshot == nil {
		return nil, fmt.Errorf("no snapshot available")
	}

	// Check if stale
	if m.currentSnapshot.IsStale(m.maxAge) {
		slog.Warn("serving stale snapshot",
			"age", m.currentSnapshot.Age(),
			"max_age", m.maxAge,
			"server_id", m.serverID)
	}

	// Return a copy to prevent external mutations
	snapshot := *m.currentSnapshot
	return &snapshot, nil
}

// GetLocalMeta returns the local metadata
func (m *SnapshotPolicyManager) GetLocalMeta() (*LocalMetadata, error) {
	m.snapshotMu.RLock()
	defer m.snapshotMu.RUnlock()

	if m.currentMeta == nil {
		return &LocalMetadata{}, nil
	}

	meta := *m.currentMeta
	return &meta, nil
}

// Watch returns a channel that receives snapshot updates
func (m *SnapshotPolicyManager) Watch() <-chan *PolicySnapshot {
	m.watchMu.Lock()
	defer m.watchMu.Unlock()

	ch := make(chan *PolicySnapshot, 10)
	m.watchChans = append(m.watchChans, ch)
	return ch
}

// HandlePushUpdate processes a raw server-data-update payload received via Mycelium Spine.
// It is safe to call from multiple goroutines (e.g. a spine handler goroutine).
func (m *SnapshotPolicyManager) HandlePushUpdate(ctx context.Context, payload []byte) {
	slog.InfoContext(ctx, "received push config update", "server_id", m.serverID)

	var event struct {
		ServerID string                 `json:"server_id"`
		Version  int                    `json:"version"`
		Config   map[string]interface{} `json:"config"`
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		slog.ErrorContext(ctx, "failed to parse push config update", "error", err)
		return
	}
	if err := m.updateSnapshot(event.Version, event.Config); err != nil {
		slog.ErrorContext(ctx, "failed to apply push config update", "error", err)
	}
}

// periodicReconcile performs periodic pull-based sync
func (m *SnapshotPolicyManager) periodicReconcile() {
	defer m.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			slog.Error("periodic reconcile panic recovered",
				"panic", r,
				"stack", string(debug.Stack()))
		}
	}()

	ticker := time.NewTicker(m.reconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := m.reconcile(m.ctx); err != nil {
				slog.Warn("reconciliation failed", "error", err)
			}
		case <-m.ctx.Done():
			return
		}
	}
}

// Reconcile fetches the latest config from the control plane immediately.
// It is exported so callers (e.g. spine event handlers) can trigger an
// on-demand sync without waiting for the periodic reconciler.
func (m *SnapshotPolicyManager) Reconcile(ctx context.Context) error {
	return m.reconcile(ctx)
}

// reconcile fetches latest config from control plane
func (m *SnapshotPolicyManager) reconcile(ctx context.Context) error {
	if m.controlPlane == nil {
		return fmt.Errorf("no control plane client configured")
	}

	slog.DebugContext(ctx, "reconciling config", "server_id", m.serverID)

	config, version, err := m.controlPlane.GetServerConfig(m.ctx, m.serverID)
	if err != nil {
		return fmt.Errorf("failed to fetch config: %w", err)
	}

	return m.updateSnapshot(version, config)
}

// updateSnapshot validates and stores a new snapshot
func (m *SnapshotPolicyManager) updateSnapshot(version int, payload map[string]interface{}) error {
	// Create new snapshot
	snapshot := NewPolicySnapshot(m.serverID, version, payload)

	// Validate
	if err := snapshot.Validate(); err != nil {
		return fmt.Errorf("snapshot validation failed: %w", err)
	}

	// Check if this is actually newer
	m.snapshotMu.RLock()
	if m.currentSnapshot != nil && m.currentSnapshot.Version >= version {
		m.snapshotMu.RUnlock()
		slog.Debug("skipping older/same version", "current", m.currentSnapshot.Version, "received", version)
		return nil
	}
	m.snapshotMu.RUnlock()

	// Save to disk
	if err := m.store.SaveSnapshot(snapshot); err != nil {
		return fmt.Errorf("failed to save snapshot: %w", err)
	}

	// Update in-memory snapshot
	m.snapshotMu.Lock()
	m.currentSnapshot = snapshot
	m.snapshotMu.Unlock()

	slog.Info("config snapshot updated", "server_id", m.serverID, "version", version)

	// Notify watchers
	m.notifyWatchers(snapshot)

	return nil
}

// loadFromDisk loads snapshot and metadata from disk
func (m *SnapshotPolicyManager) loadFromDisk() error {
	snapshot, err := m.store.LoadSnapshot()
	if err != nil {
		return err
	}

	meta, err := m.store.LoadLocalMeta()
	if err != nil {
		return err
	}

	// Validate snapshot server_id matches our configured server_id
	if snapshot != nil && snapshot.ServerID != m.serverID {
		slog.Warn("loaded snapshot is for different server, ignoring stale data",
			"snapshot_server_id", snapshot.ServerID,
			"configured_server_id", m.serverID,
			"snapshot_version", snapshot.Version)
		// Don't load mismatched snapshot - force fresh fetch
		snapshot = nil
	}

	m.snapshotMu.Lock()
	m.currentSnapshot = snapshot
	m.currentMeta = meta
	m.snapshotMu.Unlock()

	if snapshot != nil {
		slog.Info("loaded snapshot from disk",
			"server_id", snapshot.ServerID,
			"version", snapshot.Version,
			"age", snapshot.Age())
	} else {
		slog.Info("no valid snapshot on disk, will fetch fresh config")
	}

	return nil
}

// notifyWatchers sends updates to all watch channels
func (m *SnapshotPolicyManager) notifyWatchers(snapshot *PolicySnapshot) {
	m.watchMu.Lock()
	defer m.watchMu.Unlock()

	for _, ch := range m.watchChans {
		select {
		case ch <- snapshot:
		default:
			// Channel full, skip
		}
	}
}
