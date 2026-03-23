package raft

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/crypto/keymanager"
	"github.com/rs/zerolog/log"
)

const (
	// KV paths for seal state
	sealStatusPath     = "/_system/seal/status"
	masterKeyBlobPath  = "/_system/seal/master_key_blob"
	sealInitializedKey = "/_system/seal/initialized"
)

// SealStatus represents the current seal state of the cluster
type SealStatus struct {
	Sealed      bool                   `json:"sealed"`
	Initialized bool                   `json:"initialized"`
	ClusterID   string                 `json:"cluster_id"`
	NodeID      string                 `json:"node_id"`
	Backend     keymanager.BackendInfo `json:"backend"`
	InitTime    *time.Time             `json:"init_time,omitempty"`
	UnsealTime  *time.Time             `json:"unseal_time,omitempty"`
	SealTime    *time.Time             `json:"seal_time,omitempty"`
}

// SealManager manages the seal/unseal state of the secret store
type SealManager struct {
	mu          sync.RWMutex
	node        *Node
	kv          *KV
	keyManager  keymanager.KeyManager
	clusterID   string
	nodeID      string
	initialized bool
	sealed      bool
}

// NewSealManager creates a new seal manager
func NewSealManager(node *Node, clusterID, nodeID string, kmConfig *keymanager.Config) (*SealManager, error) {
	if node == nil {
		return nil, fmt.Errorf("node cannot be nil")
	}

	// Create key manager
	km, err := keymanager.New(kmConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create key manager: %w", err)
	}

	sm := &SealManager{
		node:       node,
		kv:         NewKV(node),
		keyManager: km,
		clusterID:  clusterID,
		nodeID:     nodeID,
		sealed:     true, // Start sealed
	}

	// Check if already initialized by looking in KV store
	if err := sm.loadState(); err != nil {
		log.Warn().Err(err).Msg("Failed to load seal state, assuming uninitialized")
	}

	return sm, nil
}

// Initialize performs the initial seal ceremony
// This should only be called once per cluster
func (sm *SealManager) Initialize(ctx context.Context) (*SealStatus, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.initialized {
		return nil, keymanager.ErrAlreadyInitialized
	}

	// Wait for cluster to be ready
	if err := sm.waitForReady(ctx); err != nil {
		return nil, fmt.Errorf("cluster not ready: %w", err)
	}

	// Initialize the key manager
	masterKeyBlob, err := sm.keyManager.Initialize()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize key manager: %w", err)
	}

	// Store the master key blob in the KV store
	if err := sm.kv.Put(masterKeyBlobPath, masterKeyBlob, ""); err != nil {
		return nil, fmt.Errorf("failed to store master key blob: %w", err)
	}

	// Mark as initialized
	initData := map[string]interface{}{
		"initialized": true,
		"init_time":   time.Now().UTC(),
		"cluster_id":  sm.clusterID,
		"node_id":     sm.nodeID,
		"backend":     sm.keyManager.GetBackendInfo(),
	}
	initJSON, err := json.Marshal(initData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal init data: %w", err)
	}

	if err := sm.kv.Put(sealInitializedKey, initJSON, ""); err != nil {
		return nil, fmt.Errorf("failed to store initialization marker: %w", err)
	}

	sm.initialized = true
	sm.sealed = false

	// Update seal status
	status := sm.getStatusLocked()
	if err := sm.updateStatusLocked(status); err != nil {
		log.Error().Err(err).Msg("Failed to update seal status after initialization")
	}

	log.Info().
		Str("cluster_id", sm.clusterID).
		Str("backend", string(sm.keyManager.GetBackendInfo().Type)).
		Msg("Seal manager initialized successfully")

	return status, nil
}

// Seal locks the secret store
func (sm *SealManager) Seal(ctx context.Context) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if !sm.initialized {
		return keymanager.ErrNotInitialized
	}

	if sm.sealed {
		return nil // Already sealed
	}

	// Seal the key manager
	if err := sm.keyManager.Seal(); err != nil {
		return fmt.Errorf("failed to seal key manager: %w", err)
	}

	sm.sealed = true

	// Update seal status
	status := sm.getStatusLocked()
	if err := sm.updateStatusLocked(status); err != nil {
		log.Error().Err(err).Msg("Failed to update seal status after sealing")
	}

	log.Info().
		Str("cluster_id", sm.clusterID).
		Str("node_id", sm.nodeID).
		Msg("Seal manager sealed")

	return nil
}

// Unseal unlocks the secret store
func (sm *SealManager) Unseal(ctx context.Context) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if !sm.initialized {
		// Try to load state from KV store
		if err := sm.loadStateLocked(); err != nil {
			return fmt.Errorf("not initialized: %w", err)
		}
	}

	if !sm.sealed {
		return nil // Already unsealed
	}

	// Wait for cluster to be ready
	if err := sm.waitForReady(ctx); err != nil {
		return fmt.Errorf("cluster not ready: %w", err)
	}

	// Retrieve the master key blob from KV store.
	// Use stale read: the blob is immutable after init and replicated
	// via Raft, so followers can safely read from their local FSM.
	entry, err := sm.kv.Get(masterKeyBlobPath, ReadModeStale)
	if err != nil {
		return fmt.Errorf("failed to retrieve master key blob: %w", err)
	}
	if entry == nil {
		return fmt.Errorf("master key blob not found, cluster may not be initialized")
	}

	// Unseal the key manager
	if err := sm.keyManager.Unseal(entry.Value); err != nil {
		return fmt.Errorf("failed to unseal key manager: %w", err)
	}

	sm.sealed = false

	// Update seal status
	status := sm.getStatusLocked()
	if err := sm.updateStatusLocked(status); err != nil {
		log.Error().Err(err).Msg("Failed to update seal status after unsealing")
	}

	log.Info().
		Str("cluster_id", sm.clusterID).
		Str("node_id", sm.nodeID).
		Msg("Seal manager unsealed")

	return nil
}

