# Dev Mode: Local UMC Development Plan

> Build flag-gated development mode that lets developers bypass UCRS and load UMC binaries from local paths, enabling fast cross-component iteration without registry publishing.

## Problem

When developing across multiple UMCs (deployment engine, cron engine, MMA, or custom providers), the standard flow requires:

1. Build the UMC binary
2. Publish it to UCRS (with Ed25519-signed snapshot)
3. Wait for the agent's `SyncClient` to pull the updated snapshot
4. Have the `CapabilityManager` resolve, download, verify, and install it

This round-trip is prohibitive during development. Go's `replace` directives don't help because UMC binaries are separate OS processes — they're not imported as Go modules by the agent.

## Solution Overview

A **build-tag-gated dev mode** (`//go:build dev`) that introduces:

- A `--dev-mode` flag on `ufctl start` / `ufctl agent start`
- A `build.yaml` config file specifying local binary overrides and dev-time settings
- A `DevOverrideResolver` that short-circuits the UCRS → download → install pipeline
- File-watching for automatic UMC hot-restart on binary rebuild

The build tag ensures **zero dev-mode code ships in production binaries**. The standard `make build` produces a clean binary; `make build-dev` includes the dev plumbing.

---

## Design

### `build.yaml` Schema

```yaml
# build.yaml — dev-mode configuration for local UMC development
version: "1"

# Global defaults applied to all overrides unless explicitly set per-UMC
defaults:
  env:
    LOG_LEVEL: debug
    KERNEL_SOCKET: /tmp/ua_kernel.sock
  watch: true              # Enable file-watching for auto-restart
  skip_signature_check: true  # Bypass Ed25519 verification on snapshots

# Override specific UMCs with local binary paths
overrides:
  # Override the deployment engine
  deployment-engine:
    binary: ../umcs/deployment_engine/bin/serve
    args: []
    env:
      CUSTOM_VAR: "value"
    watch: true            # Override global default
    health_endpoint: http://localhost:10081/health
    health_timeout: 10s

  # Override the cron engine
  cron-engine:
    binary: ../umcs/cron_engine/bin/serve
    health_endpoint: http://localhost:8081/health

  # Override MMA
  mma:
    binary: ../umcs/mycelium_mesh_agent/bin/serve
    health_endpoint: http://localhost:8082/health

  # Override a custom/third-party provider by its UCRS provider ID
  my-custom-provider:
    binary: /absolute/path/to/my-provider
    args: ["--verbose"]
    env:
      MY_CONFIG: "/path/to/dev-config.yaml"
    watch: true

# Optional: override the agent itself (for kernel development)
agent:
  skip_mtls: false         # Set true to skip mTLS setup (useful for pure-local dev)
  skip_spine: false        # Set true to skip Spine connection (offline dev)
  skip_ucrs_sync: true     # Don't sync from UCRS at all
  capability_registry:
    enabled: false         # Disable the full capability manager in dev mode
```

**Key design decisions:**

1. **Paths are relative to `build.yaml` location** — so a monorepo `build.yaml` at the root can reference `../umcs/deployment_engine/bin/serve` and it'll resolve correctly regardless of where `ufctl` is invoked from.

2. **Per-UMC granularity** — you can override just the deployment engine while letting other UMCs load from UCRS normally. Any UMC not listed in `overrides` follows the standard resolution path.

3. **Watch mode** — when `watch: true`, the agent monitors the binary's mtime. On change, it sends SIGTERM → waits grace period → restarts. This pairs with `make build` in a separate terminal or an `fswatch`/`entr` loop. No need for complex file-system watchers in the agent itself — just periodic stat polling (every 2s).

4. **Provider ID mapping** — the override keys map to well-known names (`deployment-engine`, `cron-engine`, `mma`) and to arbitrary UCRS provider IDs for custom providers. The `DevOverrideResolver` checks the override map before falling through to the standard `Resolver`.

---

### Architecture

