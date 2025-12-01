# Underleaf Client

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**Underleaf Client** provides both a CLI tool (`ufctl`) and an agent binary (`underleaf_agent`) for managing distributed edge servers through the Underleaf control plane.

## Features

- 🚀 **Remote Command Execution** - Execute commands across multiple servers with real-time status updates
- 📊 **Job Management** - Track and monitor command execution jobs with detailed per-server results
- 🔄 **Event-Driven Architecture** - Real-time event bus integration for instant command delivery
- 🛠️ **Local Agent Mode** - Run agent locally for development and testing
- 📝 **Rich CLI Output** - Beautiful terminal UI with tables, spinners, and colored output
- 🔐 **Secure Authentication** - JWT-based authentication with Auth0 integration
- ⚙️ **Configuration Management** - Centralized config distribution via event bus

## Installation

### From Source

```bash
# Clone the repository
git clone https://github.com/ambientlabscomputing/underleaf_client.git
cd underleaf_client

# Build both binaries
go build -o ufctl ./cmd/ufctl
go build -o underleaf_agent ./cmd/underleaf_agent

# Optional: Install to $GOPATH/bin
go install ./cmd/ufctl
go install ./cmd/underleaf_agent
```

### Binary Releases

Download pre-built binaries from the [Releases](https://github.com/ambientlabscomputing/underleaf_client/releases) page.

## Quick Start

### 1. Configure the CLI

Create a configuration file at `~/.underleaf/config.yaml` or use the provided example:

```yaml
version: 0.0.0
payload:
  api:
    base_url: http://localhost:8080/api/v1
  auth:
    token: your-jwt-token-here
  event_bus:
    endpoint: ws://localhost:9000
    commit_interval: 5s
  local:
    server_id: your-server-id
```

### 2. Authenticate

```bash
# Authenticate with the control plane
ufctl local auth

# Register as a server (for running agent)
ufctl local register
```

### 3. Run Commands

```bash
# Execute a command on a specific server (waits for completion)
ufctl servers exec server-123 -- echo "Hello World"

# Execute on all servers
ufctl servers exec all -- uptime

# Fire-and-forget mode
ufctl servers exec server-123 -d -- long-running-task

# With environment variables and timeout
ufctl servers exec server-123 -e FOO=bar -t 60 -- ./deploy.sh
```

### 4. Monitor Jobs

```bash
# List all jobs
ufctl jobs list

# Get job status
ufctl jobs status <job-id>

# Watch job in real-time
ufctl jobs status <job-id> --watch

# Show full command output
ufctl jobs status <job-id> --output
```

### 5. Run Local Agent

```bash
# Start agent in foreground
ufctl local agent start

# Start agent in detached mode
ufctl local agent start -d

# Stop agent
ufctl local agent stop

# Check agent status
ufctl local agent status
```

## Architecture

### Components

**ufctl** - CLI tool for developers and operators
- Command execution and job management
- Server listing and status checks
- Local agent control
- Configuration management

**underleaf_agent** - Server-side agent binary
- Receives commands via event bus
- Executes commands with timeout/env controls
- Reports results back to control plane
- Manages local configuration snapshots

### Communication Flow

```
┌─────────┐                  ┌──────────────┐                 ┌─────────┐
│  ufctl  │ ──── HTTP ────> │Control Plane│                 │  Agent  │
└─────────┘                  └──────┬───────┘                 └────┬────┘
                                    │                              │
                                    └──── Event Bus (WebSocket) ──┘
                                          │                     │
                                    Publish Command       Subscribe
                                    Listen Results        Execute & Report
```

## CLI Commands

### Servers

```bash
ufctl servers list                         # List all servers
ufctl servers describe <server-id>         # Describe a specific server
ufctl servers exec <selector> -- <cmd>     # Execute command
ufctl servers status <selector>            # Get server status
ufctl servers logs <selector>              # View server logs
```

### Jobs

```bash
ufctl jobs list                            # List all jobs
ufctl jobs status <job-id>                 # Get job status
ufctl jobs status <job-id> --watch         # Watch job until completion
ufctl jobs status <job-id> --output        # Show full stdout/stderr
```

### Local

```bash
ufctl local auth                           # Authenticate with control plane
ufctl local register                       # Register this machine as a server
ufctl local config get                     # Show current configuration
ufctl local config set <key> <value>       # Set configuration value
ufctl local agent start [-d]               # Start local agent
ufctl local agent stop                     # Stop local agent
ufctl local agent status                   # Check agent status
```

## Configuration

### CLI Configuration

Location: `~/.underleaf/config.yaml`

```yaml
version: 0.0.0
payload:
  api:
    base_url: http://localhost:8080/api/v1  # Control plane API URL
  auth:
    token: eyJhbGc...                        # JWT authentication token
  event_bus:
    endpoint: ws://localhost:9000            # Event bus WebSocket endpoint
    commit_interval: 5s                      # Offset commit interval
  local:
    server_id: 62db5115-b200-469f-bc8b      # This server's ID (for agent mode)
```

### Agent Configuration

The agent uses the same configuration file format. When running as an agent:
- `local.server_id` identifies this server
- `event_bus.endpoint` specifies where to connect
- Configuration updates are received automatically via the event bus

### Environment Variables

```bash
UNDERLEAF_API_URL=http://localhost:8080/api/v1
UNDERLEAF_TOKEN=your-jwt-token
UNDERLEAF_EVENT_BUS=ws://localhost:9000
UNDERLEAF_SERVER_ID=your-server-id
```

## Development

### Project Structure

```bash
underleaf_client/
├─ cmd/
│  ├─ ufctl/              # CLI binary
│  └─ underleaf_agent/    # Agent binary
├─ internal/
│  ├─ agent/              # Agent server and lifecycle
│  ├─ bus/                # Event bus client wrapper
│  ├─ cli/                # CLI root command and flags
│  ├─ commands/           # CLI subcommands
│  │  ├─ jobs/            # Job management commands
│  │  ├─ local/           # Local commands (auth, agent, config)
│  │  └─ servers/         # Server management commands
│  ├─ config/             # Configuration loading and storage
│  ├─ config_manager/     # Config snapshot management
│  ├─ controlplane/       # Control plane API clients
│  ├─ exec/               # Command execution engine
│  ├─ logging/            # Structured logging
│  ├─ server/             # Server domain models
│  └─ ui/                 # Terminal UI components
├─ pkg/
│  └─ version/            # Version information
└─ guides/                # Development guides
```

### Running Tests

```bash
# Run all tests
go test ./...

# Run with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run specific package
go test ./internal/exec/...
```

### Building

```bash
# Build CLI
go build -o ufctl ./cmd/ufctl

# Build agent
go build -o underleaf_agent ./cmd/underleaf_agent

# Build with version info
go build -ldflags "-X github.com/ambientlabscomputing/underleaf_client/pkg/version.Version=1.0.0" ./cmd/ufctl
```

## Usage Examples

### Execute Commands

```bash
# Simple command
ufctl servers exec server-abc123 -- whoami

# Multi-server targeting
ufctl servers exec all -- df -h

# With environment variables
ufctl servers exec production -- printenv | grep -e FOO -e BAR

# Long-running task (detached)
ufctl servers exec server-123 -d -- ./backup.sh
```

### Monitor Job Progress

```bash
# Execute and watch automatically (default behavior)
ufctl servers exec server-123 -- ./deploy.sh

# Check status later
JOB_ID=abc-123-xyz
ufctl jobs status $JOB_ID

# Watch until completion
ufctl jobs status $JOB_ID --watch

# See full output
ufctl jobs status $JOB_ID --output
```

### Agent Operations

```bash
# Start agent in foreground (for development)
ufctl local agent start

# Start agent in background
ufctl local agent start -d

# Check if agent is running
ufctl local agent status

# Stop background agent
ufctl local agent stop

# View agent logs
tail -f /var/log/underleaf_agent/agent.log
```

## Event Bus Integration

The system uses an event bus for real-time communication:

### Topics

- `commands.run.server.request` - Command execution requests
- `server-data-update` - Server status and data updates
- `config.snapshot.update` - Configuration updates

### Message Flow

1. CLI dispatches command via HTTP to control plane
2. Control plane publishes to event bus
3. Agent(s) receive and execute command
4. Agent reports result via HTTP POST
5. Control plane updates job status
6. CLI polls job status for completion

## Troubleshooting

### Agent Not Receiving Commands

```bash
# Check agent is running
ufctl local agent status

# Check event bus connection
# Look for "event bus client started" in logs

# Verify server_id matches
ufctl local config get local.server_id
```

### Authentication Issues

```bash
# Re-authenticate
ufctl local auth

# Verify token
ufctl local config get auth.token

# Check token expiry (JWT tokens expire after 24 hours)
```

### Command Timeouts

```bash
# Increase timeout (default is server-defined)
ufctl servers exec server-123 -t 300 -- long-task.sh

# Check job status for timeout errors
ufctl jobs status <job-id> --output
```

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

### Code Style

- Follow standard Go conventions
- Use `gofmt` for formatting
- Add tests for new functionality
- Update documentation for API changes

### Submitting Changes

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Support

- Documentation: [docs.ambientlabs.io](https://docs.ambientlabs.io)
- Issues: [GitHub Issues](https://github.com/ambientlabscomputing/underleaf_client/issues)
- Email: support@ambientlabs.io

## Acknowledgments

Built with:
- [Cobra](https://github.com/spf13/cobra) - CLI framework
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - Terminal UI
- [Lipgloss](https://github.com/charmbracelet/lipgloss) - Style definitions
- [Viper](https://github.com/spf13/viper) - Configuration management
