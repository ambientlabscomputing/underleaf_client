# Dev Mode: Local UMC Development

Dev mode lets you load UA-Managed Component (UMC) binaries from local paths on disk, bypassing UCRS registration and download. This enables fast iteration when developing across the agent and one or more UMCs without publishing to the registry.

Dev mode is **build-tag gated** (`//go:build dev`). The standard `make build` produces a clean production binary with zero dev-mode code included. Dev mode is only included in binaries built with `make build-dev`.

---

## Quick Start

```bash
# 1. Copy the example config and customize it
cp build.yaml.example build.yaml

# 2. Edit build.yaml to point to your local UMC builds
#    (see Configuration section below)

# 3. Build the dev-mode binaries
make build-dev

# 4. Start the agent with dev mode enabled
ufctl start --dev-mode --build-config ./build.yaml -p 2023
```

The agent will print a banner when running in dev mode:

```
⚠ DEV MODE — Loading UMCs from local paths
  deployment-engine: ../umcs/deployment_engine/bin/serve (watch: true)
  Agent: Skip UCRS sync
```

---

## Configuration: `build.yaml`

Create a `build.yaml` file (it is `.gitignore`d — each developer maintains their own). Use `build.yaml.example` as a starting point.

```yaml
version: "1"

# Global defaults applied to all overrides unless explicitly set per-UMC
defaults:
  env:
    LOG_LEVEL: debug
    KERNEL_SOCKET: /tmp/ua_kernel.sock
  watch: true              # Restart UMC automatically when its binary changes

# Per-UMC overrides — any UMC not listed loads from UCRS normally
overrides:
  deployment-engine:
    binary: ../umcs/deployment_engine/bin/serve   # relative to build.yaml location
    health_endpoint: http://localhost:10081/health
    health_timeout: 10s

  cron-engine:
    binary: ../umcs/cron_engine/bin/serve
    health_endpoint: http://localhost:8081/health

  # Any provider by its UCRS provider ID:
  my-custom-provider:
    binary: /absolute/path/to/provider
    args: [--verbose]
    env:
      MY_CONFIG: /path/to/dev-config.yaml
    watch: false           # Disable per-UMC (overrides global default)

# Agent-level settings
agent:
  skip_spine: false        # true = don't connect to Mycelium Spine (offline dev)
  skip_ucrs_sync: true     # Don't sync from UCRS — use only local overrides
  capability_registry:
    enabled: false         # Disable the capability manager entirely if needed
```

### Path Resolution

Binary paths are resolved relative to the **directory containing `build.yaml`**, not the current working directory. This lets you reference sibling directories in a monorepo:

```
underleaf/
├── build.yaml              # paths like ../umcs/deployment_engine/bin/serve
├── underleaf_client/
│   └── ...
└── umcs/
    └── deployment_engine/
        └── bin/serve       # ← this binary
```

### Partial Overrides

You only need to list UMCs you want to override locally. Any UMC not in `overrides` follows the standard UCRS resolution path. For example, override just the deployment engine while letting cron engine load from the registry.

---

## Build Targets

```bash
# Build both binaries with dev mode enabled
make build-dev

# Build only ufctl with dev mode
make build-cli-dev

# Build only underleaf_agent with dev mode
make build-agent-dev

# Build dev binaries and install to /usr/local/bin
make bni-dev
```

Standard `make build` and `make bni` always produce production binaries without dev-mode code.

---

## CLI Flags

When running a dev-mode binary, two additional flags are available on `ufctl start` and `ufctl agent start`:

| Flag | Default | Description |
|------|---------|-------------|
| `--dev-mode` | `false` | Enable dev mode |
| `--build-config` | `./build.yaml` | Path to the `build.yaml` config file |

These flags are **not present** in production builds.

```bash
# Use default build.yaml in current directory
ufctl start --dev-mode

# Specify a different location
ufctl start --dev-mode --build-config /path/to/build.yaml

# Agent-only start
ufctl agent start --dev --dev-mode --build-config ./build.yaml
```

---