```
┌─────────────────────────────────────────────────────────┐
│  ufctl start --dev-mode --build-config ./build.yaml     │
│                                                          │
│  Flags (dev build only):                                 │
│    --dev-mode          Enable dev mode                   │
│    --build-config      Path to build.yaml (default: ./build.yaml) │
└─────────────┬───────────────────────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────────────────────┐
│  Agent Launcher                                          │
│                                                          │
│  if devMode {                                            │
│    parse build.yaml                                      │
│    inject DevConfig into WireAgent()                     │
│  }                                                       │
└─────────────┬───────────────────────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────────────────────┐
│  WireAgent(ctx, ..., devConfig *DevConfig)               │
│                                                          │
│  Standard wiring, but at each UMC launch point:         │
│                                                          │
│  1. Deployment Engine launch:                            │
│     if devConfig.Has("deployment-engine") {              │
│       use devConfig.Binary instead of PATH search        │
│       merge devConfig.Env with standard env              │
│       start file watcher if watch=true                   │
│     }                                                    │
│                                                          │
│  2. Capability Manager:                                  │
│     if devConfig.SkipUCRSSync { skip SyncClient.Start }  │
│     if !devConfig.CapabilityRegistry.Enabled { skip }    │
│     wrap Resolver with DevOverrideResolver               │
│                                                          │
│  3. ensureProviderInstalled():                           │
│     if devConfig.Has(providerID) { use local binary }   │
│     else { standard UCRS flow }                          │
└─────────────────────────────────────────────────────────┘
```

---

## Implementation Plan

### Phase 1: Build Infrastructure & Config Parsing

**Files to create:**

| File | Purpose |
|------|---------|
| `internal/devmode/config.go` | `DevConfig` struct, `build.yaml` parser, path resolution |
| `internal/devmode/config_test.go` | Unit tests for config parsing, path resolution, defaults merging |
| `internal/devmode/doc.go` | Package documentation (build-tagged `//go:build dev`) |

**`DevConfig` struct:**

```go
//go:build dev

package devmode

type DevConfig struct {
    Version   string                      `yaml:"version"`
    Defaults  DevDefaults                 `yaml:"defaults"`
    Overrides map[string]UMCOverride      `yaml:"overrides"`
    Agent     AgentDevSettings            `yaml:"agent"`
    
    // Internal: resolved base path for relative paths
    basePath  string
}

type DevDefaults struct {
    Env                map[string]string `yaml:"env"`
    Watch              bool              `yaml:"watch"`
    SkipSignatureCheck bool              `yaml:"skip_signature_check"`
}

type UMCOverride struct {
    Binary         string            `yaml:"binary"`
    Args           []string          `yaml:"args"`
    Env            map[string]string `yaml:"env"`
    Watch          *bool             `yaml:"watch"`          // nil = use default
    HealthEndpoint string            `yaml:"health_endpoint"`
    HealthTimeout  Duration          `yaml:"health_timeout"` // custom YAML duration
}

type AgentDevSettings struct {
    SkipMTLS             bool              `yaml:"skip_mtls"`
    SkipSpine            bool              `yaml:"skip_spine"`
    SkipUCRSSync         bool              `yaml:"skip_ucrs_sync"`
    CapabilityRegistry   CapRegDevSettings `yaml:"capability_registry"`
}

type CapRegDevSettings struct {
    Enabled *bool `yaml:"enabled"` // nil = default behavior
}

// LoadBuildConfig parses build.yaml and resolves all relative paths
// against the directory containing the config file.
func LoadBuildConfig(path string) (*DevConfig, error) { ... }

// ResolveOverride returns the override for a UMC name/provider ID, or nil.
func (c *DevConfig) ResolveOverride(name string) *UMCOverride { ... }

// ResolveBinary returns the absolute path to the binary, resolving
// relative paths against the build.yaml location.
func (c *DevConfig) ResolveBinary(name string) (string, error) { ... }

// EffectiveEnv merges defaults.env + override.env + base env.
func (c *DevConfig) EffectiveEnv(name string, base map[string]string) map[string]string { ... }

// ShouldWatch returns whether file-watching is enabled for a UMC.
func (c *DevConfig) ShouldWatch(name string) bool { ... }
```

**Stub for production builds:**

| File | Purpose |
|------|---------|
| `internal/devmode/stub.go` | `//go:build !dev` — nil-safe stub types with no-op methods |

```go
//go:build !dev

package devmode

// DevConfig is a no-op in production builds.
type DevConfig struct{}

func LoadBuildConfig(path string) (*DevConfig, error) {
    return nil, fmt.Errorf("dev mode not available in production build")
}

func (c *DevConfig) ResolveOverride(name string) *UMCOverride { return nil }
// ... all methods return zero values
```

