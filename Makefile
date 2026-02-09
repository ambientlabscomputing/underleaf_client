.PHONY: build test clean install fmt vet lint run-cli run-agent help e2e-test

# Variables
BINARY_CLI=ufctl
BINARY_AGENT=underleaf_agent
VERSION?=dev
API_BASE_URL?=http://localhost:8080/api/v1/servers
EVENT_BUS_ENDPOINT?=ws://localhost:9000/ws
UCRS_BASE_URL?=http://localhost:8083/api/v1/registry
LDFLAGS=-ldflags "-X github.com/ambientlabscomputing/underleaf_client/pkg/version.Version=$(VERSION) \
	-X github.com/ambientlabscomputing/underleaf_client/pkg/defaults.APIBaseURL=$(API_BASE_URL) \
	-X github.com/ambientlabscomputing/underleaf_client/pkg/defaults.EventBusEndpoint=$(EVENT_BUS_ENDPOINT) \
	-X github.com/ambientlabscomputing/underleaf_client/pkg/defaults.UCRSBaseURL=$(UCRS_BASE_URL)"

## help: Display this help message
help:
	@echo "Underleaf Client - Makefile targets:"
	@echo ""
	@grep -E '^##' Makefile | sed 's/## /  /'
	@echo ""

## bni: Build & Install
.PHONY: bni
bni: build install

## run: Run Underleaf Agent
run:
	ufctl start -p 12012

## build: Build both CLI and agent binaries
build: build-cli build-agent

## build-cli: Build the CLI binary (ufctl)
build-cli:
	@echo "Building $(BINARY_CLI)..."
	@go build $(LDFLAGS) -o $(BINARY_CLI) ./cmd/ufctl
	@echo "✓ Built $(BINARY_CLI)"

## build-agent: Build the agent binary
build-agent:
	@echo "Building $(BINARY_AGENT)..."
	@go build $(LDFLAGS) -o $(BINARY_AGENT) ./cmd/underleaf_agent
	@echo "✓ Built $(BINARY_AGENT)"

## install: Install binaries to /usr/local/bin/
install:
	@echo "Installing binaries to /usr/local/bin/..."
	@cp $(BINARY_CLI) /usr/local/bin/$(BINARY_CLI)
	@cp $(BINARY_AGENT) /usr/local/bin/$(BINARY_AGENT)
	@echo "✓ Installed to /usr/local/bin/"

## install-gopath: Install binaries to $GOPATH/bin
install-gopath:
	@echo "Installing binaries..."
	@go install $(LDFLAGS) ./cmd/ufctl
	@go install $(LDFLAGS) ./cmd/underleaf_agent

## proto: Generate Go code from proto files
.PHONY: proto
proto:
	@echo "Generating Go code from proto files..."
	@protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		proto/ua_mma/v1/event_stream.proto
	@echo "✓ Proto code generated"
	@echo "✓ Installed to $(GOPATH)/bin"

## test: Run all tests
test:
	@echo "Running tests..."
	@go test -v ./...

## test-coverage: Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	@go test -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "✓ Coverage report generated: coverage.html"

## e2e-test: Run end-to-end tests with multiple agent processes
e2e-test: build
	@echo "Running E2E tests..."
	@./scripts/e2e-test.sh

## vet: Run go vet
vet:
	@echo "Running go vet..."
	@go vet ./...
	@echo "✓ Vet passed"

## lint: Run golangci-lint (requires golangci-lint installed)
lint:
	@echo "Running golangci-lint..."
	@golangci-lint run ./...
	@echo "✓ Lint passed"

## clean: Remove build artifacts
clean:
	@echo "Cleaning up..."
	@rm -f $(BINARY_CLI) $(BINARY_AGENT)
	@rm -f coverage.out coverage.html
	@rm -rf dist/
	@echo "✓ Cleaned"

## run-cli: Run the CLI with arguments (usage: make run-cli ARGS="servers list")
run-cli:
	@go run ./cmd/ufctl $(ARGS)

## run-agent: Run the agent locally
run-agent:
	@go run ./cmd/underleaf_agent

## deps: Download dependencies
deps:
	@echo "Downloading dependencies..."
	@go mod download
	@go mod tidy
	@echo "✓ Dependencies updated"

## proto: Generate protobuf code
proto:
	@echo "Generating protobuf code..."
	@protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		internal/raft/proto/raft.proto
	@echo "✓ Protobuf code generated"

## check: Run fmt, vet, and test
check: fmt vet test
	@echo "✓ All checks passed"

## ci: Run CI pipeline locally (matches GitHub Actions workflow)
ci: ci-test ci-lint
	@echo "✓ CI pipeline completed successfully"

## ci-test: Run test job (matches ci.yml test job)
ci-test:
	@echo "Running CI test pipeline..."
	@echo "→ Downloading dependencies..."
	@go mod download
	@echo "→ Verifying dependencies..."
	@go mod verify
	@echo "→ Running go vet..."
	@go vet ./...
	@echo "→ Running tests with race detector..."
	@go test -v -race -coverprofile=coverage.out ./...
	@echo "→ Building ufctl..."
	@go build -v ./cmd/ufctl
	@echo "→ Building underleaf_agent..."
	@go build -v ./cmd/underleaf_agent
	@echo "✓ Test pipeline completed"

## ci-lint: Run lint job (matches ci.yml lint job)
ci-lint:
	@echo "Running CI lint pipeline..."
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "Error: golangci-lint is not installed"; \
		echo "Install it with: brew install golangci-lint (macOS) or go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		exit 1; \
	fi
	@echo "→ Running golangci-lint with 5m timeout..."
	@golangci-lint run --timeout=5m
	@echo "✓ Lint pipeline completed"

## release: Build release binaries for multiple platforms
release:
	@echo "Building release binaries..."
	@mkdir -p dist
	@GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY_CLI)-darwin-amd64 ./cmd/ufctl
	@GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o dist/$(BINARY_CLI)-darwin-arm64 ./cmd/ufctl
	@GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY_CLI)-linux-amd64 ./cmd/ufctl
	@GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/$(BINARY_CLI)-linux-arm64 ./cmd/ufctl
	@GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY_AGENT)-darwin-amd64 ./cmd/underleaf_agent
	@GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o dist/$(BINARY_AGENT)-darwin-arm64 ./cmd/underleaf_agent
	@GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY_AGENT)-linux-amd64 ./cmd/underleaf_agent
	@GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/$(BINARY_AGENT)-linux-arm64 ./cmd/underleaf_agent
	@echo "✓ Release binaries built in dist/"
