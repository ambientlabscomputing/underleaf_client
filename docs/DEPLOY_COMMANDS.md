# Deploy Command Suite

The `ufctl deploy` command suite provides tools for managing application deployments using the three-way diff reconciliation pattern (Desired, Observed, LastApplied).

## Commands

### `ufctl deploy list [directory]`

Lists all deployment JSON files in a directory.

**Examples:**
```bash
ufctl deploy list
ufctl deploy list ./deployments
ufctl deploy list examples/
```

**Output:**
- FILE: Deployment filename
- ID: Deployment ID
- SLUG: Deployment slug (used for resource naming)
- NAME: Human-readable name
- VERSION: Current version number
- SERVICES: Number of container services
- NETWORKS: Number of networks
- VOLUMES: Number of volumes

### `ufctl deploy compile [deployment-file]`

Compiles a deployment specification into a dependency graph with topologically sorted creation and deletion orders.

**Examples:**
```bash
ufctl deploy compile my-app.json
ufctl deploy compile my-app.json -o compiled.json
ufctl deploy compile examples/deployment-example.json --output /tmp/result.json
```

**Flags:**
- `-o, --output <file>`: Write output to JSON file instead of stdout

**Output Format:**
```json
{
  "deployment": {...},
  "compiled_service": {
    "nodes": [...],
    "edges": [...],
    "creation_order": [...],
    "deletion_order": [...]
  }
}
```

**What it does:**
1. Loads deployment specification from JSON file
2. Compiles to dependency graph with:
   - Resource nodes (networks, volumes, containers)
   - Dependency edges (e.g., containers depend on networks)
   - Topologically sorted creation order (dependencies first)
   - Reverse deletion order (dependents deleted first)
3. Outputs compiled graph with slug-prefixed resource names

### `ufctl deploy plan [deployment-file]`

Performs full three-way reconciliation and generates an execution plan.

**Examples:**
```bash
ufctl deploy plan my-app.json
ufctl deploy plan my-app.json -o plan.json
ufctl deploy plan my-app.json --state-dir ./state
ufctl deploy plan my-app.json --prune-unknown --adopt-orphans
```

**Flags:**
- `-o, --output <file>`: Write plan to JSON file
- `--state-dir <dir>`: Directory for last-applied state (default: ./state)
- `--prune-unknown`: Remove unknown resources with matching slug
- `--adopt-orphans`: Adopt orphaned resources if config matches

**Output Format:**
```json
{
  "deployment": {...},
  "compiled_graph": {...},
  "observed_state": {...},
  "last_applied": {...},
  "diff_results": {...},
  "execution_plan": {...}
}
```

**What it does:**
1. Compiles deployment to graph
2. Queries Docker for observed state (networks, volumes, containers)
3. Loads last-applied state from JSON files
4. Performs three-way reconciliation:
   - **New resources**: Create operations
   - **Deleted resources**: Delete operations (if in last-applied)
   - **Drifted resources**: Update operations (config mismatch)
   - **Unknown resources**: Optional prune operations
   - **Orphaned resources**: Optional adopt operations
5. Generates ordered execution plan with dependencies

### `ufctl deploy diff [deployment-file]`

Shows what changes would be made without executing them (like Terraform plan).

**Examples:**
```bash
ufctl deploy diff my-app.json
ufctl deploy diff my-app.json --show-configs
ufctl deploy diff my-app.json --state-dir ./state
```

**Flags:**
- `--state-dir <dir>`: Directory for last-applied state (default: ./state)
- `--show-configs`: Show detailed desired vs observed configurations

**Output:**
- Table showing operations (create, update, delete, noop)
- Resource type and name
- Reason for each operation
- Summary counts by operation type

**What it does:**
1. Same as `plan` but with user-friendly table output
2. Shows each operation with TYPE, RESOURCE, NAME, REASON
3. Color-coded operation types:
   - 🟢 create (green)
   - 🟡 update (yellow)
   - 🔴 delete (red)
   - ⚪ noop (gray)

## Reconciliation Logic

The deployment compiler uses a sophisticated three-way diff pattern:

### State Sources

1. **Desired State**: Compiled from deployment JSON
   - Networks, volumes, containers with dependencies
   - Slug-prefixed names (e.g., `mywebapp_database`)
   - Labels: `underleaf.deployment`, `underleaf.slug`, `underleaf.type`

2. **Observed State**: Queried from Docker
   - Current networks, volumes, containers
   - Filtered by deployment ID or slug labels

