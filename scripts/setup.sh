#!/usr/bin/env bash

# Underleaf CLI - Development Setup Script
# This script helps new developers get started quickly

set -e

echo "🚀 Underleaf CLI Development Setup"
echo "=================================="
echo ""

# Check Go version
echo "📦 Checking Go installation..."
if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed. Please install Go 1.24.3 or later."
    echo "   Visit: https://golang.org/dl/"
    exit 1
fi

GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
echo "✅ Found Go $GO_VERSION"
echo ""

# Download dependencies
echo "📥 Downloading dependencies..."
go mod download
go mod tidy
echo "✅ Dependencies downloaded"
echo ""

# Create example config if it doesn't exist
if [ ! -f "config.yaml" ]; then
    echo "📝 Creating config.yaml from template..."
    cp config.example.yaml config.yaml
    echo "✅ Created config.yaml - please edit with your settings"
else
    echo "ℹ️  config.yaml already exists"
fi
echo ""

# Build binaries
echo "🔨 Building binaries..."
make build
echo "✅ Binaries built successfully"
echo ""

# Run tests
echo "🧪 Running tests..."
make test
echo "✅ Tests passed"
echo ""

# Summary
echo "✨ Setup complete!"
echo ""
echo "Next steps:"
echo "  1. Edit config.yaml with your API endpoint and credentials"
echo "  2. Run the CLI: ./ufctl --help"
echo "  3. Run the agent: ./underleaf_agent"
echo "  4. Read the docs: cat README.md"
echo ""
echo "Useful commands:"
echo "  make build         - Build both binaries"
echo "  make test          - Run tests"
echo "  make test-coverage - Generate coverage report"
echo "  make check         - Run fmt, vet, and test"
echo "  make clean         - Remove build artifacts"
echo "  make help          - Show all available targets"
echo ""
echo "Happy coding! 🎉"
