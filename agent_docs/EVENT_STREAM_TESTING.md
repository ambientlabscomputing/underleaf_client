# UA→MMA Event Stream Testing Guide

This guide covers how to test the gRPC event stream between Underleaf Agent (UA) and Mycelium Mesh Agent (MMA).

## Architecture Overview

- **UA Event Stream Server**: Publishes events from UA to MMA via gRPC
- **MMA Event Consumer**: Subscribes to UA events and processes them
- **Transport**: Unix domain socket at `/tmp/ua_mma.sock` with peer credential authentication
- **Backpressure**: 50k event ring buffer on UA, per-subscriber buffers (1k-10k), drop-oldest strategy
- **Resumption**: MMA tracks `last_seen_seq` per node for catch-up after reconnects

## Quick Start

### 1. Test the Event Stream Server

Start the UA event stream server in standalone mode:

```bash
cd underleaf_client
go run test_event_stream.go server
```

This starts the gRPC server on `/tmp/ua_mma.sock`.

### 2. Send Test Events

In another terminal, send test events:

```bash
cd underleaf_client
go run test_event_stream.go test
```

This publishes sample events of various types:
- `cluster.snapshot`
- `member.joined`
- `service.started`
- `mesh_policy.updated`
- `capability_cache.snapshot_updated`
- `identity.trust_roots.updated`

### 3. Run MMA to Consume Events

Start MMA in another terminal:

```bash
cd mycelium_mesh_agent
go run cmd/serve/main.go --config config.yaml
```

Ensure `config.yaml` has:

```yaml
control_channel:
  address: "/tmp/ua_mma.sock"
  transport: "grpc_uds"
  tls:
    enabled: false
```

MMA will connect to UA and start consuming events.

## Testing with Full UA Agent

### 1. Configure UA

The event stream is integrated into the UA agent via the `Dependencies` struct in [internal/agent/wiring.go](internal/agent/wiring.go).

When you start the UA agent with:

```bash
./ufctl agent start --dev
```

The event stream server will:
1. Initialize in `WireAgent()` with cluster ID and node ID
2. Start listening on `/tmp/ua_mma.sock`
3. Begin accepting connections from MMA

### 2. Publish Events from UA Subsystems

Use the helper methods in [internal/agent/event_helpers.go](internal/agent/event_helpers.go):

```go
// When a peer joins the Raft cluster
deps.EventStreamServer.PublishMemberJoined(ctx, member)

// When a service is discovered via mDNS
deps.EventStreamServer.PublishServiceStarted(ctx, serviceInfo)

// When mesh policy is updated
deps.EventStreamServer.PublishMeshPolicyUpdated(ctx, policySnapshot)

// When capability cache snapshot changes
deps.EventStreamServer.PublishCapabilityCacheSnapshotUpdated(ctx, snapshot)

// When trust roots are rotated
deps.EventStreamServer.PublishIdentityTrustRootsUpdated(ctx, trustRoots)

// Periodic cluster snapshots
deps.EventStreamServer.PublishClusterSnapshot(ctx, snapshot)
```

### 3. Send Test Events Manually

In the UA code where you have access to `deps`:

```go
if err := deps.EventStreamServer.SendTestEvents(ctx); err != nil {
    logger.Error("failed to send test events", "error", err)
}
```

## Wiring Event Generation (TODO)

The following UA subsystems need to be wired to publish events:

### Raft Membership Events

**File**: [internal/raft/node.go](internal/raft/node.go)

**Location**: When `ApplyConfigChange` is called or membership changes are detected

**Events to publish**:
- `member.joined` when a new peer joins
- `member.left` when a peer leaves
- `member.updated` when a peer's attributes change

```go
// In RaftNode.ApplyConfigChange or similar
if isNewMember {
    deps.EventStreamServer.PublishMemberJoined(ctx, types.MemberInfo{
        NodeID: nodeID,
        // ... other fields
    })
}
```

### mDNS Service Discovery

**File**: [internal/discovery/mdns.go](internal/discovery/mdns.go) or similar

**Location**: When `handleServiceEntry` processes discoveries

**Events to publish**:
- `service.started` when a new service is discovered
- `service.stopped` when a service disappears

```go
// In handleServiceEntry
if isNewService {
    deps.EventStreamServer.PublishServiceStarted(ctx, serviceInfo)
}
```

### Policy Manager Updates

**File**: [internal/policy/...](internal/policy/) or wherever policy snapshots are managed

**Location**: When policy snapshots change

**Events to publish**:
- `mesh_policy.updated`

