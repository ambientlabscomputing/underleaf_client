# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Initial release of Underleaf CLI (`ufctl`) and Agent (`underleaf_agent`)
- Command execution across remote servers via event bus architecture
- Real-time job status monitoring with event timeline
- Support for server selectors (role-based, tag-based, ID-based)
- Local agent for receiving and executing commands
- Configuration management for API endpoints and authentication
- Rich terminal UI with color-coded output
- Docker-compose-like execution behavior (wait by default, `--detach` for background)
- Per-server command results with exit codes and stdout/stderr
- Comprehensive documentation (README, CONTRIBUTING, LICENSE)
- Example configuration file
- Unit tests for core packages
- Makefile for common development tasks

### Features
- **CLI Commands**:
  - `ufctl servers list` - List all servers
  - `ufctl servers exec` - Execute commands on servers
  - `ufctl jobs list` - List jobs
  - `ufctl jobs status` - View detailed job status
  - `ufctl run` - Run agent locally
  - `ufctl init` - Initialize configuration

- **Agent**:
  - Subscribe to event bus for command execution requests
  - Execute commands locally with timeout and environment support
  - Report results back to control plane
  - HTTP server for health checks and configuration updates

### Architecture
- Hybrid HTTP + WebSocket communication
- Event-driven command dispatch and execution
- Long-polling event bus with automatic reconnection
- Structured logging with configurable levels

## [0.1.0] - TBD

Initial release.

[Unreleased]: https://github.com/ambientlabscomputing/underleaf_client/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/ambientlabscomputing/underleaf_client/releases/tag/v0.1.0
