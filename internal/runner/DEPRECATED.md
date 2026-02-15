# DEPRECATED

This package is **deprecated** as of the kernelization refactor (Phase E).

## Replacement

Runner functionality has been moved to the **deployment_engine UMC** at:
- `umcs/deployment_engine/internal/runner/runner.go`

The deployment engine's runner:
- Implements full Docker container lifecycle (pull, create, start, stop)
- Handles image pulling with proper error handling
- Manages container networking and port bindings
- Provides deployment execution results

## Migration

Use the deployment_engine UMC instead:

```bash
# Old: UA-K directly runs deployments
# (no longer supported)

# New: UA-K delegates to deployment_engine UMC
# POST to deployment_engine API at http://localhost:8080/deploy
curl -X POST http://localhost:8080/deploy \
  -H "Content-Type: application/json" \
  -d '{"deployment_id": "...", "spec": {...}}'
```

## References

- Deployment Engine Runner: `umcs/deployment_engine/internal/runner/runner.go`
- UA Kernelization Spec: `underleaf_client/agent_docs/UA_KERNELIZATION_SPEC.md`
