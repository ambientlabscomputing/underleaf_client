# Raft Cluster Quick Start Guide

## Overview
This guide walks through setting up a 3-node Raft cluster for high-availability KV storage.

## Prerequisites
- 3 servers with Underleaf agent installed
- Network connectivity between nodes on port 7946
- Agent HTTP port (default: 8081) accessible

## Setup Steps

### Step 1: Bootstrap First Node

On **server1** (10.0.1.1):

Edit `~/.underleaf/config.yaml`:
```yaml
payload:
  raft:
    enabled: true
    node_id: node1
    bind_addr: 10.0.1.1:7946
    data_dir: /var/lib/underleaf/raft
    bootstrap: true
```

Start agent:
```bash
ufctl agent start --mode dev
```

Verify:
```bash
ufctl cluster status
# Expected: Role: Leader, Is Leader: true
```

### Step 2: Configure Second Node

On **server2** (10.0.1.2):

Edit `~/.underleaf/config.yaml`:
```yaml
payload:
  raft:
    enabled: true
    node_id: node2
    bind_addr: 10.0.1.2:7946
    data_dir: /var/lib/underleaf/raft
    bootstrap: false
    bootstrap_peers:
      - node1@10.0.1.1:7946
```

Start agent:
```bash
ufctl agent start --mode dev
```

### Step 3: Add Second Node to Cluster

On **server1**:
```bash
ufctl cluster join node2 10.0.1.2:7946
```

Verify membership:
```bash
ufctl cluster status
# Should show 2 nodes (1 voter, 1 non-voter)
```

### Step 4: Promote Second Node

Enable maintenance mode:
```bash
ufctl cluster maintenance enable
```

Promote node2:
```bash
ufctl cluster promote node2
```

Disable maintenance mode:
```bash
ufctl cluster maintenance disable
```

### Step 5: Configure Third Node

On **server3** (10.0.1.3):

Edit `~/.underleaf/config.yaml`:
```yaml
payload:
  raft:
    enabled: true
    node_id: node3
    bind_addr: 10.0.1.3:7946
    data_dir: /var/lib/underleaf/raft
    bootstrap: false
    bootstrap_peers:
      - node1@10.0.1.1:7946
      - node2@10.0.1.2:7946
```

Start agent:
```bash
ufctl agent start --mode dev
```

### Step 6: Add and Promote Third Node

On **server1**:
```bash
# Add node3
ufctl cluster join node3 10.0.1.3:7946

# Enable maintenance mode
ufctl cluster maintenance enable

# Promote node3
ufctl cluster promote node3

# Disable maintenance mode
ufctl cluster maintenance disable
```

### Step 7: Verify Cluster

On any node:
```bash
ufctl cluster status
# Expected: 3 nodes (3 voters)
```

## Common Operations

### Store Configuration
```bash
# Store a value
ufctl cluster kv put /config/db_host postgres.example.com

# Store JSON (as string)
ufctl cluster kv put /config/limits '{"max_conn":1000,"timeout":30}'
```

### Read Configuration
```bash
# Get a specific key
ufctl cluster kv get /config/db_host

# List all keys under /config
ufctl cluster kv list /config
```

### Watch for Changes (via API)
```go
import "github.com/ambientlabscomputing/underleaf_client/internal/raft"

// Create watcher
watch := raft.NewWatch(node)
watcher := watch.CreateWatcher("/config", 0)

// Listen for events
for event := range watcher.Events() {
    switch event.Type {
    case raft.WatchEventPut:
        fmt.Printf("Key %s updated to %s\n", event.Key, event.Value)
    case raft.WatchEventDelete:
        fmt.Printf("Key %s deleted\n", event.Key)
    }
}
```

### Distributed Lock
```go
import "github.com/ambientlabscomputing/underleaf_client/internal/raft"

// Acquire lock
lock := raft.NewLock(node)
lockID, err := lock.Acquire("deployment-lock", "node1", 60)
if err != nil {
    log.Fatal(err)
}

// Do exclusive work
performDeployment()

// Release lock
lock.Release(lockID)
```

## Troubleshooting

### Cluster Won't Form
```bash
# Check logs
tail -f /tmp/underleaf-agent.log

# Verify connectivity
nc -zv 10.0.1.1 7946

# Check firewall
sudo ufw status
```

### Leader Election Timeout
- Ensure all nodes can reach each other on Raft port (7946)
- Check network latency between nodes
- Verify no firewall blocking traffic

### Split Brain
- Always maintain odd number of nodes (1, 3, 5)
- Don't exceed 3 voters in edge clusters
- Use maintenance mode for topology changes

### Node Recovery
If a node fails and needs to be replaced:
```bash
# Remove dead node
ufctl cluster leave old-node-id

# Add new node
ufctl cluster join new-node-id new-node-address

# Promote if needed
ufctl cluster maintenance enable
ufctl cluster promote new-node-id
ufctl cluster maintenance disable
```

## Best Practices

1. **Always use odd numbers**: 1, 3, or 5 nodes (3 recommended for edge)
2. **Use maintenance mode**: For promote/demote operations
3. **Monitor health**: Regularly check `ufctl cluster status`
4. **Backup data**: BoltDB files in `data_dir` are your cluster state
5. **Network latency**: Keep nodes close (<100ms latency)
6. **Firewall rules**: Allow port 7946 between cluster nodes

## HTTP API Reference

### Cluster Status
```bash
curl http://localhost:8081/api/v1/raft/status
```

### Store Value
```bash
curl -X PUT http://localhost:8081/api/v1/raft/kv/mykey \
  -H "Content-Type: application/json" \
  -d '{"value":"myvalue"}'
```

### Get Value
```bash
curl http://localhost:8081/api/v1/raft/kv/mykey
```

### List Keys
```bash
curl http://localhost:8081/api/v1/raft/kv?prefix=/config
```

### Add Node
```bash
curl -X POST http://localhost:8081/api/v1/raft/nodes \
  -H "Content-Type: application/json" \
  -d '{"node_id":"node2","address":"10.0.1.2:7946"}'
```

## Performance Tuning

For production deployments, consider tuning these Raft parameters in `NodeConfig`:
- `HeartbeatTimeout`: 1s (default) - Lower for faster failure detection
- `ElectionTimeout`: 1s (default) - Higher for unstable networks
- `CommitTimeout`: 50ms (default) - Lower for faster commits
- `MaxAppendEntries`: 64 (default) - Higher for better throughput

## Security Considerations

1. **TLS**: Configure gRPC transport with TLS certificates
2. **Authentication**: Add API authentication to HTTP endpoints
3. **Network Isolation**: Restrict Raft port to cluster nodes only
4. **Encryption at Rest**: Encrypt BoltDB data directory
5. **Audit Logging**: Enable logging for all KV operations

## Migration from Standalone

To migrate from standalone agent to clustered:

1. Stop agent on node1
2. Add Raft configuration to config.yaml (bootstrap: true)
3. Start agent (forms single-node cluster)
4. Add additional nodes as needed
5. Migrate application data to Raft KV store

## Support

For issues or questions:
- Check logs: `/tmp/underleaf-agent.log`
- View documentation: `internal/raft/README.md`
- Run diagnostics: `ufctl cluster status`
