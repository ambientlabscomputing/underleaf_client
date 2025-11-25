# `ufctl` The Underleaf CLI Tool

## Directory Structure

```bash
underleaf/
├─ cmd/
│  ├─ ufctl/                   # CLI binary (dev/operator tool)
│  │  └─ main.go
│  └─ underleaf-agent/         # Node agent binary (runs on edge nodes)
│     └─ main.go
│
├─ internal/
│  ├─ cli/                     # CLI root wiring + global helpers
│  │  ├─ root.go               # root cmd: `ufctl`
│  │  ├─ flags.go              # shared flag helpers
│  │  └─ completion.go         # shell completion
│  │
│  ├─ commands/                     # CLI subcommand tree (resource-oriented)
│  │  ├─ local/                # `ufctl local ...` (local cli commands)
│  │  │  ├─ local.go           # parent `local` cmd
│  │  │  ├─ auth.go            # authenticate with the control plan
│  │  ├─ node/                 # `ufctl node ...` (remote nodes)
│  │  │  ├─ node.go            # parent `node` cmd
│  │  │  ├─ list.go            # `node list`
│  │  │  ├─ describe.go        # `node describe <node>`
│  │  │  ├─ exec.go            # `node exec <selector> -- ...`
│  │  │  ├─ logs.go            # `node logs <selector>`
│  │  │  └─ status.go          # `node status <selector>`
│  │
│  ├─ config_manager/               # SHARED: config manager core + RPC endpoints
│  │  ├─ manager.go            # subscribes to control plane, reconciles snapshots
│  │  ├─ store.go              # local snapshot persistence (file/bolt/sqlite)
│  │  ├─ types.go              # ConfigSnapshot, VersionInfo, etc.
│  │  ├─ server.go             # local API server (Unix socket / HTTP)
│  │  └─ client.go             # local client used by CLI (`local config ...`)
│  │
│  ├─ agent/                   # Node agent entry + lifecycle
│  │  ├─ server.go             # Start all agent services (config_manager, exec, etc.)
│  │  ├─ runtime.go            # signal handling, graceful shutdown
│  │  └─ wiring.go             # wire config_manager.Manager, exec.Runner, bus clients
│  │
│  ├─ exec/                    # SHARED: command execution on node
│  │  ├─ runner.go             # runs commands, handles timeouts, env, etc.
│  │  ├─ types.go              # CommandRequest, Result, ExitCode, etc.
│  │  ├─ server.go             # agent-side exec API (used by CLI `local exec`)
│  │  └─ client.go             # local exec client (CLI & tests)
│  │
│  ├─ node/                    # Node "domain" model + selectors
│  │  ├─ selector.go           # parse self/labels/ids → NodeSelector
│  │  ├─ types.go              # Node, NodeStatus, Labels, etc.
│  │  └─ service.go            # high-level node ops via control plane API
│  │
│  ├─ controlplane/            # SHARED: backend/control-plane HTTP clients
│  │  ├─ client.go             # base client (auth, transport)
│  │  ├─ node_client.go
│  │  ├─ request_client.go
│  │  └─ cluster_client.go
│  │
│  ├─ config/                  # SHARED: CLI/agent configuration (on-disk)
│  │  ├─ paths.go              # resolve config dirs (/etc, ~/.underleaf, etc.)
│  │  ├─ types.go              # Context, ClusterConfig, AgentConfig, etc.
│  │  └─ store.go              # load/save config YAML/JSON
│  │
│  ├─ bus/                     # SHARED: event bus / messaging client abstraction
│  │  ├─ client.go             # publish/subscribe abstraction
│  │  └─ topics.go             # topic/subject naming helpers
│  │
│  ├─ ui/                      # SHARED: output & UX helpers for CLI
│  │  ├─ printer.go            # table/json/yaml output
│  │  ├─ table.go
│  │  └─ errors.go             # nice error formatting
│  │
│  └─ logging/                 # SHARED: slog logger wrapper
│     └─ logger.go
│
├─ pkg/                        # optional: public packages if needed elsewhere
│  └─ version/
│     └─ version.go            # version info for both binaries (ldflags)
│
├─ go.mod
└─ README.md
```
