# DEPRECATED

This package is **deprecated** as of the kernelization refactor (Phase E).

## Replacement

Compiler functionality has been moved to the **deployment_engine UMC** at:
- `umcs/deployment_engine/internal/compiler/`

The deployment engine's compiler:
- Parses deployment specifications
- Builds dependency graphs
- Validates service configurations
- Generates execution plans

## Migration

The deployment_engine UMC handles compilation internally. No direct API access is needed.

Deployment flow:
1. Client sends deployment spec to deployment_engine API
2. Deployment engine compiles the spec using its internal compiler
3. Compiled graph is executed by the runner
4. Results are reported back to Server API

## References

- Deployment Engine Compiler: `umcs/deployment_engine/internal/compiler/`
- UA Kernelization Spec: `underleaf_client/agent_docs/UA_KERNELIZATION_SPEC.md`