This ensures any accidental reference to `devmode.DevConfig` in shared code compiles to a no-op in production.

---

### Phase 2: CLI Flag Integration

**Files to modify:**

| File | Change |
|------|--------|
| `internal/commands/local/start.go` | Add `--dev-mode` and `--build-config` flags (behind build tag) |
| `internal/commands/local/agent.go` | Add same flags to `ufctl agent start` |
| `internal/commands/local/flags_dev.go` | **New**: `//go:build dev` — registers dev flags on both commands |
| `internal/commands/local/flags_prod.go` | **New**: `//go:build !dev` — no-op, no flags registered |
| `internal/agent/launch.go` | Accept `DevConfig` in `LauncherConfig`, propagate to `WireAgent` |

**Flag registration pattern** (keeps the main command files clean):

```go
//go:build dev
// flags_dev.go

package local

func init() {
    StartCmd.Flags().Bool("dev-mode", false, "Enable development mode (load UMCs from local paths)")
    StartCmd.Flags().String("build-config", "./build.yaml", "Path to build.yaml for dev mode overrides")

    agentStartCmd.Flags().Bool("dev-mode", false, "Enable development mode")
    agentStartCmd.Flags().String("build-config", "./build.yaml", "Path to build.yaml")
}
```

**LauncherConfig change:**

```go
type LauncherConfig struct {
    Mode      LaunchMode
    Port      int
    DevConfig *devmode.DevConfig // nil in production builds
}
```

When `--dev-mode` is set, the command handler:
1. Parses `build.yaml` from the `--build-config` path
2. Validates all referenced binaries exist and are executable
3. Logs a clear banner: `⚠ DEV MODE — loading UMCs from build.yaml`
4. Injects the parsed `DevConfig` into `LauncherConfig`

---

### Phase 3: Wiring Integration

**Files to modify:**

| File | Change |
|------|--------|
| `internal/agent/wiring.go` | Accept `*devmode.DevConfig`, use it at 4 integration points |
| `internal/agent/wiring_dev.go` | **New**: `//go:build dev` — helper functions for dev-mode wiring |
| `internal/agent/wiring_prod.go` | **New**: `//go:build !dev` — stub helpers that always return false |

**Integration points in `WireAgent()`:**

#### Point 1: Deployment Engine Launch (~line 730)

Current code searches PATH for `deployment-engine-serve`. With dev mode:

```go
// wiring_dev.go
func devDeploymentEnginePath(dc *devmode.DevConfig) (string, bool) {
    if dc == nil { return "", false }
    override := dc.ResolveOverride("deployment-engine")
    if override == nil { return "", false }
    path, err := dc.ResolveBinary("deployment-engine")
    if err != nil { return "", false }
    return path, true
}
```

In `WireAgent`:
```go
var deEnginePath string
if path, ok := devDeploymentEnginePath(devConfig); ok {
    deEnginePath = path
    logger.Warn("DEV MODE: using local deployment engine", "path", path)
} else {
    deEnginePath = findDeploymentEngine() // existing search
}
```

#### Point 2: Capability Manager Config (~line 818)

When `devConfig.Agent.SkipUCRSSync` is true, disable the `SyncClient` entirely:

```go
if devConfig != nil && devConfig.Agent.SkipUCRSSync {
    logger.Warn("DEV MODE: skipping UCRS sync")
    // Don't start syncClient — registry stays empty, only dev overrides apply
}
```

When `devConfig.Agent.CapabilityRegistry.Enabled` is explicitly `false`, skip the entire capability manager initialization.

#### Point 3: Provider Resolution (~line 870, `ensureProviderInstalled`)

Wrap the `EnsureCapability` / `InstallProviderByID` calls with a dev-mode check:

```go
func ensureProviderInstalled(ctx context.Context, mgr *capability.Manager, 
    providerID string, label string, devConfig *devmode.DevConfig) {
    
    if override, ok := devResolveProvider(devConfig, providerID); ok {
        logger.Warn("DEV MODE: loading provider from local binary",
            "provider", providerID, "binary", override.Binary)
        // Use the supervisor directly to launch the local binary
        // instead of going through UCRS → download → install
        launchLocalProvider(ctx, override, providerID)
        return
    }
    
    // Standard UCRS flow
    mgr.InstallProviderByID(ctx, providerID)
}
```

