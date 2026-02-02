package policy_manager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
}

// SnapshotPolicyManager maintains a validated local policy snapshot from control plane
// Syncs via push (event bus) and pull (periodic reconciliation)
type SnapshotPolicyManager struct {
	store             *Store
	controlPlane      ControlPlanePolicyClient
	eventBus          EventBusClient
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

// EventBusClient interface for receiving policy updates
type EventBusClient interface {
	Subscribe(ctx context.Context, topic string, targetID string, handler func(payload []byte)) error
}

// SnapshotPolicyManagerConfig configures the manager
type SnapshotPolicyManagerConfig struct {
	Store             *Store
	ControlPlane      ControlPlanePolicyClient
	EventBus          EventBusClient
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
		eventBus:          config.EventBus,
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
	if err := m.reconcile(); err != nil {
		slog.Warn("initial reconciliation failed", "error", err)
	}

	// Start event bus listener (push updates)
	if m.eventBus != nil {
		m.wg.Add(1)
		go m.listenForUpdates()
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

// listenForUpdates subscribes to event bus for push updates
func (m *SnapshotPolicyManager) listenForUpdates() {
	defer m.wg.Done()

	if m.eventBus == nil {
		slog.Debug("event bus not configured, skipping push updates")
		return
	}

	// Subscribe to server-data-update topic filtered by server ID
	err := m.eventBus.Subscribe(m.ctx, "server-data-update", m.serverID, func(payload []byte) {
		slog.Info("received config update event", "server_id", m.serverID)

		// Parse the event payload
		var event struct {
			ServerID string                 `json:"server_id"`
			Version  int                    `json:"version"`
			Config   map[string]interface{} `json:"config"`
		}

		if err := json.Unmarshal(payload, &event); err != nil {
			slog.Error("failed to parse config update event", "error", err)
			return
		}

		// Update snapshot
		if err := m.updateSnapshot(event.Version, event.Config); err != nil {
			slog.Error("failed to update snapshot from event", "error", err)
		}
	})

	if err != nil {
		slog.Error("failed to subscribe to config updates", "error", err)
		return
	}

	<-m.ctx.Done()
}

// periodicReconcile performs periodic pull-based sync
func (m *SnapshotPolicyManager) periodicReconcile() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.reconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := m.reconcile(); err != nil {
				slog.Warn("reconciliation failed", "error", err)
			}
		case <-m.ctx.Done():
			return
		}
	}
}

// reconcile fetches latest config from control plane
func (m *SnapshotPolicyManager) reconcile() error {
	if m.controlPlane == nil {
		return fmt.Errorf("no control plane client configured")
	}

	slog.Debug("reconciling config", "server_id", m.serverID)

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

	m.snapshotMu.Lock()
	m.currentSnapshot = snapshot
	m.currentMeta = meta
	m.snapshotMu.Unlock()

	if snapshot != nil {
		slog.Info("loaded snapshot from disk",
			"server_id", snapshot.ServerID,
			"version", snapshot.Version,
			"age", snapshot.Age())
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