```go
// When policy snapshot updates
deps.EventStreamServer.PublishMeshPolicyUpdated(ctx, policySnapshot)
```

### Bus Events

**File**: [internal/agent/bus_client.go](internal/agent/bus_client.go) or similar

**Location**: When control plane events are received

**Events to publish**:
- `capability_cache.snapshot_updated`
- `consent_state.updated`

### Identity/Certificate Events

**File**: Wherever trust roots/CA certificates are managed

**Location**: When trust roots are rotated or updated

**Events to publish**:
- `identity.trust_roots.updated`

```go
// On CA rotation
deps.EventStreamServer.PublishIdentityTrustRootsUpdated(ctx, trustRoots)
```

### Periodic Cluster Snapshots

**File**: Dedicated goroutine or existing periodic task

**Location**: Every 30-60 seconds

**Events to publish**:
- `cluster.snapshot`

```go
// In a periodic ticker
ticker := time.NewTicker(30 * time.Second)
for range ticker.C {
    deps.EventStreamServer.PublishClusterSnapshot(ctx, snapshot)
}
```

## Testing Reconnection

### 1. Test Normal Disconnect/Reconnect

1. Start UA server
2. Start MMA (connects to UA)
3. Stop MMA (Ctrl+C)
4. Restart MMA

**Expected behavior**: MMA reconnects with exponential backoff (starting at 1s), requests events since `last_seen_seq`, catches up.

### 2. Test UA Restart

1. Start MMA first (it will wait for UA)
2. Start UA server
3. Stop UA server
4. Restart UA server

**Expected behavior**: MMA reconnects after detecting disconnect, resumes from `last_seen_seq`.

### 3. Test Backpressure

Send a burst of 100k events quickly:

```go
for i := 0; i < 100000; i++ {
    server.PublishEvent(ctx, event)
}
```

**Expected behavior**: 
- Ring buffer holds 50k events
- Slow subscribers get up to 10k buffer
- Oldest events are dropped when buffer is full
- Subscribers see `dropped_events_count` in logs

## Monitoring (Without Prometheus)

### UA Side

Check event stream server logs:

```
[INFO] event stream server started socket=/tmp/ua_mma.sock
[INFO] new subscriber connected last_seq=12345 buffer_size=1000
[WARN] subscriber buffer full, dropping oldest events dropped=50
[INFO] subscriber disconnected duration=120s total_events=5000
```

### MMA Side

Check event consumer logs:

```
[INFO] starting gRPC stream last_seq=12345
[INFO] received event type=member.joined seq=12346 node_id=node-001
[WARN] stream error, retrying in 1s error=connection refused
[INFO] reconnected after 5s backoff=2s
```

### Manual Inspection

Check socket:

```bash
ls -la /tmp/ua_mma.sock
# Should show: srw------- (permissions 0600)

lsof /tmp/ua_mma.sock
# Should show UA and MMA processes connected
```

Check MMA sequence tracking:

```bash
# In MMA logs, look for:
[DEBUG] updating node sequence node=node-001 seq=12346
```

## Troubleshooting

### Socket Permission Denied

**Symptom**: MMA cannot connect to UA socket

**Solution**: 
- Ensure socket has 0600 permissions
- Run UA and MMA as same user
- Check socket path matches in both configs

### Events Not Flowing

**Symptom**: MMA connects but receives no events

**Checks**:
1. Is UA publishing events? (check `PublishEvent` calls)
2. Is MMA subscribed? (check `new subscriber connected` in UA logs)
3. Are events being filtered? (check `last_seen_seq` resumption)
4. Send test events: `deps.EventStreamServer.SendTestEvents(ctx)`

### Reconnect Loop

**Symptom**: MMA continuously reconnects

**Checks**:
1. Is UA event stream server actually running?
2. Check UA logs for stream errors
3. Verify socket path matches in both configs
4. Check for TLS mismatch (should be disabled for UDS)

### Events Dropped

**Symptom**: Subscribers see `dropped_events_count > 0`

**Root cause**: Subscriber cannot keep up with event rate

**Solutions**:
1. Increase subscriber buffer size (currently 1k-10k)
2. Optimize MMA event processing
3. Filter events by type on UA side
4. Consider multiple MMA instances with load balancing

## Next Steps

1. **Wire UA Subsystems**: Connect Raft, mDNS, policy manager to event publishers
2. **Integration Tests**: Create automated tests for event flow scenarios
3. **Load Testing**: Simulate high event rates to validate backpressure
4. **Documentation**: Update Interface Contract with implementation status
