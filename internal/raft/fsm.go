package raft

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/raft"
)

// FSMCommand represents a command to be applied to the FSM.
type FSMCommand struct {
	Op    string `json:"op"`    // Operation: "put", "delete"
	Key   string `json:"key"`   // Key to operate on
	Value []byte `json:"value"` // Value for put operations
	Lease string `json:"lease"` // Optional lease ID
}

// FSMSnapshot implements raft.FSMSnapshot.
type FSMSnapshot struct {
	data []byte
}

// Persist writes the snapshot to the sink.
func (s *FSMSnapshot) Persist(sink raft.SnapshotSink) error {
	if _, err := sink.Write(s.data); err != nil {
		sink.Cancel()
		return err
	}
	return sink.Close()
}

// Release is called when the snapshot is no longer needed.
func (s *FSMSnapshot) Release() {}

// KVStateMachine implements raft.FSM for the KV store.
type KVStateMachine struct {
	mu sync.RWMutex

	// data is the in-memory KV store
	data map[string]*KVEntry

	// revision is the global monotonic counter
	revision uint64

	// maxValueSize is the maximum size for a single value
	maxValueSize int

	// maxStorageSize is the maximum total storage budget
	maxStorageSize int64

	// currentSize tracks the current total storage size
	currentSize int64

	// leases tracks active leases and their associated keys
	leases map[string]*LeaseInfo

	// logger for structured logging
	logger *slog.Logger
}

// NewKVStateMachine creates a new KV state machine.
func NewKVStateMachine(maxValueSize int, maxStorageSize int64, logger *slog.Logger) *KVStateMachine {
	return &KVStateMachine{
		data:           make(map[string]*KVEntry),
		revision:       0,
		maxValueSize:   maxValueSize,
		maxStorageSize: maxStorageSize,
		currentSize:    0,
		leases:         make(map[string]*LeaseInfo),
		logger:         logger,
	}
}

// Apply applies a Raft log entry to the FSM.
func (fsm *KVStateMachine) Apply(log *raft.Log) interface{} {
	fsm.mu.Lock()
	defer fsm.mu.Unlock()

	var cmd FSMCommand
	if err := json.Unmarshal(log.Data, &cmd); err != nil {
		fsm.logger.Error("failed to unmarshal FSM command", "error", err)
		return err
	}

	// Increment revision for any state change
	fsm.revision++

	switch cmd.Op {
	case "put":
		return fsm.applyPut(cmd.Key, cmd.Value, cmd.Lease)
	case "delete":
		return fsm.applyDelete(cmd.Key)
	case "grant_lease":
		return fsm.applyGrantLease(cmd.Key, cmd.Value)
	case "revoke_lease":
		return fsm.applyRevokeLease(cmd.Key)
	case "attach_lease":
		return fsm.applyAttachLease(cmd.Key, cmd.Lease)
	default:
		fsm.logger.Warn("unknown FSM command", "op", cmd.Op)
		return fmt.Errorf("unknown command: %s", cmd.Op)
	}
}

// applyPut applies a put operation.
func (fsm *KVStateMachine) applyPut(key string, value []byte, leaseID string) error {
	// Validate key format
	if !isValidKey(key) {
		return ErrInvalidKey
	}

	// Check value size
	if len(value) > fsm.maxValueSize {
		return ErrValueTooLarge
	}

	now := time.Now()
	var entry *KVEntry

	// Check if key exists
	if existing, exists := fsm.data[key]; exists {
		// Update existing entry
		oldSize := len(existing.Value)
		newSize := len(value)
		fsm.currentSize = fsm.currentSize - int64(oldSize) + int64(newSize)

		entry = existing
		entry.Value = value
		entry.ModifiedRevision = fsm.revision
		entry.ModTime = now
		entry.Version++
	} else {
		// Check storage budget for new key
		newSize := int64(len(key) + len(value))
		if fsm.currentSize+newSize > fsm.maxStorageSize {
			return ErrStorageFull
		}

		// Create new entry
		entry = &KVEntry{
			Key:              key,
			Value:            value,
			Revision:         fsm.revision,
			CreatedRevision:  fsm.revision,
			ModifiedRevision: fsm.revision,
			Version:          1,
			CreateTime:       now,
			ModTime:          now,
		}
		fsm.data[key] = entry
		fsm.currentSize += newSize
	}

	// Attach to lease if specified
	if leaseID != "" {
		if lease, exists := fsm.leases[leaseID]; exists {
			lease.Keys = append(lease.Keys, key)
		}
	}

	fsm.logger.Debug("applied put", "key", key, "revision", fsm.revision, "size", len(value))
	return nil
}

// applyDelete applies a delete operation.
func (fsm *KVStateMachine) applyDelete(key string) error {
	entry, exists := fsm.data[key]
	if !exists {
		return ErrKeyNotFound
	}

	// Update storage size
	size := int64(len(key) + len(entry.Value))
	fsm.currentSize -= size

	delete(fsm.data, key)

	fsm.logger.Debug("applied delete", "key", key, "revision", fsm.revision)
	return nil
}

// applyGrantLease creates a new lease.
func (fsm *KVStateMachine) applyGrantLease(leaseID string, ttlBytes []byte) error {
	var ttl int64
	if err := json.Unmarshal(ttlBytes, &ttl); err != nil {
		return err
	}

	now := time.Now()
	lease := &LeaseInfo{
		ID:        leaseID,
		TTL:       ttl,
		GrantedAt: now,
		ExpiresAt: now.Add(time.Duration(ttl) * time.Second),
		Keys:      make([]string, 0),
	}

	fsm.leases[leaseID] = lease
	fsm.logger.Debug("granted lease", "lease_id", leaseID, "ttl", ttl)
	return nil
}

