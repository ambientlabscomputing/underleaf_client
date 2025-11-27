# Config Manager - Snapshotted Configuration System

## Overview

The config manager ensures each edge device always has a **complete, validated, and up-to-date configuration snapshot** without blocking runtime operations on central API calls. It combines push (event-driven) and pull (periodic reconciliation) patterns to maintain local config state.

## Architecture

### Components

#### 1. **ConfigSnapshot** (`types.go`)
- Versioned, validated configuration from control plane
- Contains: `Version`, `Payload`, `Hash`, `Timestamp`, `ServerID`
- Immutable once created, validated via SHA256 hash
- Tracks age and staleness

#### 2. **LocalMetadata** (`types.go`)
- Local-only configuration not synced from control plane
- Contains: `ServerID`, `ServerName`, `AuthToken`, `APIBaseURL`, `EventBus` config
- Mutable, updated by CLI commands (e.g., `ufctl local auth login`)

#### 3. **Store** (`store.go`)
- Persistent storage with atomic writes
- Separate files for snapshot vs local metadata
- **CLI**: `~/.underleaf/snapshot.yaml` and `~/.underleaf/config.yaml`
- **Agent**: `/var/lib/underleaf/snapshot.yaml` and `/var/lib/underleaf/local.yaml`

#### 4. **SnapshotConfigManager** (`manager.go`)
- Core sync engine running as agent background service
- **Push**: Listens to `server-data-update` event bus topic (filtered by `target_id`)
- **Pull**: Periodic reconciliation (default: 5 minutes) via control plane API
- **Validate**: Hash verification before saving
- **Watch**: Channels for real-time update notifications

#### 5. **SnapshotConfigClient** (`server.go`)
- Unified ConfigClient interface implementation
- Merges snapshot + local metadata into single API
- Keys prefixed with `local.` route to LocalMetadata
- Other keys route to snapshot payload
- Falls back to simple viper client when no token/server ID

### Data Flow

```
Control Plane API (/servers/{id})
    │
    ├─ Pull (periodic) ──────────────┐
    │                                 │
    └─ Push (event bus) ─────────────┤
                                      ▼
                          SnapshotConfigManager
                                      │
                                      ├─ Validate (hash check)
                                      ├─ Version comparison
                                      ├─ Atomic write to disk
                                      └─ Notify watchers
                                      ▼
                                    Store
                                      │
                                      ├─ snapshot.yaml (versioned config)
                                      └─ local.yaml (local metadata)
                                      ▼
                          SnapshotConfigClient
                                      │
                                      └─ Merged Get/Set interface
                                      ▼
                              Agent / CLI consumers
```

## Configuration Tiers

### Tier 1: Versioned Snapshot (from control plane)
- Source: `GET /servers/{id}` → `configuration.payload`
- Synced via push (events) + pull (periodic)
- Read-only to local consumers
- Version tracked, hash validated
- Examples: `commands.allow_literal_commands`, `platform.os`

### Tier 2: Local Metadata (node-local)
- Source: Local CLI commands or agent initialization
- Mutable via `ConfigClient.Set()`
- Not synced to control plane
- Examples: `local.auth.token`, `local.server_name`, `local.event_bus.endpoint`

### Merged Access Pattern

```go
// Get from snapshot
allowCommands, ok := config.Get("commands.allow_literal_commands")

// Get from local metadata (prefixed)
token, ok := config.Get("local.auth.token")
serverName, ok := config.Get("local.server_name")

// Set local metadata only
config.Set("local.auth.token", "new-token")

// Get merged config
fullConfig := config.Config() // Returns both tiers merged
```

## Resilience Features

### 1. Stale Snapshot Handling
- Default max age: 24 hours
- Serves stale config with warning if control plane unreachable
- Logs age metadata: `age=2h15m` 
- Configurable threshold per deployment

### 2. Validation
- SHA256 hash verification on load
- Version monotonicity (never downgrade)
- Atomic file writes (temp → rename)

### 3. Graceful Degradation
- Falls back to simple viper client when:
  - No auth token configured
  - No server ID configured
  - Snapshot file corrupted
- CLI continues to work with local-only config

## Usage

### Agent Integration

```go
// In agent startup (wiring.go)
deps, err := WireAgent(ctx, port)
// Automatically creates and starts SnapshotConfigManager
// Server exposes config via GET /api/v1/config

// Config manager lifecycle tied to agent
defer deps.ConfigManager.Stop(ctx)
```

### CLI Integration

```go
// In CLI command
ctx := cmd.Context()
config := config_manager.GetConfig(ctx)

// Automatically uses snapshot client if token exists
// Falls back to simple viper client otherwise
token, ok := config.Get("local.auth.token")
```

### Event Bus Subscription

The config manager subscribes to:
- **Topic**: `server-data-update`
- **Filter**: `target_id={serverID}`
- **Payload**: `{"server_id": "...", "version": 2, "config": {...}}`

When event received:
1. Parse payload
2. Validate version > current
3. Create new snapshot
4. Atomic save
5. Notify watchers

### Watching for Updates

```go
// Consumer can watch for config changes
watchCh := configManager.Watch()

go func() {
    for snapshot := range watchCh {
        log.Printf("Config updated to version %d", snapshot.Version)
        // Reload application config
    }
}()
```

## Files

- `types.go` - ConfigSnapshot, LocalMetadata, validation
- `store.go` - Persistent storage with atomic writes
- `manager.go` - SnapshotConfigManager sync engine
- `server.go` - SnapshotConfigClient unified interface
- `client.go` - Factory functions, fallback logic
- `adapters.go` - Event bus and control plane adapters

## Testing

```bash
# Start agent with config sync
./underleaf_agent serve --port 8081

# Check config status
curl http://localhost:8081/api/v1/config

# View snapshot on disk
cat ~/.underleaf/snapshot.yaml  # CLI
cat /var/lib/underleaf/snapshot.yaml  # Agent

# Trigger update via control plane API
# (will be pushed to agent via event bus or pulled on next reconciliation)
```

## Future Enhancements

1. **Conditional sync** - Skip reconciliation if event bus is live and healthy
2. **Config diff** - Track what changed between versions
3. **Rollback** - Keep last N snapshots for recovery
4. **Metrics** - Prometheus metrics for sync latency, staleness, errors
5. **Compression** - gzip large config payloads
