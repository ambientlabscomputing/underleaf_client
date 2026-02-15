# DEPRECATED

This package is **deprecated** as of the kernelization refactor (Phase E).

## Replacement

Recipe reconciliation and capability management is now handled through:
1. **UA-K syscalls** - `ProviderService` gRPC interface
2. **Capability Manager** - `underleaf_client/internal/capability/`

## New Architecture

With kernelization:
- Deployments with capability requirements go through deployment_engine UMC
- Deployment engine makes syscall requests to UA-K for capability operations
- UA-K's capability manager handles provider installation and management
- No direct recipe reconciliation is needed

## Syscall Interface

Capability operations via syscalls:
- `InstallProvider` - install a capability provider
- `StartProvider` - start a provider process
- `StopProvider` - stop a provider process
- `GrantCapability` - grant capability access to a UMC
- `RevokeCapability` - revoke capability access
- `ListProviders` - list installed providers

## Migration

Old flow (deprecated):
```
Event Bus → DeploymentHandler → RecipeReconciler → CapabilityManager
```

New flow:
```
Event Bus → Deployment Engine UMC → UA-K Syscalls → CapabilityManager
```

## References

- Provider Syscalls: `umc_sdk/proto/ua_kernel/v1/syscall.proto`
- Capability Manager: `underleaf_client/internal/capability/`
- UA Kernelization Spec: `underleaf_client/agent_docs/UA_KERNELIZATION_SPEC.md`
