//go:build dev

package devmode

// Package devmode provides development-mode functionality for loading UMCs from local paths.
// This package is only compiled into development builds (via //go:build dev).
//
// Dev mode allows developers to:
// - Override UMC binary locations with local builds
// - Skip UCRS sync and registry verification
// - Enable automatic process restart on binary changes
// - Configure per-UMC environment variables and health endpoints
//
// Configuration is provided via a build.yaml file with the following structure:
//
//version: "1"
//defaults:
//  env:
//    LOG_LEVEL: debug
//  watch: true
//overrides:
//  deployment-engine:
//    binary: ../umcs/deployment_engine/bin/serve
//    health_endpoint: http://localhost:10081/health
//agent:
//  skip_ucrs_sync: true
//  capability_registry:
//    enabled: false