#### Point 4: Agent-Level Settings

```go
if devConfig != nil && devConfig.Agent.SkipSpine {
    logger.Warn("DEV MODE: skipping Spine connection")
    // Skip spine client initialization, use a no-op publisher
}
```

---

### Phase 4: File Watcher for Hot-Restart

**Files to create:**

| File | Purpose |
|------|---------|
| `internal/devmode/watcher.go` | `//go:build dev` — binary file watcher with debounced restart |
| `internal/devmode/watcher_test.go` | Unit tests |

**Design:**

```go
// Watcher polls binary mtimes and triggers restarts on change.
type Watcher struct {
    entries  map[string]watchEntry // keyed by UMC name
    interval time.Duration         // default: 2s
    onRestart func(name string, binaryPath string) error
}

type watchEntry struct {
    binaryPath string
    lastMod    time.Time
    process    *os.Process // reference for graceful kill
}

func (w *Watcher) Start(ctx context.Context) {
    ticker := time.NewTicker(w.interval)
    for {
        select {
        case <-ctx.Done(): return
        case <-ticker.C:
            for name, entry := range w.entries {
                info, err := os.Stat(entry.binaryPath)
                if err != nil { continue }
                if info.ModTime().After(entry.lastMod) {
                    slog.Info("DEV MODE: binary changed, restarting",
                        "umc", name, "binary", entry.binaryPath)
                    w.onRestart(name, entry.binaryPath)
                    entry.lastMod = info.ModTime()
                    w.entries[name] = entry
                }
            }
        }
    }
}
```

The watcher **polls** rather than using `fsnotify` to avoid adding a dependency and to work reliably across macOS/Linux. A 2-second poll interval is fast enough for development workflows where you run `make build` and wait.

The `onRestart` callback integrates with the existing `ProcessSupervisor` — it calls `Stop(name)` then `Start(name)`, reusing the same health-check and restart logic.

---

### Phase 5: Makefile & Build Tags

**Files to modify:**

| File | Change |
|------|--------|
| `underleaf_client/Makefile` | Add `build-dev` and `bni-dev` targets |

```makefile
## build-dev: Build CLI and agent with dev mode enabled
build-dev: build-cli-dev build-agent-dev

## build-cli-dev: Build ufctl with dev mode
build-cli-dev:
	@echo "Building $(BINARY_CLI) [dev mode]..."
	go build -tags dev $(LDFLAGS) -o $(BINARY_CLI) ./cmd/ufctl

## build-agent-dev: Build underleaf_agent with dev mode
build-agent-dev:
	@echo "Building $(BINARY_AGENT) [dev mode]..."
	go build -tags dev $(LDFLAGS) -o $(BINARY_AGENT) ./cmd/agent

## bni-dev: Build & Install with dev mode
bni-dev: build-dev install
```

---

### Phase 6: Dev Workflow Convenience

**Files to create:**

| File | Purpose |
|------|---------|
| `build.yaml.example` | Documented template at the monorepo root |
| `underleaf_client/internal/devmode/validate.go` | Startup validation: check binaries exist, ports aren't conflicting |

**Validation on startup:**

When `--dev-mode` is active, before launching anything:

1. **Binary existence** — for each override, verify the file exists and is executable. Fail fast with a clear message: `DEV MODE ERROR: deployment-engine binary not found at ../umcs/deployment_engine/bin/serve (resolved to /abs/path). Run 'make build' in that directory.`

2. **Port conflicts** — if multiple overrides specify health endpoints on the same port, warn.

3. **Version mismatch** — if `build.yaml` version is unsupported, error with upgrade instructions.

**`build.yaml.example`:**

```yaml
# build.yaml.example — Copy to build.yaml and customize for your dev setup
#
# Usage:
#   make build-dev && ufctl start --dev-mode --build-config ./build.yaml
#
# This file is .gitignored. Each developer maintains their own.
version: "1"

defaults:
  env:
    LOG_LEVEL: debug
    KERNEL_SOCKET: /tmp/ua_kernel.sock
  watch: true
  skip_signature_check: true

overrides:
  # Uncomment and set paths to your local builds:
  
  # deployment-engine:
  #   binary: ./umcs/deployment_engine/bin/serve
  #   health_endpoint: http://localhost:10081/health

  # cron-engine:
  #   binary: ./umcs/cron_engine/bin/serve
  #   health_endpoint: http://localhost:8081/health

  # mma:
  #   binary: ./umcs/mycelium_mesh_agent/bin/serve
  #   health_endpoint: http://localhost:8082/health

agent:
  skip_ucrs_sync: true
  capability_registry:
    enabled: false
```

