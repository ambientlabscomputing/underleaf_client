# Deployment Compiler Guide

The Underleaf Deployment Compiler is a sophisticated system that transforms high-level application deployment specifications into low-level Docker resources using a Kubernetes-inspired three-way diff reconciliation pattern.

## Overview

The compiler operates in several phases:

1. **Compilation**: Transform AppDeployment → CompiledGraph
2. **Observation**: Query current Docker state
3. **State Loading**: Load last applied state from JSON files
4. **Reconciliation**: Three-way diff (Desired vs Observed vs LastApplied)
5. **Planning**: Generate ordered execution plan
6. **Execution**: Execute Docker operations
7. **State Persistence**: Save new last applied state

## Architecture

### Components

```
internal/compiler/
├── types.go              # Core types (ResourceID, Operation, ExecutionPlan, etc.)
├── compiler.go           # AppDeployment → CompiledGraph transformation
├── reconciler.go         # Three-way diff reconciliation
├── planner.go            # Execution plan generation
├── graph.go              # Dependency graph utilities
└── state/
    ├── observed.go       # Docker state querying
    └── lastapplied.go    # JSON-based state persistence

internal/runner/
└── runner.go             # Plan execution engine

cmd/ufctl/
└── deploy.go             # CLI integration
```

## Resource Naming

All Docker resources are prefixed with the deployment slug:
- Network: `{slug}_{network_name}` (e.g., `mywebapp_app_network`)
- Volume: `{slug}_{volume_name}` (e.g., `mywebapp_db_data`)
- Container: `{slug}_{service_name}` (e.g., `mywebapp_database`)

## Resource Labels

All managed resources are labeled:
```yaml
underleaf.deployment: "{deployment_id}"
underleaf.slug: "{deployment_slug}"
underleaf.type: "{resource_type}"
```

These labels enable:
- Identifying managed vs manual resources
- Filtering resources by deployment
- Detecting orphaned resources

## Three-Way Diff Logic

The reconciler compares three states:

1. **Desired**: What should exist (from CompiledGraph)
2. **Observed**: What actually exists (from Docker)
3. **LastApplied**: What we last applied (from JSON files)

### Reconciliation Cases

For each desired resource:

| Observed | LastApplied | Action |
|----------|-------------|--------|
| nil | nil | **CREATE** - New resource |
| nil | exists | **CREATE** - Was deleted externally |
| exists (unmanaged) | nil | **ADOPT** (if flag set) or **ERROR** |
| exists (managed) | nil | **UPDATE** - Orphaned resource |
| exists (managed) | exists | **UPDATE** if drift detected, **NOOP** otherwise |

For observed resources not in desired:
- If in LastApplied: **DELETE** (we created it, now unwanted)
- If not in LastApplied: **IGNORE** (manual resource)

### Drift Detection

Configuration drift is detected by comparing JSON-marshaled configs:
- If `desiredConfig != observedConfig`: Resource has drifted → UPDATE
- If configs match: No drift → NOOP

## Execution Plan Ordering

Operations are ordered to respect dependencies:

### Creation Order
1. Networks (no dependencies)
2. Volumes (no dependencies)
3. Containers (topologically sorted by depends_on)

### Deletion Order
1. Containers (reverse topological order)
2. Volumes
3. Networks

### Update/Mixed Order
- Deletes first
- Updates second
- Creates last (respecting dependencies)
- Other operations (noop, adopt) anytime

## State Persistence

Last applied state is stored as JSON files:

```
{state_dir}/
└── {deployment_id}/
    ├── v1.json         # Version 1 snapshot
    ├── v2.json         # Version 2 snapshot
    ├── v3.json         # Version 3 snapshot
    └── latest.json     # Symlink/copy of most recent
```

### Snapshot Structure
```json
{
  "deployment_id": "deploy-001",
  "version": 3,
  "applied_at": "2024-01-15T10:30:00Z",
  "resources": {
    "deploy-001:network:mywebapp_app_network": {
      "driver": "bridge",
      "labels": {...}
    },
    "deploy-001:volume:mywebapp_db_data": {
      "labels": {...}
    },
    "deploy-001:container:mywebapp_database": {
      "image": "postgres:15-alpine",
      "environment": {...},
      "labels": {...}
    }
  }
}
```

## Failure Handling

### Partial State Strategy

