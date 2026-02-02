# Policy Manager Rename - Summary

## Overview
Successfully renamed `config_manager` to `policy_manager` throughout the codebase to clarify its role in the system architecture and distinguish it from the Raft-based KV store.

## Rationale

### Problem
- The name `config_manager` was ambiguous - it could refer to any configuration system
- With the addition of Raft KV store for runtime state, the distinction was unclear
- Developers and users needed clarity on which system to use for different purposes

### Solution
- Renamed to `policy_manager` to emphasize its role: **distributing policy snapshots from control plane**
- Clear separation: 
  - **policy_manager**: Centralized policy distribution (commands allowed, platform settings, etc.)
  - **raft**: Distributed runtime state and coordination (leader election, locks, ephemeral data)

## Changes Made

### 1. Directory Structure
```
internal/config_manager/ → internal/policy_manager/
```

### 2. Package Rename
All files in the package updated:
- `package config_manager` → `package policy_manager`

### 3. Core Type Renames

#### Type Changes
| Old Name | New Name | Purpose |
|----------|----------|---------|
| `ConfigSnapshot` | `PolicySnapshot` | Policy snapshot from control plane |
| `ConfigError` | `PolicyError` | Policy-specific errors |
| `ConfigManager` | `PolicyManager` | Interface for policy management |
| `SnapshotConfigManager` | `SnapshotPolicyManager` | Policy snapshot manager implementation |
| `SnapshotConfigClient` | `SnapshotPolicyClient` | Client backed by policy manager |
| `ControlPlaneConfigClient` | `ControlPlanePolicyClient` | Control plane policy fetching interface |

#### Function Changes
| Old Name | New Name |
|----------|----------|
| `NewConfigSnapshot()` | `NewPolicySnapshot()` |
| `NewSnapshotConfigManager()` | `NewSnapshotPolicyManager()` |
| `NewSnapshotConfigClient()` | `NewSnapshotPolicyClient()` |

### 4. Interface Clarity

The **ConfigClient** interface remains unchanged because:
- It provides unified access to both local configuration and policy
- Keys prefixed with `local.` access local metadata (e.g., `local.auth.token`)
- Other keys access policy snapshots from control plane
- This dual-purpose interface makes sense as "ConfigClient"

```go
// ConfigClient - unified interface for both local config and policy
type ConfigClient interface {
    Get(key string) (interface{}, bool)
    Set(key string, value interface{}) error
    Delete(key string) error
    Config() Configuration
}

// PolicyManager - manages policy snapshots from control plane
type PolicyManager interface {
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    GetSnapshot() (*PolicySnapshot, error)
    Watch() <-chan *PolicySnapshot
}
```

### 5. Field and Variable Renames

#### Agent Dependencies
```go
type Dependencies struct {
    Config        policy_manager.ConfigClient  // Unchanged - unified interface
    PolicyManager policy_manager.PolicyManager // Was: ConfigManager
    // ...
}
```

#### MetricsCollector
```go
type MetricsCollector struct {
    policyManager policy_manager.PolicyManager  // Was: configManager
    // ...
}
```

#### Variable Names
- `snapshotManager` → `policyManager` (for consistency)
- `configManager` → `policyManager` (field names)

### 6. Documentation Updates

#### README.md Updates
- Package overview emphasizes "policy distribution" role
- Data flow diagram shows `SnapshotPolicyManager`
- Section renamed to "Policy Tiers" from "Configuration Tiers"

#### Comments and Documentation
- Updated all comments to reflect policy management semantics
- Clarified distinction between policy (from control plane) and runtime state (Raft)

### 7. Import Path Changes

All files updated:
```go
// Before
import "github.com/ambientlabscomputing/underleaf_client/internal/config_manager"

// After  
import "github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
```

Files affected: 50+ files across:
- `internal/agent/`
- `internal/commands/`
- `internal/controlplane/`
- `internal/types/`
- `cmd/ufctl/`

## Architecture Clarity

### System Roles Now Clear

#### Policy Manager (policy_manager)
- **Purpose**: Distribute policy from control plane to edge nodes
- **Data**: Commands whitelist, platform settings, feature flags, quotas
- **Source**: Control plane API + Event bus
- **Sync**: Push (events) + Pull (periodic reconciliation)
- **Mutability**: Read-only on edge (controlled by control plane)
- **Storage**: Local snapshot files with version tracking

#### Raft KV Store (raft)
- **Purpose**: Quorum-based distributed state and coordination
- **Data**: Leader election state, distributed locks, ephemeral runtime data
- **Source**: Local cluster consensus
- **Sync**: Raft consensus algorithm
- **Mutability**: Read-write by cluster members
- **Storage**: BoltDB log with snapshots

### When to Use Which?

**Use Policy Manager for:**
- Control plane policy distribution
- Platform-wide settings
- Command execution rules
- Centrally managed configuration

**Use Raft KV for:**
- Cluster coordination
- Leader election
- Distributed locks and leases
- Ephemeral runtime state
- High-availability requirements

## Testing

All packages compile successfully:
```bash
✅ go build ./internal/policy_manager/...
✅ go build ./internal/agent/...
✅ go build ./internal/commands/...
✅ go build ./cmd/ufctl
✅ go build ./cmd/underleaf_agent
```

## Migration Notes

### For Developers

1. **Import changes**: Update imports from `config_manager` to `policy_manager`
2. **Type changes**: Update type references as per table above
3. **Field names**: `ConfigManager` → `PolicyManager` in structs
4. **Variable names**: Consider renaming `configManager` to `policyManager` for clarity

### Backward Compatibility

- File paths remain the same (snapshot.yaml, config.yaml)
- ConfigClient interface unchanged (no API breaking changes)
- Wire protocol and storage format unchanged

## Benefits

1. **Clarity**: Clear distinction between policy distribution and runtime state
2. **Maintainability**: Easier to understand which system to use
3. **Documentation**: Better semantic alignment with actual purpose
4. **Extensibility**: Clear boundaries for future enhancements
5. **Developer Experience**: Less confusion when reading code

## Related Systems

```
┌─────────────────────┐
│  Control Plane API  │  (Source of Truth)
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐
│  Policy Manager     │  (Policy Distribution)
│  - Snapshots        │
│  - Versioning       │
│  - Push/Pull Sync   │
└─────────────────────┘

┌─────────────────────┐
│  Raft KV Store      │  (Runtime Coordination)
│  - Consensus        │
│  - Leader Election  │
│  - Distributed Lock │
└─────────────────────┘
```

## Conclusion

The rename from `config_manager` to `policy_manager` successfully clarifies the system architecture and provides a clear semantic distinction between centralized policy distribution and distributed runtime state management. All code compiles successfully and the changes are backward compatible at the storage and protocol level.
