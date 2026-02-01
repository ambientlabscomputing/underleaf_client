# Raft-based KV Quorum Subsystem

This package implements an edge-optimized Raft-based consensus subsystem for small clusters (1-3 nodes), providing strongly consistent coordination primitives and a durable key-value store.

## Architecture Overview

The implementation consists of several key components:

### Core Components

- **Node** (`node.go`) - Manages Raft node lifecycle, bootstrapping, and cluster operations
- **FSM** (`fsm.go`) - Finite State Machine implementing the KV store with revision tracking
- **Transport** (`transport.go`) - gRPC-based transport layer for Raft node-to-node communication
- **Types** (`types.go`) - Core data structures and configuration types
- **Errors** (`errors.go`) - Typed error handling following existing patterns

### Feature Modules

- **KV** (`kv.go`) - Key-value operations with linearizable and stale read modes
- **Watch** (`watch.go`) - Revision-ordered watch streams with prefix filtering
- **Lease** (`lease.go`) - TTL-based leases for ephemeral data and coordination
- **Lock** (`lock.go`) - Distributed mutex primitives backed by leases
- **Election** (`election.go`) - Leader election for application-level coordination
- **Membership** (`membership.go`) - Cluster membership management with safety guarantees

## Key Features

### Consistency Guarantees

- **Linearizable Reads**: Requires leader confirmation, always returns latest committed state
- **Stale Reads**: Returns locally cached data, may lag behind leader
- **Strong Write Consistency**: All writes go through Raft consensus

### Cluster Size Support

Following RFC Section 4.1, the system supports:

- **1 Node**: Single-node authoritative leader (no fault tolerance)
- **2 Nodes**: Leader + follower (limited fault tolerance)
- **3 Nodes**: Standard Raft quorum (optimal for edge deployments)
- **4+ Nodes**: Maximum 3 voters, additional nodes join as learners

### Data Model

Keys follow a hierarchical structure:
```
/org/{orgId}/cluster/{clusterId}/resource/...
```

Each write:
- Increments a global monotonic revision counter
- Produces a durable version identifier
- Enforces maximum value size limits
- Respects total storage budget

### Coordination Primitives

**Leases**
- Time-bounded ownership tokens with TTL
- Automatic expiration on node failure
- Keys can be attached to leases for automatic cleanup

**Locks**
- Mutual exclusion backed by leases
- Automatic release on lease expiration
- Try-lock and blocking acquire modes

**Elections**
- Distributed leader election for application roles
- Lease-backed with automatic failover
- Observable via watch streams

## Usage Examples

### Single Node Cluster

```go
config := &raft.NodeConfig{
    NodeID:           "node1",
    BindAddr:         "127.0.0.1:7000",
    DataDir:          "/var/lib/underleaf/raft",
    Bootstrap:        true,
    HeartbeatTimeout: 1000 * time.Millisecond,
    ElectionTimeout:  1000 * time.Millisecond,
    MaxValueSize:     1024 * 1024,      // 1 MB
    MaxStorageSize:   100 * 1024 * 1024, // 100 MB
}

node, err := raft.NewNode(config, logger)
if err != nil {
    return err
}

if err := node.Start(); err != nil {
    return err
}
defer node.Stop()

// Wait for leader election
if err := node.WaitForLeader(5 * time.Second); err != nil {
    return err
}
```

### KV Operations

```go
kv := raft.NewKV(node)

// Put a value
key := "/org/acme/cluster/prod/config/database"
value := []byte(`{"host": "db.example.com", "port": 5432}`)
if err := kv.Put(key, value, ""); err != nil {
    return err
}

// Get with linearizable read
entry, err := kv.Get(key, raft.ReadModeLinearizable)
if err != nil {
    return err
}

// List keys with prefix
entries, err := kv.List("/org/acme/", raft.ReadModeLinearizable)
if err != nil {
    return err
}

// Delete a key
if err := kv.Delete(key); err != nil {
    return err
}
```

### Watch for Changes

