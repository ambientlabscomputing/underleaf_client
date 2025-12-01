# Developer Guides

This directory contains detailed technical guides for developers working on Underleaf CLI.

## Available Guides

### [Bubble Tea Implementation](./BUBBLE_TEA_IMPLEMENTATION.md)
Detailed documentation on how Bubble Tea (terminal UI framework) is used throughout the project for interactive command-line experiences.

### [UI Quick Reference](./UI_QUICK_REFERENCE.md)
Quick reference guide for UI components, styling, and terminal output formatting using Lipgloss and Bubble Tea.

### [White Least Command Guide](./WHITE_LEAST_COMMAND_GUIDE.md)
Implementation guide for specific command patterns and best practices.

## Getting Started

If you're new to developing with Underleaf CLI:

1. Start with the main [README](../README.md) for project overview
2. Review [CONTRIBUTING.md](../CONTRIBUTING.md) for development setup and guidelines
3. Check out these guides for implementation details on specific features
4. Look at the code in `internal/` to see how things are structured

## Additional Resources

- **Go Version**: 1.24.3
- **CLI Framework**: [Cobra](https://github.com/spf13/cobra)
- **Terminal UI**: [Bubble Tea](https://github.com/charmbracelet/bubbletea)
- **Styling**: [Lipgloss](https://github.com/charmbracelet/lipgloss)
- **Config**: [Viper](https://github.com/spf13/viper)
- **Event Bus**: [event_bus_client v1.0.3](https://github.com/ambientlabscomputing/event_bus_client)

## Architecture Overview

```
┌─────────────┐         ┌──────────────────┐         ┌──────────────┐
│   ufctl     │  HTTP   │  Control Plane   │  Event  │    Agent     │
│   (CLI)     ├────────→│    (Backend)     │  Bus    │  (Worker)    │
└─────────────┘         └──────────────────┘         └──────────────┘
      │                          │                          │
      │ WebSocket               │ WebSocket               │
      └─────────────────────────┼──────────────────────────┘
                                 │
                         Event Bus (Long-Polling)
```

The CLI communicates with the Control Plane via HTTP for command dispatch and WebSocket for real-time updates. Agents subscribe to the event bus to receive commands and report results.

## Contributing

When adding new guides:

1. Use clear, concise Markdown
2. Include code examples where helpful
3. Link to relevant source files
4. Keep guides focused on a single topic
5. Update this README with new guide descriptions

## Questions?

- Check the [main README](../README.md) for general information
- Review [CONTRIBUTING.md](../CONTRIBUTING.md) for development practices
- Open an issue for bugs or feature requests
