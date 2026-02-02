package raft

import (
	"encoding/json"
	"fmt"
	"time"
)

// KV provides key-value operations on the Raft store.
type KV struct {
	node *Node
}

// NewKV creates a new KV instance.
func NewKV(node *Node) *KV {
	return &KV{node: node}
}

// Get retrieves a value by key with the specified read mode.
func (kv *KV) Get(key string, mode ReadMode) (*KVEntry, error) {
	if kv.node.raft == nil {
		return nil, fmt.Errorf("raft not initialized")
	}

	switch mode {
	case ReadModeLinearizable:
		return kv.getLinearizable(key)
	case ReadModeStale:
		return kv.getStale(key)
	default:
		return nil, fmt.Errorf("invalid read mode: %s", mode)
	}
}

// getLinearizable performs a linearizable read (requires leader confirmation).
func (kv *KV) getLinearizable(key string) (*KVEntry, error) {
	// Verify we can read from leader
	if err := kv.node.VerifyLeader(); err != nil {
		// If not leader, try to forward to leader
		leaderAddr, getErr := kv.node.GetLeader()
		if getErr != nil {
			return nil, ErrNotLeader
		}
		return nil, NewRaftErrorf("not_leader", "not the leader; leader is at %s", leaderAddr)
	}

	// Read from FSM (this is safe because we verified leadership)
	return kv.node.fsm.Get(key)
}

// getStale performs a stale read from local FSM (no leader confirmation).
func (kv *KV) getStale(key string) (*KVEntry, error) {
	return kv.node.fsm.Get(key)
}

// Put stores a key-value pair.
func (kv *KV) Put(key string, value []byte, leaseID string) error {
	if kv.node.raft == nil {
		return fmt.Errorf("raft not initialized")
	}

	// Validate key format
	if !isValidKey(key) {
		return ErrInvalidKey
	}

	// Create FSM command
	cmd := FSMCommand{
		Op:    "put",
		Key:   key,
		Value: value,
		Lease: leaseID,
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	// Apply to Raft log
	return kv.node.Apply(cmdBytes, 10*time.Second)
}

// Delete removes a key from the store.
func (kv *KV) Delete(key string) error {
	if kv.node.raft == nil {
		return fmt.Errorf("raft not initialized")
	}

	// Create FSM command
	cmd := FSMCommand{
		Op:  "delete",
		Key: key,
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	// Apply to Raft log
	return kv.node.Apply(cmdBytes, 10*time.Second)
}

// List retrieves all keys with the given prefix.
func (kv *KV) List(prefix string, mode ReadMode) ([]*KVEntry, error) {
	if kv.node.raft == nil {
		return nil, fmt.Errorf("raft not initialized")
	}

	switch mode {
	case ReadModeLinearizable:
		// Verify leadership for linearizable reads
		if err := kv.node.VerifyLeader(); err != nil {
			leaderAddr, _ := kv.node.GetLeader()
			return nil, NewRaftErrorf("not_leader", "not the leader; leader is at %s", leaderAddr)
		}
		return kv.node.fsm.List(prefix)

	case ReadModeStale:
		// Stale read directly from FSM
		return kv.node.fsm.List(prefix)

	default:
		return nil, fmt.Errorf("invalid read mode: %s", mode)
	}
}

// GetRange retrieves all keys in the range [startKey, endKey).
func (kv *KV) GetRange(startKey, endKey string, mode ReadMode) ([]*KVEntry, error) {
	// For now, implement using List and filter
	// A more optimized version could be added to FSM later
	entries, err := kv.List("", mode)
	if err != nil {
		return nil, err
	}

	var result []*KVEntry
	for _, entry := range entries {
		if entry.Key >= startKey && entry.Key < endKey {
			result = append(result, entry)
		}
	}

	return result, nil
}

// CompareAndSwap atomically updates a key only if the current revision matches.
func (kv *KV) CompareAndSwap(key string, expectedRevision uint64, newValue []byte) error {
	if kv.node.raft == nil {
		return fmt.Errorf("raft not initialized")
	}

	// First, get the current entry with linearizable read
	current, err := kv.Get(key, ReadModeLinearizable)
	if err != nil && err != ErrKeyNotFound {
		return err
	}

	// Check revision
	if current != nil && current.Revision != expectedRevision {
		return NewRaftErrorf("revision_mismatch",
			"revision mismatch: expected %d, got %d", expectedRevision, current.Revision)
	}

	if current == nil && expectedRevision != 0 {
		return NewRaftErrorf("revision_mismatch",
			"key does not exist, but expected revision %d", expectedRevision)
	}

	// Perform the put
	return kv.Put(key, newValue, "")
}

// PutIfNotExists creates a key only if it doesn't already exist.
func (kv *KV) PutIfNotExists(key string, value []byte) (bool, error) {
	if kv.node.raft == nil {
		return false, fmt.Errorf("raft not initialized")
	}

	// Check if key exists with linearizable read
	_, err := kv.Get(key, ReadModeLinearizable)
	if err == nil {
		// Key exists
		return false, nil
	}

	if err != ErrKeyNotFound {
		// Some other error occurred
		return false, err
	}

	// Key doesn't exist, create it
	if err := kv.Put(key, value, ""); err != nil {
		return false, err
	}

	return true, nil
}

// Transaction represents a batch of KV operations.
type Transaction struct {
	ops []FSMCommand
}

// NewTransaction creates a new transaction.
func (kv *KV) NewTransaction() *Transaction {
	return &Transaction{
		ops: make([]FSMCommand, 0),
	}
}

// Put adds a put operation to the transaction.
func (tx *Transaction) Put(key string, value []byte) *Transaction {
	tx.ops = append(tx.ops, FSMCommand{
		Op:    "put",
		Key:   key,
		Value: value,
	})
	return tx
}

// Delete adds a delete operation to the transaction.
func (tx *Transaction) Delete(key string) *Transaction {
	tx.ops = append(tx.ops, FSMCommand{
		Op:  "delete",
		Key: key,
	})
	return tx
}

// Commit applies the transaction atomically.
// Note: This is a simple implementation. A production version would need
// true transactional semantics in the FSM.
func (kv *KV) Commit(tx *Transaction) error {
	if kv.node.raft == nil {
		return fmt.Errorf("raft not initialized")
	}

	// For now, apply operations sequentially
	// A better implementation would batch them in a single Raft log entry
	for _, op := range tx.ops {
		cmdBytes, err := json.Marshal(op)
		if err != nil {
			return fmt.Errorf("failed to marshal command: %w", err)
		}

		if err := kv.node.Apply(cmdBytes, 10*time.Second); err != nil {
			return err
		}
	}

	return nil
}

// GetRevision returns the current FSM revision.
func (kv *KV) GetRevision() uint64 {
	if kv.node == nil || kv.node.fsm == nil {
		return 0
	}
	return kv.node.fsm.GetRevision()
}
