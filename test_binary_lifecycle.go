// test_binary_lifecycle.go - Test binary provider lifecycle
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	ucrstypes "github.com/ambientlabscomputing/ucrs/sdk/types"
	"github.com/ambientlabscomputing/underleaf_client/internal/capability"
	"github.com/ambientlabscomputing/underleaf_client/internal/capability/store"
)

func main() {
	// Setup logging
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)
	slog.SetDefault(logger)

	ctx := context.Background()

	// Setup test directories
	homeDir, _ := os.UserHomeDir()
	providerDir := filepath.Join(homeDir, ".underleaf", "test_providers")
	logDir := filepath.Join(providerDir, "logs")
	stagingDir := filepath.Join(providerDir, "staging")

	fmt.Printf("Test directories:\n")
	fmt.Printf("  Provider: %s\n", providerDir)
	fmt.Printf("  Logs: %s\n", logDir)
	fmt.Printf("  Staging: %s\n\n", stagingDir)

	// Create test provider metadata for locally built MMA
	testProvider := ucrstypes.Provider{
		ProviderID: "underleaf.mma",
		Version:    "f010910-dirty",
		Capabilities: []ucrstypes.CapabilityRef{
			{ID: "mesh.binding.engine", VersionRange: "^1.0"},
			{ID: "mesh.policy.evaluator", VersionRange: "^1.0"},
		},
		Artifact: ucrstypes.Artifact{
			Type: ucrstypes.ArtifactBinary,
			URI:  "/usr/local/bin/mycelium-mesh-agent",
		},
		TrustTier:      ucrstypes.TrustLocal,
		SandboxProfile: "standard",
		RuntimeRequirements: ucrstypes.RuntimeRequirements{
			Args:           []string{},
			HealthEndpoint: "http://localhost:8080/health",
		},
		Description: "Test MMA binary provider",
		Maintainer:  "test@underleaf.local",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	fmt.Println("Test Provider Configuration:")
	providerJSON, _ := json.MarshalIndent(testProvider, "", "  ")
	fmt.Println(string(providerJSON))
	fmt.Println()

	// Create provider store
	providerStore, err := store.NewProviderStore(providerDir)
	if err != nil {
		log.Fatalf("Failed to create provider store: %v", err)
	}

	// Create lifecycle manager
	config := capability.LifecycleConfig{
		ProviderDir: providerDir,
		LogDir:      logDir,
		StagingDir:  stagingDir,
		MemoryLimit: "512m",
		CPULimit:    "1.0",
	}

	lifecycle, err := capability.NewLifecycleManager(nil, providerStore, config, logger)
	if err != nil {
		log.Fatalf("Failed to create lifecycle manager: %v", err)
	}

	// Test 1: Install
	fmt.Println("=== Test 1: Install Provider ===")
	instance, err := lifecycle.Install(ctx, &testProvider)
	if err != nil {
		log.Fatalf("Install failed: %v", err)
	}
	fmt.Printf("✓ Install successful: %s\n\n", instance.State)

	// Test 2: Start
	fmt.Println("=== Test 2: Start Provider ===")
	if err := lifecycle.Start(ctx, testProvider.ProviderID, testProvider.Version); err != nil {
		log.Fatalf("Start failed: %v", err)
	}
	fmt.Println("✓ Start successful")

	// Wait a bit for process to stabilize
	time.Sleep(3 * time.Second)

	// Test 3: Check status
	fmt.Println("=== Test 3: Check Status ===")
	status, err := providerStore.GetProvider(testProvider.ProviderID, testProvider.Version)
	if err != nil {
		log.Fatalf("Status check failed: %v", err)
	}
	statusJSON, _ := json.MarshalIndent(status, "", "  ")
	fmt.Println(string(statusJSON))
	fmt.Println()

	// Test 4: Stop
	fmt.Println("=== Test 4: Stop Provider ===")
	if err := lifecycle.Stop(ctx, testProvider.ProviderID, testProvider.Version); err != nil {
		log.Fatalf("Stop failed: %v", err)
	}
	fmt.Println("✓ Stop successful")

	// Test 5: Uninstall
	fmt.Println("=== Test 5: Uninstall Provider ===")
	if err := lifecycle.Uninstall(ctx, testProvider.ProviderID, testProvider.Version); err != nil {
		log.Fatalf("Uninstall failed: %v", err)
	}
	fmt.Println("✓ Uninstall successful")

	fmt.Println("=== All Tests Passed ===")
}
