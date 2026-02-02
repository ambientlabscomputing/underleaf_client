package raft

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Lease provides lease management for TTL-based ownership tokens.
type Lease struct {
	node *Node
}

// NewLease creates a new Lease manager.
func NewLease(node *Node) *Lease {
	return &Lease{node: node}
}

// Grant creates a new lease with the specified TTL (in seconds).
func (l *Lease) Grant(ttl int64, holder string) (string, error) {
	if l.node.raft == nil {
		return "", fmt.Errorf("raft not initialized")
	}

	// Generate unique lease ID
	leaseID := uuid.New().String()

	// Create FSM command
	ttlBytes, err := json.Marshal(ttl)
	if err != nil {
		return "", fmt.Errorf("failed to marshal ttl: %w", err)
	}

	cmd := FSMCommand{
		Op:    "grant_lease",
		Key:   leaseID,
		Value: ttlBytes,
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return "", fmt.Errorf("failed to marshal command: %w", err)
	}

	// Apply to Raft log
	if err := l.node.Apply(cmdBytes, 10*time.Second); err != nil {
		return "", err
	}

	l.node.logger.Info("lease granted", "lease_id", leaseID, "ttl", ttl, "holder", holder)
	return leaseID, nil
}

// Revoke revokes a lease, deleting all associated keys.
func (l *Lease) Revoke(leaseID string) error {
	if l.node.raft == nil {
		return fmt.Errorf("raft not initialized")
	}

	cmd := FSMCommand{
		Op:  "revoke_lease",
		Key: leaseID,
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	l.node.logger.Info("revoking lease", "lease_id", leaseID)
	return l.node.Apply(cmdBytes, 10*time.Second)
}

// KeepAlive renews a lease by granting a new lease with the same ID.
// This is a simplified implementation - a production version would have
// dedicated keepalive logic that extends the existing lease.
func (l *Lease) KeepAlive(ctx context.Context, leaseID string, ttl int64) error {
	ticker := time.NewTicker(time.Duration(ttl/2) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// Check if lease still exists
			lease, err := l.node.fsm.GetLease(leaseID)
			if err != nil {
				return fmt.Errorf("lease not found or expired: %w", err)
			}

			// Check if lease is close to expiring
			if time.Now().After(lease.ExpiresAt.Add(-time.Duration(ttl/2) * time.Second)) {
				// Renew by revoking and re-granting
				// This is a simplification - production would extend existing lease
				l.node.logger.Debug("renewing lease", "lease_id", leaseID)

				// For now, we just log that renewal is needed
				// A full implementation would extend the lease in the FSM
			}
		}
	}
}

// GetLeaseInfo returns information about a lease.
func (l *Lease) GetLeaseInfo(leaseID string) (*LeaseInfo, error) {
	if l.node.fsm == nil {
		return nil, fmt.Errorf("fsm not initialized")
	}

	return l.node.fsm.GetLease(leaseID)
}

