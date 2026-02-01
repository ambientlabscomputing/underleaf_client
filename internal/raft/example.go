package raft

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// Example demonstrates basic usage of the Raft KV store.
func Example() {
	// Setup logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Create temporary directory for this example
	tempDir, err := os.MkdirTemp("", "raft-example-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tempDir)

	// Configure and start a single-node cluster
	config := &NodeConfig{
		NodeID:           "node1",
		BindAddr:         "127.0.0.1:7000",
		AdvertiseAddr:    "127.0.0.1:7000",
		DataDir:          filepath.Join(tempDir, "node1"),
		Bootstrap:        true,
		HeartbeatTimeout: 1000 * time.Millisecond,
		ElectionTimeout:  1000 * time.Millisecond,
		MaxValueSize:     1024 * 1024,
		MaxStorageSize:   100 * 1024 * 1024,
	}

	// Create and start node
	node, err := NewNode(config, logger)
	if err != nil {
		panic(err)
	}

	if err := node.Start(); err != nil {
		panic(err)
	}
	defer node.Stop()

	// Wait for leader election
	if err := node.WaitForLeader(5 * time.Second); err != nil {
		panic(err)
	}

	logger.Info("cluster ready", "role", node.GetRole())

	// Example 1: Basic KV operations
	kv := NewKV(node)

	// Put a value
	key := "/org/acme/cluster/prod/config/database"
	value := []byte(`{"host": "db.example.com", "port": 5432}`)

	if err := kv.Put(key, value, ""); err != nil {
		panic(err)
	}
	logger.Info("stored key", "key", key)

	// Get the value with linearizable read
	entry, err := kv.Get(key, ReadModeLinearizable)
	if err != nil {
		panic(err)
	}
	logger.Info("retrieved key", "key", entry.Key, "value", string(entry.Value), "revision", entry.Revision)

	// List keys with prefix
	entries, err := kv.List("/org/acme/", ReadModeLinearizable)
	if err != nil {
		panic(err)
	}
	logger.Info("listed keys", "count", len(entries))

	// Example 2: Watch for changes
	watch := NewWatch(node)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	watcher, err := watch.WatchPrefix(ctx, "/org/acme/", 0)
	if err != nil {
		panic(err)
	}

	// Watch in background
	go func() {
		for event := range watcher.Events() {
			logger.Info("watch event", "type", event.Type, "key", event.Key, "revision", event.Revision)
		}
	}()

	// Make a change
	time.Sleep(100 * time.Millisecond)
	if err := kv.Put("/org/acme/cluster/prod/config/cache", []byte("redis://localhost:6379"), ""); err != nil {
		panic(err)
	}

	time.Sleep(500 * time.Millisecond)

	// Example 3: Distributed lock
	lock := NewLock(node)

	lockInfo, err := lock.Acquire(context.Background(), "/resources/worker-1", 30, "example-process")
	if err != nil {
		panic(err)
	}
	logger.Info("lock acquired", "key", lockInfo.Key, "holder", lockInfo.Holder)

	// Do work while holding the lock
	time.Sleep(1 * time.Second)

	// Release lock
	if err := lock.Release(lockInfo); err != nil {
		panic(err)
	}
	logger.Info("lock released")

	// Example 4: Lease management
	lease := NewLease(node)

	leaseID, err := lease.Grant(60, "example-holder")
	if err != nil {
		panic(err)
	}
	logger.Info("lease granted", "lease_id", leaseID)

	// Attach a key to the lease (it will be deleted when lease expires)
	tempKey := "/temp/ephemeral-data"
	if err := kv.Put(tempKey, []byte("temporary"), leaseID); err != nil {
		panic(err)
	}
	logger.Info("ephemeral key created", "key", tempKey, "lease_id", leaseID)

	// Example 5: Leader election
	election := NewElection(node)

	electionInfo, err := election.Campaign(context.Background(), "coordinator", "worker-1", 30)
	if err != nil {
		logger.Warn("election failed", "error", err)
	} else {
		logger.Info("election won", "name", electionInfo.Name, "leader", electionInfo.Leader)
		defer election.Resign(electionInfo)
	}

	// Example 6: Cluster stats
	stats, err := node.GetStats()
	if err != nil {
		panic(err)
	}
	logger.Info("cluster stats",
		"node_id", stats.NodeID,
		"role", stats.Role,
		"term", stats.Term,
		"keys", stats.KeyCount,
		"storage_bytes", stats.StorageSize)

	fmt.Println("\nExample completed successfully!")
}

// ExampleMultiNode demonstrates a 3-node cluster setup.
func ExampleMultiNode() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "raft-multi-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tempDir)

	nodes := make([]*Node, 3)
	addresses := []string{
		"127.0.0.1:7001",
		"127.0.0.1:7002",
		"127.0.0.1:7003",
	}

	// Start first node with bootstrap
	config1 := &NodeConfig{
		NodeID:           "node1",
		BindAddr:         addresses[0],
		AdvertiseAddr:    addresses[0],
		DataDir:          filepath.Join(tempDir, "node1"),
		Bootstrap:        true,
		BootstrapPeers:   []string{"node1", "node2", "node3"},
		HeartbeatTimeout: 1000 * time.Millisecond,
		ElectionTimeout:  1000 * time.Millisecond,
		MaxValueSize:     1024 * 1024,
		MaxStorageSize:   100 * 1024 * 1024,
	}

	nodes[0], err = NewNode(config1, logger)
	if err != nil {
		panic(err)
	}
	if err := nodes[0].Start(); err != nil {
		panic(err)
	}
	defer nodes[0].Stop()

	// Wait for initial leader
	if err := nodes[0].WaitForLeader(5 * time.Second); err != nil {
		panic(err)
	}

	logger.Info("3-node cluster example",
		"note", "In production, nodes 2 and 3 would join via AddNode()",
	)

	fmt.Println("\nMulti-node example setup complete!")
}