3. **Last Applied**: Persisted JSON state
   - Stored in `<state-dir>/<deployment-id>/vN.json`
   - Tracks what we last successfully applied

### Reconciliation Cases

| Observed | Last Applied | Action |
|----------|-------------|--------|
| ✅ exists | ✅ exists | Compare configs → update if drift |
| ✅ exists | ❌ missing | Unknown resource → adopt or ignore |
| ❌ missing | ✅ exists | Deleted externally → recreate |
| ❌ missing | ❌ missing | New resource → create |

### Execution Order

1. **Delete operations** (reverse topology)
   - Containers before volumes/networks
   - Respects dependencies
   
2. **Update operations** (any order)
   - Config drift corrections
   
3. **Create operations** (forward topology)
   - Networks/volumes before containers
   - Respects dependencies

## Example Workflow

```bash
# 1. List available deployments
./ufctl deploy list examples/

# 2. See what would be deployed (dry-run)
./ufctl deploy diff examples/deployment-example.json

# 3. Compile to see dependency graph
./ufctl deploy compile examples/deployment-example.json -o /tmp/graph.json

# 4. Generate full execution plan
./ufctl deploy plan examples/deployment-example.json -o /tmp/plan.json

# 5. Execute plan (not yet implemented - use runner directly)
# ./ufctl deploy apply /tmp/plan.json
```

## State Management

### Last-Applied State Location

```
./state/
├── deploy-001/
│   ├── latest.json       # Symlink to latest version
│   ├── v1.json          # Version 1 state
│   └── v2.json          # Version 2 state
└── deploy-002/
    └── v1.json
```

### State Contents

Each version file contains:
```json
{
  "deployment_id": "deploy-001",
  "version": 1,
  "timestamp": "2024-01-15T10:30:00Z",
  "resources": {
    "networks": [...],
    "volumes": [...],
    "containers": [...]
  }
}
```

## Docker Integration

### Resource Naming

All resources are prefixed with the deployment slug:
- Network: `mywebapp_app_network`
- Volume: `mywebapp_db_data`
- Container: `mywebapp_database`

### Labels

Every resource gets these labels:
```yaml
underleaf.deployment: "deploy-001"
underleaf.slug: "mywebapp"
underleaf.type: "network|volume|container"
```

### Querying

The compiler queries Docker using:
- **Networks**: Filter by label or name prefix
- **Volumes**: Filter by label or name prefix
- **Containers**: Filter by label or name prefix

## Testing the Compiler

### Minimal Test Deployment

Create `test-deployment.json`:
```json
{
  "id": "test-001",
  "slug": "testapp",
  "name": "Test Application",
  "version": 1,
  "networks": [
    {
      "name": "app_network",
      "driver": "bridge"
    }
  ],
  "volumes": [
    {
      "name": "app_data"
    }
  ],
  "services": [
    {
      "name": "web",
      "image": "nginx:alpine",
      "networks": ["app_network"],
      "ports": [
        {
          "container_port": 80,
          "host_port": 8080
        }
      ]
    }
  ]
}
```

Test sequence:
```bash
# Compile
./ufctl deploy compile test-deployment.json

# Diff (should show 3 creates)
./ufctl deploy diff test-deployment.json

# Plan
./ufctl deploy plan test-deployment.json -o /tmp/test-plan.json
```

## Next Steps

To complete the deployment system, implement:

1. **Apply command**: Execute plans
   ```bash
   ufctl deploy apply [deployment-file]
   ufctl deploy apply --plan /tmp/plan.json
   ```

2. **Destroy command**: Delete all resources
   ```bash
   ufctl deploy destroy [deployment-file]
   ufctl deploy destroy deploy-001
   ```

3. **Rollback command**: Revert to previous version
   ```bash
   ufctl deploy rollback deploy-001 --to-version 2
   ```

4. **Status command**: Show current state
   ```bash
   ufctl deploy status deploy-001
   ```

## Architecture

The deploy commands integrate these packages:

- **internal/types/deployment.go**: Shared type definitions
- **internal/compiler/compiler.go**: AppDeployment → CompiledGraph
- **internal/compiler/graph.go**: Topological sorting
- **internal/compiler/reconciler.go**: Three-way diff logic
- **internal/compiler/planner.go**: Execution plan generation
- **internal/compiler/state/lastapplied.go**: JSON state persistence
- **internal/compiler/state/observed.go**: Docker state querying
- **internal/runner/runner.go**: Plan execution (for future apply command)