```go
watch := raft.NewWatch(node)

watcher, err := watch.WatchPrefix(ctx, "/org/acme/", 0)
if err != nil {
    return err
}
defer watcher.Close()

for event := range watcher.Events() {
    switch event.Type {
    case raft.WatchEventPut:
        fmt.Printf("Key updated: %s (rev: %d)\n", event.Key, event.Revision)
    case raft.WatchEventDelete:
        fmt.Printf("Key deleted: %s\n", event.Key)
    }
}
```

### Distributed Locks

```go
lock := raft.NewLock(node)

// Acquire lock
lockInfo, err := lock.Acquire(ctx, "/resources/worker-1", 30, "process-123")
if err != nil {
    return err
}
defer lock.Release(lockInfo)

// Do work while holding the lock
processWork()
```

### Leader Election

```go
election := raft.NewElection(node)

// Campaign for leadership
electionInfo, err := election.Campaign(ctx, "coordinator", "worker-1", 30)
if err != nil {
    return err // Lost election or another error
}
defer election.Resign(electionInfo)

// Act as leader
coordinateWork()
```

### Membership Management

```go
membership := raft.NewMembership(node)

// Enable maintenance mode before membership changes
if err := membership.EnableMaintenanceMode(); err != nil {
    return err
}
defer membership.DisableMaintenanceMode()

// Add a new node as learner
if err := membership.AddNode("node2", "10.0.1.2:7000"); err != nil {
    return err
}

// Wait for sync
if err := membership.WaitForNodeSync("node2", 30*time.Second); err != nil {
    return err
}

// Promote to voter
if err := membership.PromoteNode("node2"); err != nil {
    return err
}
```

## Operational Considerations

### Storage

- Raft logs stored in BoltDB at `{DataDir}/raft.db`
- Snapshots stored in `{DataDir}/snapshots/`
- FSM data is in-memory with periodic snapshots

### Network

- gRPC transport on configured bind address
- Requires bidirectional connectivity between all nodes
- Consider using mTLS for production deployments

### Monitoring

```go
stats, err := node.GetStats()
if err != nil {
    return err
}

fmt.Printf("Role: %s\n", stats.Role)
fmt.Printf("Leader: %s\n", stats.LeaderID)
fmt.Printf("Term: %d\n", stats.Term)
fmt.Printf("Keys: %d\n", stats.KeyCount)
fmt.Printf("Storage: %d bytes\n", stats.StorageSize)
```

### Failure Modes

**Single Node**: No fault tolerance; node failure = cluster unavailable

**Two Nodes**: 
- Leader failure = cluster unavailable (no quorum)
- Follower failure = leader continues operating

**Three Nodes**:
- One node failure = cluster continues normally
- Two node failures = cluster becomes read-only

## Safety Guarantees

Following the RFC specification (see `agent_docs/KV_QUORUM_SUBSYSSTEM.md`):

1. **Single Leader**: At most one leader at any time
2. **Write Safety**: Writes only acknowledged after quorum commit
3. **Partition Safety**: Minority partitions cannot accept writes
4. **Membership Safety**: Membership changes are serialized
5. **Lease Safety**: Leases expire without quorum
6. **Watch Ordering**: Watch streams preserve commit order

## Future Enhancements

- [ ] Snapshot compression
- [ ] Read index optimization for linearizable reads
- [ ] PreVote extension to reduce disruptions
- [ ] Automatic learner promotion based on sync status
- [ ] Prometheus metrics integration
- [ ] Admin API for cluster management
- [ ] Historical event replay for watch from past revisions
- [ ] Transaction batching for multiple operations
- [ ] Configurable snapshot retention policies

## Integration with Underleaf

This subsystem is designed to be the coordination backbone for Underleaf edge clusters:

- **Deployment Intent**: Store desired deployment configurations
- **Scheduler Coordination**: Leader election for scheduler instances  
- **Resource Locks**: Coordinate access to shared resources
- **Cluster Metadata**: Store node registrations and health status
- **Configuration Management**: Replace/augment current snapshot-based config with consensus-backed store

## References

- RFC: `/agent_docs/KV_QUORUM_SUBSYSSTEM.md`
- Raft Paper: https://raft.github.io/raft.pdf
- HashiCorp Raft: https://github.com/hashicorp/raft
