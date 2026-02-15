# DEPRECATED

This package is **deprecated** as of the kernelization refactor (Phase E).

## Replacement

Deployment functionality has been moved to the **deployment_engine UMC** at:
- `umcs/deployment_engine/`

## Migration

The deployment engine is now a standalone UMC (Underleaf Micro-Component) that:
- Runs as a separate process managed by UA-K (Underleaf Agent Kernel)
- Communicates with UA-K via gRPC syscalls over Unix domain sockets
- Handles all deployment compilation, planning, and execution
- Manages Docker containers via the Docker API

## Functionality Mapping

Old (underleaf_client):
- `internal/deployment/handler.go` → **Removed** (handled by deployment_engine API)
- `internal/runner/runner.go` → `umcs/deployment_engine/internal/runner/runner.go`
- `internal/compiler/` → `umcs/deployment_engine/internal/compiler/`
- `internal/recipe/` → **Capability requests via UA-K syscalls**

## Type Preservation

The following types are preserved for backward compatibility with Server API:
- `DeploymentResult` - deployment execution results
- `DeploymentProgress` - deployment progress updates

These types are used by the control plane client and should remain until Server API is updated.

## Removal Timeline

This package will be fully removed in a future release once:
1. Server API is updated to use the new deployment flow
2. All clients have migrated to the deployment_engine UMC
3. Event bus integration is updated

## References

- UA Kernelization Spec: `underleaf_client/agent_docs/UA_KERNELIZATION_SPEC.md`
- Deployment Engine: `umcs/deployment_engine/`
- Kernelization Status: `KERNELIZATION_STATUS.md`
