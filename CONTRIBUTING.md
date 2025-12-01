# Contributing to Underleaf Client

Thank you for your interest in contributing to Underleaf Client! This document provides guidelines and instructions for contributing.

## Code of Conduct

- Be respectful and inclusive
- Focus on constructive feedback
- Help others learn and grow

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/YOUR_USERNAME/underleaf_client.git`
3. Add upstream remote: `git remote add upstream https://github.com/ambientlabscomputing/underleaf_client.git`
4. Create a feature branch: `git checkout -b feature/my-feature`

## Development Setup

### Prerequisites

- Go 1.24 or higher
- Access to a running Underleaf control plane (for integration tests)
- Event bus server (for event-driven features)

### Building

```bash
# Build CLI
go build -o ufctl ./cmd/ufctl

# Build agent
go build -o underleaf_agent ./cmd/underleaf_agent

# Run tests
go test ./...

# Run tests with coverage
go test -coverprofile=coverage.out ./...
```

### Code Style

- Follow standard Go conventions
- Use `gofmt` to format code
- Use `golint` and `go vet` to check code quality
- Add comments for exported functions and types
- Keep functions focused and small

### Testing

- Add unit tests for new functionality
- Ensure all tests pass before submitting PR
- Aim for >80% code coverage for new code
- Test edge cases and error conditions

### Commit Messages

Use clear, descriptive commit messages:

```
Short summary (50 chars or less)

More detailed explanation if necessary. Wrap at 72 characters.
Explain what and why, not how.

- Bullet points are okay
- Use present tense: "Add feature" not "Added feature"
```

## Pull Request Process

1. **Update Documentation**: Update README.md if you change functionality
2. **Add Tests**: Include tests for new features
3. **Run Tests**: Ensure all tests pass
4. **Update CHANGELOG**: Add entry describing your changes
5. **Submit PR**: Create pull request with clear description

### PR Description Template

```markdown
## Description
Brief description of what this PR does

## Type of Change
- [ ] Bug fix
- [ ] New feature
- [ ] Breaking change
- [ ] Documentation update

## Testing
Describe how you tested your changes

## Checklist
- [ ] Code follows project style guidelines
- [ ] Tests added/updated and passing
- [ ] Documentation updated
- [ ] No breaking changes (or documented if unavoidable)
```

## Project Structure

```
underleaf_client/
├─ cmd/              # Binary entry points
├─ internal/         # Internal packages (not importable)
│  ├─ agent/         # Agent server logic
│  ├─ cli/           # CLI root and global setup
│  ├─ commands/      # CLI subcommands
│  ├─ config/        # Configuration management
│  ├─ controlplane/  # API clients
│  ├─ exec/          # Command execution
│  └─ ui/            # Terminal UI components
├─ pkg/              # Public packages (importable)
└─ guides/           # Development documentation
```

## Areas for Contribution

### High Priority

- [ ] Unit tests for exec package
- [ ] Unit tests for bus client
- [ ] Integration tests for agent
- [ ] Documentation improvements
- [ ] Example configurations

### Feature Requests

- [ ] Shell auto-completion improvements
- [ ] Better error messages
- [ ] Metrics and monitoring support
- [ ] Command history
- [ ] Interactive mode enhancements

### Bug Reports

When reporting bugs, please include:

- Go version (`go version`)
- OS and architecture
- Steps to reproduce
- Expected vs actual behavior
- Relevant logs or error messages

Use the GitHub issue template for bug reports.

## Code Review Process

1. Maintainers will review PRs within 1-2 business days
2. Address feedback and update PR as needed
3. Once approved, maintainers will merge your PR

## Questions?

- Open an issue for general questions
- Use GitHub Discussions for broader topics
- Email: dev@ambientlabs.io for security concerns

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