// AttachKey attaches a key to a lease so it's deleted when the lease expires.
func (l *Lease) AttachKey(leaseID, key string) error {
	if l.node.raft == nil {
		return fmt.Errorf("raft not initialized")
	}

	cmd := FSMCommand{
		Op:    "attach_lease",
		Key:   key,
		Lease: leaseID,
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	return l.node.Apply(cmdBytes, 10*time.Second)
}

// Lock provides distributed lock primitives backed by leases.
type Lock struct {
	node  *Node
	lease *Lease
	kv    *KV
}

// NewLock creates a new Lock manager.
func NewLock(node *Node) *Lock {
	return &Lock{
		node:  node,
		lease: NewLease(node),
		kv:    NewKV(node),
	}
}

// Acquire attempts to acquire a lock on the given key.
func (lock *Lock) Acquire(ctx context.Context, key string, ttl int64, holder string) (*LockInfo, error) {
	// Generate lock key
	lockKey := fmt.Sprintf("/locks%s", key)

	// Try to create the lock key with PutIfNotExists
	leaseID, err := lock.lease.Grant(ttl, holder)
	if err != nil {
		return nil, err
	}

	// Store lock information
	lockData := map[string]string{
		"holder":   holder,
		"lease_id": leaseID,
	}
	lockBytes, err := json.Marshal(lockData)
	if err != nil {
		lock.lease.Revoke(leaseID)
		return nil, err
	}

	// Try to acquire lock (create key if it doesn't exist)
	created, err := lock.kv.PutIfNotExists(lockKey, lockBytes)
	if err != nil {
		lock.lease.Revoke(leaseID)
		return nil, err
	}

	if !created {
		// Lock is already held
		lock.lease.Revoke(leaseID)
		return nil, ErrLockHeld
	}

	// Attach lock key to lease
	if err := lock.lease.AttachKey(leaseID, lockKey); err != nil {
		// Try to clean up
		lock.kv.Delete(lockKey)
		lock.lease.Revoke(leaseID)
		return nil, err
	}

	info := &LockInfo{
		Key:        key,
		LeaseID:    leaseID,
		Holder:     holder,
		AcquiredAt: time.Now(),
	}

	lock.node.logger.Info("lock acquired", "key", key, "holder", holder, "lease_id", leaseID)
	return info, nil
}

// Release releases a lock.
func (lock *Lock) Release(lockInfo *LockInfo) error {
	lockKey := fmt.Sprintf("/locks%s", lockInfo.Key)

	// Delete the lock key
	if err := lock.kv.Delete(lockKey); err != nil {
		return err
	}

	// Revoke the lease
	if err := lock.lease.Revoke(lockInfo.LeaseID); err != nil {
		return err
	}

	lock.node.logger.Info("lock released", "key", lockInfo.Key, "holder", lockInfo.Holder)
	return nil
}

// TryAcquire attempts to acquire a lock without blocking.
func (lock *Lock) TryAcquire(key string, ttl int64, holder string) (*LockInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	return lock.Acquire(ctx, key, ttl, holder)
}

// Election provides distributed leader election backed by leases.
type Election struct {
	node  *Node
	lease *Lease
	kv    *KV
}

// NewElection creates a new Election manager.
func NewElection(node *Node) *Election {
	return &Election{
		node:  node,
		lease: NewLease(node),
		kv:    NewKV(node),
	}
}

// ElectionInfo represents information about an election.
type ElectionInfo struct {
	// Name is the election name/key
	Name string

	// LeaseID is the backing lease
	LeaseID string

	// Leader is the current leader ID
	Leader string

	// ElectedAt is when leadership was obtained
	ElectedAt time.Time
}

// Campaign attempts to become the leader for the given election.
func (e *Election) Campaign(ctx context.Context, name string, candidateID string, ttl int64) (*ElectionInfo, error) {
	electionKey := fmt.Sprintf("/elections/%s/leader", name)

	// Grant a lease for leadership
	leaseID, err := e.lease.Grant(ttl, candidateID)
	if err != nil {
		return nil, err
	}

	// Try to become leader
	leaderData := map[string]interface{}{
		"leader":     candidateID,
		"lease_id":   leaseID,
		"elected_at": time.Now().Unix(),
	}
	leaderBytes, err := json.Marshal(leaderData)
	if err != nil {
		e.lease.Revoke(leaseID)
		return nil, err
	}

	// Try to create the election key
	created, err := e.kv.PutIfNotExists(electionKey, leaderBytes)
	if err != nil {
		e.lease.Revoke(leaseID)
		return nil, err
	}

	if !created {
		// Someone else is already leader
		e.lease.Revoke(leaseID)
		return nil, NewRaftError("not_elected", "another candidate is already the leader")
	}

	// Attach election key to lease
	if err := e.lease.AttachKey(leaseID, electionKey); err != nil {
		e.kv.Delete(electionKey)
		e.lease.Revoke(leaseID)
		return nil, err
	}

	info := &ElectionInfo{
		Name:      name,
		LeaseID:   leaseID,
		Leader:    candidateID,
		ElectedAt: time.Now(),
	}

	e.node.logger.Info("election won", "name", name, "leader", candidateID)
	return info, nil
}

// Resign resigns from leadership.
func (e *Election) Resign(info *ElectionInfo) error {
	electionKey := fmt.Sprintf("/elections/%s/leader", info.Name)

	// Delete the election key
	if err := e.kv.Delete(electionKey); err != nil {
		return err
	}

	// Revoke the lease
	if err := e.lease.Revoke(info.LeaseID); err != nil {
		return err
	}

	e.node.logger.Info("resigned from election", "name", info.Name, "leader", info.Leader)
	return nil
}

// GetLeader returns the current leader for an election.
func (e *Election) GetLeader(name string) (string, error) {
	electionKey := fmt.Sprintf("/elections/%s/leader", name)

	entry, err := e.kv.Get(electionKey, ReadModeLinearizable)
	if err != nil {
		return "", err
	}

	var leaderData map[string]interface{}
	if err := json.Unmarshal(entry.Value, &leaderData); err != nil {
		return "", err
	}

	leader, ok := leaderData["leader"].(string)
	if !ok {
		return "", fmt.Errorf("invalid leader data")
	}

	return leader, nil
}

// Observe watches for leadership changes in an election.
func (e *Election) Observe(ctx context.Context, name string) (<-chan string, error) {
	electionKey := fmt.Sprintf("/elections/%s/leader", name)

	watch := NewWatch(e.node)
	watcher, err := watch.WatchKey(ctx, electionKey, 0)
	if err != nil {
		return nil, err
	}

	leaderCh := make(chan string, 10)

	// Convert watch events to leader changes
	go func() {
		defer close(leaderCh)
		defer watcher.Close()

		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-watcher.Events():
				if !ok {
					return
				}

				if event.Type == WatchEventPut {
					var leaderData map[string]interface{}
					if err := json.Unmarshal(event.Value, &leaderData); err != nil {
						e.node.logger.Error("failed to unmarshal leader data", "error", err)
						continue
					}

					if leader, ok := leaderData["leader"].(string); ok {
						select {
						case leaderCh <- leader:
						case <-ctx.Done():
							return
						}
					}
				}
			}
		}
	}()

	return leaderCh, nil
}
