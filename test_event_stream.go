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
	if len(os.Args) < 2 {
		fmt.Println("Usage: test_event_stream <command>")
		fmt.Println("Commands:")
		fmt.Println("  server    - Start the UA event stream server")
		fmt.Println("  test      - Send test events (assumes server is running)")
		os.Exit(1)
	}

	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	switch os.Args[1] {
	case "server":
		startServer(ctx, logger)
	case "test":
		sendTestEvents(ctx, logger)
	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

func startServer(ctx context.Context, logger *slog.Logger) {
	fmt.Println("Starting UA event stream server...")

	server := agent.NewEventStreamServer("/tmp/ua_mma.sock", "test-cluster", "node-001", logger)

	if err := server.Start(ctx); err != nil {
		logger.Error("failed to start server", "error", err)
		os.Exit(1)
	}

	logger.Info("UA event stream server started", "socket", "/tmp/ua_mma.sock")
	fmt.Println("Server running. Press Ctrl+C to stop.")
	fmt.Println("Waiting 10 seconds, then will send test events...")

	// Wait for subscribers to connect, then send test events
	go func() {
		time.Sleep(10 * time.Second)
		fmt.Println("Sending test events...")
		if err := server.SendTestEvents(ctx); err != nil {
			logger.Error("failed to send test events", "error", err)
		} else {
			fmt.Println("Test events sent!")
		}
	}()

	// Keep running
	select {}
}

func sendTestEvents(ctx context.Context, logger *slog.Logger) {
	fmt.Println("Connecting to UA event stream server...")

	// Create a server instance (without starting it, just to access the helper methods)
	server := agent.NewEventStreamServer("/tmp/ua_mma.sock", "test-cluster", "node-001", logger)

	// The server needs to be started to publish events
	// In a real scenario, the server would already be running
	// For this test, we'll create a client that connects to the existing server

	fmt.Println("Sending test events...")
	if err := server.SendTestEvents(ctx); err != nil {
		logger.Error("failed to send test events", "error", err)
		os.Exit(1)
	}

	fmt.Println("Test events sent successfully!")
	time.Sleep(1 * time.Second)
}