On failure, the system:
1. **STOPS execution** immediately
2. **LEAVES resources** in partial state (doesn't rollback)
3. **GENERATES detailed report** with:
   - Which operations succeeded
   - Which operation failed (with error message)
   - Exact timestamps and durations
   - Full operation details

### Execution Reports

Reports are saved to `{report_dir}/execution_{deployment_id}_v{version}_{timestamp}.json`

```json
{
  "plan_id": "plan-xyz",
  "deployment_id": "deploy-001",
  "version": 3,
  "started_at": "2024-01-15T10:30:00Z",
  "completed_at": "2024-01-15T10:30:45Z",
  "success": false,
  "partial_state": true,
  "error": "Operation failed: Create container mywebapp_api - image pull failed",
  "results": [
    {
      "operation_id": "op-001",
      "resource_id": "deploy-001:network:mywebapp_app_network",
      "type": "create",
      "success": true,
      "started_at": "2024-01-15T10:30:00Z",
      "completed_at": "2024-01-15T10:30:02Z",
      "duration": "2s"
    },
    {
      "operation_id": "op-002",
      "resource_id": "deploy-001:container:mywebapp_api",
      "type": "create",
      "success": false,
      "error": "failed to pull image: connection timeout",
      "started_at": "2024-01-15T10:30:02Z",
      "completed_at": "2024-01-15T10:30:45Z",
      "duration": "43s"
    }
  ]
}
```

### Recovery from Failure

To recover from a failed deployment:
1. Fix the underlying issue (e.g., network connectivity, image availability)
2. Re-run the same deployment
3. Reconciler will:
   - Detect already-created resources (via LastApplied)
   - Skip them (NOOP)
   - Retry failed operations
   - Continue from where it left off

## Usage Examples

### Basic Deployment

```bash
# Deploy an application
ufctl deploy examples/deployment-example.json

# Output:
# 📦 Underleaf Deployment Compiler
# ================================
# 
# 📄 Loading deployment from examples/deployment-example.json...
#   ✅ Loaded deployment: mywebapp (v1)
# 
# 🗄️  Initializing state stores...
#   ✅ State stores ready
# 
# ⚙️  Compiling deployment graph...
#   ✅ Graph compiled: 6 resources, 5 dependencies
#   📋 Creation order: [network:app_network, volume:db_data, container:database, container:api, container:web]
# 
# 🔍 Querying observed Docker state...
#   ✅ Found: 0 networks, 0 volumes, 0 containers
# 
# 📜 Loading last applied state...
#   ⚠️  No previous state found (new deployment)
# 
# 🔄 Performing three-way reconciliation...
#   ✅ Diff computed: 6 operations
#      - create: 6
# 
# 📋 Generating execution plan...
#   ✅ Plan generated: 6 operations, ~30 seconds
#   🔗 Plan ID: plan-abc123
# 
# 🚀 Executing deployment plan for mywebapp (v2)
# [1/6] create - Create network mywebapp_app_network
#   ✅ Success (1.2s)
# [2/6] create - Create volume mywebapp_db_data
#   ✅ Success (0.8s)
# [3/6] create - Create container mywebapp_database
#   📥 Pulling image postgres:15-alpine...
#   ✅ Success (15.3s)
# [4/6] create - Create container mywebapp_api
#   ✅ Success (2.1s)
# [5/6] create - Create container mywebapp_web
#   ✅ Success (1.9s)
# 
# ✅ Deployment successful!
# 
# 💾 Saving last applied state...
#   ✅ State saved (v2)
```

### Dry Run

```bash
# Preview changes without executing
ufctl deploy examples/deployment-example.json --dry-run
```

### Update Deployment

```bash
# Modify deployment-example.json (e.g., change image version)
# Re-deploy
ufctl deploy examples/deployment-example.json

# Reconciler will:
# - Detect changed containers → UPDATE (stop, remove, recreate)
# - Detect unchanged resources → NOOP
```

### Prune Unknown Resources

```bash
# Remove resources with matching slug that aren't in desired state
ufctl deploy examples/deployment-example.json --prune-unknown
```

### Adopt Orphaned Resources

```bash
# Adopt existing resources if config matches
ufctl deploy examples/deployment-example.json --adopt-orphans
```

## Advanced Scenarios

### Handling External Changes

If someone manually modifies a managed resource:

1. Next deployment detects drift (config doesn't match LastApplied)
2. Resource is marked for UPDATE
3. Container is recreated with desired config
4. New state saved to LastApplied

### Handling Manual Deletions

If someone manually deletes a managed resource:

1. Observed state shows resource missing
2. LastApplied shows we created it
3. Resource is marked for CREATE
4. Resource is recreated

### Partial Deployment Recovery

Deployment fails halfway through:
1. Some resources created successfully
2. One resource fails (e.g., image pull timeout)
3. Execution stops, partial state left
4. Detailed report generated

Next deployment:
1. Loads LastApplied (partial state)
2. Reconciles: existing resources → NOOP, missing → CREATE
3. Effectively resumes from failure point

## Configuration

### State Directory
Default: `./state`
Override: `--state-dir /path/to/state`

### Report Directory
Default: `./reports`
Override: `--report-dir /path/to/reports`

## Dependencies

The compiler uses:
- **Docker Go SDK** (`github.com/moby/moby/client`) for Docker operations
- **Standard encoding/json** for state persistence
- **No SQLite** - pure JSON file storage

## Best Practices

1. **Version Control Deployments**: Store deployment JSON files in git
2. **Keep State Directory**: Don't delete state/ - needed for proper reconciliation
3. **Review Dry Runs**: Always run `--dry-run` first for complex changes
4. **Monitor Reports**: Check execution reports after deployments
5. **Backup State**: State directory contains deployment history
6. **Use Slugs Wisely**: Slug becomes part of all resource names - keep it short

## Troubleshooting

### "Resource already exists" errors
- Another deployment using same slug
- Solution: Use unique slug per deployment

### Reconciliation shows unexpected UPDATEs
- Check LastApplied state - may be stale
- Compare observed vs last applied configs
- Look for external modifications

### Partial state after failure
- Check execution report for exact failure
- Fix underlying issue
- Re-run deployment (will resume)

### Resources not cleaned up
- Ensure deployment has consistent ID/slug
- Check resources have correct labels
- LastApplied must exist for cleanup

## Future Enhancements

Potential future features:
- Rollback to previous version
- Blue/green deployments
- Health checks and readiness probes
- Resource limits and quotas
- Secret management integration
- Multi-server orchestration via targeting
