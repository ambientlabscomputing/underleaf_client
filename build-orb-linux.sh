#!/bin/bash
# Build Linux arm64 binaries with host.orb.internal endpoints baked in
set -e
cd "$(dirname "$0")"

VERSION=dev
API="http://host.orb.internal:8080/api/v1/servers"
SPINE="host.orb.internal:9097"
UCRS="http://host.orb.internal:8083/api/v1/registry"
LDFLAGS="-X github.com/ambientlabscomputing/underleaf_client/pkg/version.Version=${VERSION} -X github.com/ambientlabscomputing/underleaf_client/pkg/defaults.APIBaseURL=${API} -X github.com/ambientlabscomputing/underleaf_client/pkg/defaults.SpineEndpoint=${SPINE} -X github.com/ambientlabscomputing/underleaf_client/pkg/defaults.UCRSBaseURL=${UCRS}"

mkdir -p bin

echo "Building ufctl (linux/arm64, orb endpoints)..."
GOOS=linux GOARCH=arm64 go build -ldflags "$LDFLAGS" -o bin/ufctl-linux-arm64 ./cmd/ufctl

echo "Building underleaf_agent (linux/arm64, orb endpoints)..."
GOOS=linux GOARCH=arm64 go build -ldflags "$LDFLAGS" -o bin/underleaf_agent-linux-arm64 ./cmd/underleaf_agent

echo "✓ Done"
file bin/ufctl-linux-arm64 bin/underleaf_agent-linux-arm64