// AutoUnseal attempts to automatically unseal if possible
// This is called on node startup
func (sm *SealManager) AutoUnseal(ctx context.Context) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check if initialized
	if err := sm.loadStateLocked(); err != nil {
		log.Debug().Msg("Seal manager not initialized, skipping auto-unseal")
		return nil // Not an error, just not initialized
	}

	if !sm.sealed {
		return nil // Already unsealed
	}

	log.Info().Msg("Attempting auto-unseal")

	// Unlock for unseal operation
	sm.mu.Unlock()
	err := sm.Unseal(ctx)
	sm.mu.Lock()

	if err != nil {
		log.Error().Err(err).Msg("Auto-unseal failed")
		return err
	}

	log.Info().Msg("Auto-unseal successful")
	return nil
}

// Status returns the current seal status
func (sm *SealManager) Status(ctx context.Context) (*SealStatus, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Re-check Raft KV if not yet initialized — the leader may have
	// initialized since this node's SealManager was created.
	if !sm.initialized {
		_ = sm.loadStateLocked() // best-effort refresh
	}

	return sm.getStatusLocked(), nil
}

// IsSealed returns true if the seal manager is sealed
func (sm *SealManager) IsSealed() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.sealed
}

// IsInitialized returns true if the seal manager is initialized
func (sm *SealManager) IsInitialized() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.initialized
}

// KeyManager returns the underlying key manager
// This should only be used by SecretStore
func (sm *SealManager) KeyManager() keymanager.KeyManager {
	return sm.keyManager
}

// Close closes the seal manager
func (sm *SealManager) Close() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.keyManager != nil {
		return sm.keyManager.Close()
	}
	return nil
}

// getStatusLocked returns the current status (must hold lock)
func (sm *SealManager) getStatusLocked() *SealStatus {
	status := &SealStatus{
		Sealed:      sm.sealed,
		Initialized: sm.initialized,
		ClusterID:   sm.clusterID,
		NodeID:      sm.nodeID,
		Backend:     sm.keyManager.GetBackendInfo(),
	}

	// Try to get init time from KV store
	if sm.initialized {
		entry, err := sm.kv.Get(sealInitializedKey, ReadModeStale)
		if err == nil && entry != nil {
			var initData map[string]interface{}
			if err := json.Unmarshal(entry.Value, &initData); err == nil {
				if initTimeStr, ok := initData["init_time"].(string); ok {
					if initTime, err := time.Parse(time.RFC3339, initTimeStr); err == nil {
						status.InitTime = &initTime
					}
				}
			}
		}
	}

	now := time.Now().UTC()
	if sm.sealed {
		status.SealTime = &now
	} else {
		status.UnsealTime = &now
	}

	return status
}

// updateStatusLocked updates the seal status in the KV store (must hold lock)
func (sm *SealManager) updateStatusLocked(status *SealStatus) error {
	statusJSON, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("failed to marshal status: %w", err)
	}

	if err := sm.kv.Put(sealStatusPath, statusJSON, ""); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	return nil
}

// loadState loads the seal state from the KV store
func (sm *SealManager) loadState() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.loadStateLocked()
}

// loadStateLocked loads the seal state from the KV store (must hold lock)
func (sm *SealManager) loadStateLocked() error {
	// Wait for KV to be available
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := sm.waitForReady(ctx); err != nil {
		return err
	}

	// Check if initialized
	entry, err := sm.kv.Get(sealInitializedKey, ReadModeStale)
	if err != nil {
		return fmt.Errorf("failed to check initialization: %w", err)
	}
	if entry == nil {
		return keymanager.ErrNotInitialized
	}

	var initData map[string]interface{}
	if err := json.Unmarshal(entry.Value, &initData); err != nil {
		return fmt.Errorf("failed to unmarshal init data: %w", err)
	}

	initialized, ok := initData["initialized"].(bool)
	if !ok || !initialized {
		return keymanager.ErrNotInitialized
	}

	sm.initialized = true
	sm.sealed = sm.keyManager.IsSealed()

	log.Debug().
		Bool("initialized", sm.initialized).
		Bool("sealed", sm.sealed).
		Msg("Loaded seal state from KV store")

	return nil
}

// waitForReady waits for the cluster to be ready.
// Any node (leader or follower) is considered ready once it has applied
// at least one Raft log entry, indicating the FSM is populated.
func (sm *SealManager) waitForReady(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if sm.node.raft != nil && sm.node.raft.AppliedIndex() > 0 {
				time.Sleep(100 * time.Millisecond)
				return nil
			}
		}
	}
}