## Auto-Restart on Rebuild (Watch Mode)

When `watch: true` is set (globally or per-UMC), the agent polls the binary's modification time every 2 seconds. When a change is detected, the UMC is restarted automatically.

**Typical workflow:**

```bash
# Terminal 1: Run the agent with dev mode
ufctl start --dev-mode -p 2023

# Terminal 2: Rebuild a UMC and it restarts automatically
cd umcs/deployment_engine && make build
# → Agent logs: "DEV MODE: binary changed, restarting" → UMC restarts
```

You can also use `entr` or `fswatch` to trigger `make build` automatically on source changes:

```bash
# Rebuild and auto-restart when Go source files change
find . -name '*.go' | entr make build
```

---

## Startup Validation

Before launching anything, the agent validates all overrides listed in `build.yaml`:

- **Binary exists and is executable** — fails fast with a clear message pointing to the resolved absolute path
- **Port conflicts** — warns if multiple overrides specify health endpoints on the same port

```
DEV MODE ERROR: binary not found for "deployment-engine" at
  /Users/you/umcs/deployment_engine/bin/serve
  → Run 'make build' in the deployment_engine directory
```

---

## Agent Dev Settings

The `agent:` section of `build.yaml` controls agent-level behavior:

| Setting | Default | Effect |
|---------|---------|--------|
| `skip_spine` | `false` | Skip Mycelium Spine connection (useful for fully offline dev) |
| `skip_ucrs_sync` | `false` | Skip UCRS registry sync — the capability manager starts but doesn't pull from the registry |
| `capability_registry.enabled` | `true` | Set to `false` to disable the capability manager entirely |

For typical UMC development, `skip_ucrs_sync: true` with `capability_registry.enabled: false` is the recommended configuration, as it avoids needing Docker and a running UCRS instance.

---

## How It Works

```
ufctl start --dev-mode --build-config ./build.yaml
  │
  ├── Parses build.yaml, validates binaries
  ├── Prints dev mode banner
  │
  └── WireAgent(ctx, port, devConfig)
        │
        ├── Deployment Engine launch
        │     └── if override exists → use local binary instead of PATH search
        │
        ├── Spine connection
        │     └── if skip_spine → skip entirely
        │
        ├── Capability Manager
        │     ├── if !enabled → skip entirely
        │     └── if skip_ucrs_sync → start manager but skip UCRS sync
        │
        └── File watcher (if watch: true)
              └── polls binary mtime every 2s → restarts UMC on change
```

Dev mode is implemented across build-tag-separated files:

| File | Build Tag | Purpose |
|------|-----------|---------|
| `internal/devmode/config.go` | `dev` | Config parsing, path resolution, env merging |
| `internal/devmode/stub.go` | `!dev` | No-op types; production builds compile out all dev paths |
| `internal/devmode/watcher.go` | `dev` | Poll-based binary file watcher |
| `internal/devmode/validate.go` | `dev` | Startup binary validation |
| `internal/agent/wiring_dev.go` | `dev` | Dev-mode helper functions called from `WireAgent` |
| `internal/agent/wiring_prod.go` | `!dev` | No-op stubs for the same helpers |
| `internal/commands/local/flags_dev.go` | `dev` | Registers `--dev-mode` and `--build-config` flags |

---

## Example: Minimal Setup for Deployment Engine Development

```yaml
# build.yaml
version: "1"

defaults:
  watch: true

overrides:
  deployment-engine:
    binary: ../umcs/deployment_engine/bin/deployment-engine-serve
    health_endpoint: http://localhost:10081/health

agent:
  skip_ucrs_sync: true
  capability_registry:
    enabled: false
```

```bash
# Build agent with dev support
cd underleaf_client && make build-dev

# Start (from the monorepo root so relative paths resolve correctly)
cd ..
underleaf_client/ufctl start --dev-mode --build-config ./build.yaml -p 2023
```

When the agent starts, it uses your local `deployment-engine-serve` binary instead of looking for a UCRS-installed one. Rebuild `deployment-engine-serve` in another terminal and the agent automatically restarts it.
