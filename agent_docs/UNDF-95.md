# UNDF-95: Bug — Agent fails to use existing mTLS cert on startup

## Summary

When an agent already has a valid mTLS certificate on disk (signed by the Underleaf Root CA), the `ufctl start` flow silently skips populating the `mtls.certificate_path` and `mtls.private_key_path` config keys. This causes the agent daemon to connect to the Mycelium Spine without presenting a client certificate, which fails with `tls: certificate required` in production environments where the spine enforces `client_auth: require_and_verify`.

## Background

The `ufctl start` command is the primary onboarding flow. It combines three steps:

1. **Register** the server with the control plane
2. **Bootstrap mTLS** — generate a key, submit a CSR to server_api, save the signed cert, and persist the cert/key paths into the agent config
3. **Start the agent daemon**

This flow calls `SetupMTLSCertificate()` as Step 2 before launching the agent binary.

## Root Cause

`SetupMTLSCertificate()` in [internal/commands/local/mtls_setup.go](internal/commands/local/mtls_setup.go#L24) has an early-return guard that checks if the `.crt` file already exists on disk:

```go
// mtls_setup.go lines 32-36
if _, err := os.Stat(certPath); err == nil {
    printer.PrintSuccess("✓ mTLS certificate already exists at: " + certPath)
    printer.PrintInfo("✓ mTLS authentication is ready to use")
    return nil   // <--- BUG: returns without setting config keys
}
```

The config keys are only written at the **end** of the function, after a fresh CSR is submitted and the cert is saved:

```go
// mtls_setup.go lines 125-126
configClient.Set("local.mtls.certificate_path", certPath)
configClient.Set("local.mtls.private_key_path", keyPath)
```

So when the cert file already exists — for example after a `--existing` reclaim, a re-install on the same machine, or a manual cert setup — the early return fires and the config keys are **never populated**.

Downstream, the agent daemon's wiring reads these config keys to decide whether to attach a client certificate to both the control-plane HTTP client and the spine gRPC connection:

```go
// wiring.go lines 231-232
certPath, hasCert := getConfigValue(configClient, "mtls.certificate_path")
keyPath, hasKey := getConfigValue(configClient, "mtls.private_key_path")

// wiring.go lines 234-235
if hasCert && hasKey && certPath != "" && keyPath != "" {
    // ... attaches client cert
```

`getConfigValue()` ([wiring.go](internal/agent/wiring.go#L70)) tries `local.mtls.certificate_path` first (with the `local.` prefix), then falls back to the bare key `mtls.certificate_path`. If neither exists in the YAML config, the agent silently falls through to the `else` branch and connects with no client cert.

For spine specifically, this means the agent connects with server-only TLS (it verifies the spine's cert against the CA, but never presents its own cert). The production spine (`client_auth: "require_and_verify"`) correctly rejects this with `tls: certificate required`.

## Affected Code Paths

| File | Lines | Role |
|------|-------|------|
| [internal/commands/local/mtls_setup.go](internal/commands/local/mtls_setup.go#L32-L36) | 32-36 | Early return skips config key persistence when cert exists |
| [internal/commands/local/mtls_setup.go](internal/commands/local/mtls_setup.go#L125-L126) | 125-126 | Config keys only written after fresh CSR flow |
| [internal/agent/wiring.go](internal/agent/wiring.go#L231-L252) | 231-252 | Reads config keys to build mTLS HTTP transport |
| [internal/agent/wiring.go](internal/agent/wiring.go#L280-L294) | 280-294 | Reads same keys to attach client cert to spine TLS |
| [internal/agent/wiring.go](internal/agent/wiring.go#L70-L77) | 70-77 | `getConfigValue()` — tries `local.` prefix, then bare key |
| [internal/commands/local/start.go](internal/commands/local/start.go#L217) | 217 | Call site: `ufctl start` calls `SetupMTLSCertificate()` |
| [internal/crypto/csr.go](internal/crypto/csr.go#L199-L207) | 199-207 | `GetCertPaths()` — derives cert/key paths from serverID |

## Reproduction

1. Run `ufctl start` on a fresh machine (cert is generated, config keys are set, agent works)
2. Wipe the config file but leave `~/.underleaf/certs/` intact
3. Run `ufctl start --existing` to reclaim the server identity
4. `SetupMTLSCertificate()` sees the `.crt` on disk → early return → no config keys written
5. Agent daemon starts, finds no `mtls.certificate_path` in config, connects without client cert
6. Spine rejects with `tls: certificate required`

This also affects any scenario where the cert files pre-exist but the config doesn't reference them (manual deployment, config file replacement, etc.).

## Proposed Fix

In the early-return branch of `SetupMTLSCertificate()`, persist the cert/key paths into the config before returning. This ensures the config always has the paths when the cert exists, regardless of whether it was just generated or already present:

```go
// Check if certificate already exists
if _, err := os.Stat(certPath); err == nil {
    printer.PrintSuccess("✓ mTLS certificate already exists at: " + certPath)
    printer.PrintInfo("✓ mTLS authentication is ready to use")

    // Ensure config keys are populated even when cert already exists on disk
    configClient.Set("local.mtls.certificate_path", certPath)
    configClient.Set("local.mtls.private_key_path", keyPath)

    return nil
}
```

This is safe because:
- `GetCertPaths()` deterministically derives paths from `basePath` + `serverID`, so the values are always correct
- `configClient.Set()` is idempotent — writing the same value is a no-op
- The key file path is derived alongside the cert path, and both are generated together, so if the `.crt` exists the `.key` should too (but we might want to also verify the `.key` exists for robustness)

## Acceptance Criteria

1. **Config keys always populated**: After `SetupMTLSCertificate()` returns successfully (whether the cert was freshly generated or already existed), `local.mtls.certificate_path` and `local.mtls.private_key_path` MUST be present in the config
2. **`ufctl start --existing` works end-to-end**: An agent reclaiming an existing server identity (where certs already exist on disk) must connect to the spine with mTLS without manual config editing
3. **Fresh start still works**: The normal first-time `ufctl start` flow (generate key → submit CSR → save cert → set config) must continue to work unchanged
4. **Spine mTLS succeeds in prod**: Agent connects to a spine configured with `client_auth: "require_and_verify"` and presents its client certificate — verified by `"Spine TLS configured with agent mTLS client certificate"` in agent logs
5. **Key file validation** (optional hardening): If the `.crt` exists but the `.key` is missing, the function should not silently return success — it should either regenerate or return an error