---

## File Inventory

### New Files

| File | Build Tag | Lines (est.) |
|------|-----------|-------------|
| `internal/devmode/config.go` | `dev` | ~180 |
| `internal/devmode/stub.go` | `!dev` | ~40 |
| `internal/devmode/watcher.go` | `dev` | ~120 |
| `internal/devmode/validate.go` | `dev` | ~80 |
| `internal/devmode/config_test.go` | `dev` | ~150 |
| `internal/devmode/watcher_test.go` | `dev` | ~100 |
| `internal/commands/local/flags_dev.go` | `dev` | ~20 |
| `internal/commands/local/flags_prod.go` | `!dev` | ~5 |
| `internal/agent/wiring_dev.go` | `dev` | ~120 |
| `internal/agent/wiring_prod.go` | `!dev` | ~30 |
| `build.yaml.example` | — | ~40 |

### Modified Files

| File | Change Summary |
|------|---------------|
| `internal/agent/launch.go` | Add `DevConfig` to `LauncherConfig`; pass through to `WireAgent` |
| `internal/agent/wiring.go` | Accept `*devmode.DevConfig` parameter; call dev helper functions at 4 integration points |
| `internal/commands/local/start.go` | Read `--dev-mode` / `--build-config` flags; parse config; inject into launcher |
| `internal/commands/local/agent.go` | Same flag handling for `ufctl agent start` |
| `Makefile` | Add `build-dev`, `build-cli-dev`, `build-agent-dev`, `bni-dev` targets |
| `.gitignore` | Add `build.yaml` |

---

## Improvements Over the Original Idea

| Original | Improved |
|----------|----------|
| Single `build.yaml` with deployment-engine focus | Per-UMC granular overrides — any provider can be overridden, including custom third-party ones |
| Implicit dev mode awareness | Explicit build tag gating — `//go:build dev` ensures zero dev code in production |
| Manual restart on rebuild | File watcher with auto-restart — poll-based, no extra dependencies |
| Binary only | Merged env/args support — override environment variables and CLI args per-UMC for debug scenarios |
| Agent must bootstrap deployment engine locally | `agent:` section allows skipping Spine, mTLS, or UCRS sync — supports fully offline development |
| No validation | Startup validation — fail-fast with actionable error messages before any UMC launch |
| No production stub | `stub.go` with `//go:build !dev` — `DevConfig` compiles to no-ops in production, no `if devMode != nil` scattered everywhere |
| No example/documentation | `build.yaml.example` checked into repo, `.gitignored` actual `build.yaml` per developer |

---

## Execution Order

1. **Phase 1** — config parsing + tests (can be developed and tested in isolation)
2. **Phase 2** — CLI flags (depends on Phase 1 for `DevConfig` type)
3. **Phase 3** — wiring integration (depends on Phase 1 + 2; this is the core)
4. **Phase 5** — Makefile targets (can be done in parallel with Phase 3)
5. **Phase 4** — file watcher (nice-to-have, can ship after the core works)
6. **Phase 6** — validation + example file (polish, do last)

Estimated effort: **2–3 days** for Phases 1–3+5 (functional dev mode), **+1 day** for Phase 4+6 (watcher + polish).

---

## Usage

```bash
# One-time: copy and customize build.yaml
cp build.yaml.example build.yaml
# Edit build.yaml to point to your local UMC builds

# Build with dev mode enabled
cd underleaf_client && make build-dev

# Start with dev mode
ufctl start --dev-mode --build-config ../build.yaml -p 2023

# Or for agent-only restart:
ufctl agent start --dev --dev-mode --build-config ../build.yaml -p 2023

# In another terminal, rebuild a UMC and watch it auto-restart:
cd umcs/deployment_engine && make build
# Agent detects mtime change → restarts deployment engine automatically
```
