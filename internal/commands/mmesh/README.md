# Mycelium Mesh Agent (MMA) CLI Commands

This package provides CLI commands to control and monitor the Mycelium Mesh Agent (MMA) integration with the Underleaf Agent.

## Overview

The UA→MMA event stream allows the Underleaf Agent to publish cluster membership, service lifecycle, and policy events to the Mycelium Mesh Agent for mesh networking capabilities.

## Commands

### `ufctl mmesh status`

Display the status of the UA→MMA event stream server including:
- Running status
- Cluster and node information
- Socket address (Unix socket path)
- Active subscriber count
- Buffer usage statistics

**Example:**
```bash
ufctl mmesh status
```

**Output:**
```
┌─────────────────────────────────────┐
│ Mycelium Mesh Agent Status          │
├────────────────────┬────────────────┤
│ Property           │ Value          │
├────────────────────┼────────────────┤
│ Status             │ running        │
│ Cluster ID         │ cluster-123    │
│ Node ID            │ node-abc       │
│ Socket Address     │ /tmp/ua_mma.sock│
│ Active Subscribers │ 1              │
│ Buffer Size        │ 245 events     │
│ Buffer Capacity    │ 50000 events   │
│ Buffer Usage       │ 0.5%           │
└────────────────────┴────────────────┘
```

### `ufctl mmesh subscribers`

List active Mycelium Mesh Agent (MMA) subscribers connected to the UA event stream.

**Example:**
```bash
ufctl mmesh subscribers
```

**Output:**
```
✓ Active subscribers: 1

Note: Detailed subscriber information is not yet available.
```

### `ufctl mmesh buffer`

Display detailed statistics about the UA→MMA event stream ring buffer, including size, capacity, and usage percentage.

**Example:**
```bash
ufctl mmesh buffer
```

**Output:**
```
┌──────────────────────────────────────┐
│ Event Stream Buffer Statistics       │
├─────────────────┬────────────────────┤
│ Property        │ Value              │
├─────────────────┼────────────────────┤
│ Current Size    │ 245 events         │
│ Capacity        │ 50000 events       │
│ Free Space      │ 49755 events       │
│ Usage           │ 0.5%               │
└─────────────────┴────────────────────┘

ℹ The ring buffer stores recent events for catch-up when MMA reconnects.
ℹ When full, the oldest events are overwritten (ring buffer behavior).
```

### `ufctl mmesh publish`

Publish a test event to the Mycelium Mesh Agent event stream. This is useful for testing and debugging the UA→MMA integration.

**Example:**
```bash
ufctl mmesh publish \
  --event-type "test.event" \
  --payload '{"message":"hello","timestamp":"2024-01-01T00:00:00Z"}' \
  --entity-kind "service" \
  --entity-id "svc-123"
```

**Required Flags:**
- `--event-type`: Event type (e.g., "test.event", "member.joined")
- `--payload`: Event payload as JSON string

**Optional Flags:**
- `--entity-kind`: Entity kind (e.g., "service", "member", "policy")
- `--entity-id`: Entity ID (e.g., "svc-123", "node-abc")

**Output:**
```
✓ Event published successfully

Status: published
Event Type: test.event
Entity Kind: service
Entity ID: svc-123
```

## Event Types

The following event types are supported by the UA→MMA event stream:

### Cluster Events
- `cluster.snapshot` - Full cluster state snapshot

### Member Events
- `member.joined` - New member joined the cluster
- `member.updated` - Member information updated
- `member.left` - Member left the cluster
- `member.health` - Member health status update

### Identity Events
- `identity.trust_roots.updated` - Trust roots updated
- `identity.service.issued` - Service identity issued
- `identity.service.revoked` - Service identity revoked

### Service Events
- `service.started` - Service started
- `service.updated` - Service configuration updated
- `service.stopped` - Service stopped

### Policy Events
- `capability_cache.snapshot.updated` - Capability cache snapshot updated
- `capability_cache.delta.updated` - Capability cache delta updated
- `mesh_policy.updated` - Mesh policy updated
- `consent_state.updated` - Consent state updated

## Architecture

The MMA integration consists of:

1. **Event Stream Server**: A gRPC server running in the Underleaf Agent that publishes events to MMA subscribers via a Unix socket (`/tmp/ua_mma.sock` by default).

2. **Ring Buffer**: A circular buffer that stores recent events (default: 50,000 events) to support catch-up when MMA reconnects.

3. **HTTP API**: RESTful endpoints exposed by the agent for CLI commands to query status and publish test events.

## Troubleshooting

If commands fail with "MMA event stream server not initialized":
1. Ensure the Underleaf Agent is running: `ufctl agent status`
2. Check if the agent is assigned to a cluster: `ufctl cluster status`
3. The MMA event stream is automatically enabled when the agent joins a cluster

If commands fail with connection errors:
1. Check if the agent is running: `ufctl agent status`
2. Verify the agent port: `ufctl agent status` (default: 2240)
3. Override the port if needed: `ufctl mmesh status --config-agent-port 9090`

## Implementation Details

### HTTP Endpoints

The following HTTP endpoints are exposed by the agent:

- `GET /api/v1/mmesh/status` - Get MMA event stream status
- `GET /api/v1/mmesh/subscribers` - List active subscribers
- `GET /api/v1/mmesh/buffer` - Get buffer statistics
- `POST /api/v1/mmesh/publish` - Publish a test event

### Client Methods

The agent client provides the following methods:

```go
client.GetMMeshStatus() (map[string]interface{}, error)
client.GetMMeshSubscribers() (map[string]interface{}, error)
client.GetMMeshBuffer() (map[string]interface{}, error)
client.PublishMMeshEvent(eventType string, payload map[string]interface{}, entityKind, entityID string) (map[string]interface{}, error)
```

## Future Enhancements

Potential improvements for the MMA CLI:

1. **Subscriber Details**: Expose detailed information about each subscriber (connection time, filter settings, buffer usage)
2. **Event History**: Command to view recent events in the ring buffer
3. **Event Filtering**: Ability to filter events by type when viewing history
4. **Metrics**: Event throughput, latency, and drop statistics
5. **Diagnostics**: Ping/test connectivity to MMA, verify event delivery
