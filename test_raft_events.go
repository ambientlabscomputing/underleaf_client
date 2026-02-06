package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/agent"
)

func main() {
	// Setup logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	ctx := context.Background()

	// Initialize event stream server (same as UA does)
	socketPath := "/tmp/ua_mma.sock"
	eventServer := agent.NewEventStreamServer(socketPath, "test-cluster", "test-node", logger)

	// Start event stream server
	if err := eventServer.Start(ctx); err != nil {
		fmt.Printf("Failed to start event stream server: %v\n", err)
		os.Exit(1)
	}
	defer eventServer.Stop()

	fmt.Println("Event stream server started on", socketPath)
	fmt.Println("Waiting for MMA to connect...")
	time.Sleep(3 * time.Second)

	// Test 1: Publish member.joined event (simulating Raft AddNode)
	fmt.Println("\n=== Test 1: Publishing member.joined event ===")
	endpoints := []string{"tcp://192.168.1.100:7000"}
	tags := map[string]string{
		"role":   "voter",
		"region": "us-west",
	}
	if err := eventServer.PublishMemberJoined("node-001", endpoints, tags); err != nil {
		fmt.Printf("Failed to publish member.joined: %v\n", err)
	} else {
		fmt.Println("✓ Published member.joined event for node-001")
	}

	time.Sleep(1 * time.Second)

	// Test 2: Publish member.left event (simulating Raft RemoveNode)
	fmt.Println("\n=== Test 2: Publishing member.left event ===")
	if err := eventServer.PublishMemberLeft("node-001", "removed_from_cluster"); err != nil {
		fmt.Printf("Failed to publish member.left: %v\n", err)
	} else {
		fmt.Println("✓ Published member.left event for node-001")
	}

	time.Sleep(1 * time.Second)

	// Test 3: Publish member.updated event (simulating Raft PromoteNode)
	fmt.Println("\n=== Test 3: Publishing member.updated event ===")
	if err := eventServer.PublishMemberUpdated("node-002", nil, map[string]string{"role": "voter"}, "promoted"); err != nil {
		fmt.Printf("Failed to publish member.updated: %v\n", err)
	} else {
		fmt.Println("✓ Published member.updated event for node-002")
	}

	fmt.Println("\n=== Tests complete ===")
	fmt.Println("Check MMA logs to verify events were received")

	// Keep running for a bit to allow processing
	time.Sleep(2 * time.Second)
}