// applyRevokeLease revokes a lease and deletes all associated keys.
func (fsm *KVStateMachine) applyRevokeLease(leaseID string) error {
	lease, exists := fsm.leases[leaseID]
	if !exists {
		return ErrLeaseNotFound
	}

	// Delete all keys attached to this lease
	for _, key := range lease.Keys {
		fsm.applyDelete(key)
	}

	delete(fsm.leases, leaseID)
	fsm.logger.Debug("revoked lease", "lease_id", leaseID, "keys_deleted", len(lease.Keys))
	return nil
}

// applyAttachLease attaches a key to a lease.
func (fsm *KVStateMachine) applyAttachLease(key, leaseID string) error {
	lease, exists := fsm.leases[leaseID]
	if !exists {
		return ErrLeaseNotFound
	}

	// Check if key exists
	if _, exists := fsm.data[key]; !exists {
		return ErrKeyNotFound
	}

	lease.Keys = append(lease.Keys, key)
	fsm.logger.Debug("attached key to lease", "key", key, "lease_id", leaseID)
	return nil
}

// Snapshot creates a snapshot of the current state.
func (fsm *KVStateMachine) Snapshot() (raft.FSMSnapshot, error) {
	fsm.mu.RLock()
	defer fsm.mu.RUnlock()

	// Create snapshot data structure
	snapshot := struct {
		Revision uint64                `json:"revision"`
		Data     map[string]*KVEntry   `json:"data"`
		Leases   map[string]*LeaseInfo `json:"leases"`
	}{
		Revision: fsm.revision,
		Data:     fsm.data,
		Leases:   fsm.leases,
	}

	// Serialize to JSON
	data, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}

	fsm.logger.Info("created snapshot", "revision", fsm.revision, "keys", len(fsm.data), "size_bytes", len(data))

	return &FSMSnapshot{data: data}, nil
}

// Restore restores the FSM from a snapshot.
func (fsm *KVStateMachine) Restore(snapshot io.ReadCloser) error {
	defer snapshot.Close()

	// Read all snapshot data
	data, err := io.ReadAll(snapshot)
	if err != nil {
		return err
	}

	// Deserialize snapshot
	var snapshotData struct {
		Revision uint64                `json:"revision"`
		Data     map[string]*KVEntry   `json:"data"`
		Leases   map[string]*LeaseInfo `json:"leases"`
	}

	if err := json.Unmarshal(data, &snapshotData); err != nil {
		return err
	}

	// Restore state
	fsm.mu.Lock()
	defer fsm.mu.Unlock()

	fsm.revision = snapshotData.Revision
	fsm.data = snapshotData.Data
	fsm.leases = snapshotData.Leases

	// Recalculate storage size
	fsm.currentSize = 0
	for key, entry := range fsm.data {
		fsm.currentSize += int64(len(key) + len(entry.Value))
	}

	fsm.logger.Info("restored from snapshot", "revision", fsm.revision, "keys", len(fsm.data), "size_bytes", fsm.currentSize)

	return nil
}

// Get retrieves a value by key (for local reads).
func (fsm *KVStateMachine) Get(key string) (*KVEntry, error) {
	fsm.mu.RLock()
	defer fsm.mu.RUnlock()

	entry, exists := fsm.data[key]
	if !exists {
		return nil, ErrKeyNotFound
	}

	// Return a copy to avoid concurrent modification
	entryCopy := *entry
	return &entryCopy, nil
}

// List retrieves all keys with a given prefix.
func (fsm *KVStateMachine) List(prefix string) ([]*KVEntry, error) {
	fsm.mu.RLock()
	defer fsm.mu.RUnlock()

	var entries []*KVEntry
	for key, entry := range fsm.data {
		if strings.HasPrefix(key, prefix) {
			entryCopy := *entry
			entries = append(entries, &entryCopy)
		}
	}

	return entries, nil
}

// GetRevision returns the current revision.
func (fsm *KVStateMachine) GetRevision() uint64 {
	fsm.mu.RLock()
	defer fsm.mu.RUnlock()
	return fsm.revision
}

// GetLease returns information about a lease.
func (fsm *KVStateMachine) GetLease(leaseID string) (*LeaseInfo, error) {
	fsm.mu.RLock()
	defer fsm.mu.RUnlock()

	lease, exists := fsm.leases[leaseID]
	if !exists {
		return nil, ErrLeaseNotFound
	}

	// Return a copy
	leaseCopy := *lease
	return &leaseCopy, nil
}

// GetStats returns statistics about the FSM state.
func (fsm *KVStateMachine) GetStats() (revision uint64, keyCount int, storageSize int64) {
	fsm.mu.RLock()
	defer fsm.mu.RUnlock()

	return fsm.revision, len(fsm.data), fsm.currentSize
}

// isValidKey checks if a key follows the hierarchical format.
func isValidKey(key string) bool {
	// Key must start with /
	if !strings.HasPrefix(key, "/") {
		return false
	}

	// Must have at least one segment after /
	parts := strings.Split(key, "/")
	if len(parts) < 2 {
		return false
	}

	// Each segment must be non-empty (except the first which is empty due to leading /)
	for i := 1; i < len(parts); i++ {
		if parts[i] == "" {
			return false
		}
	}

	return true
}
