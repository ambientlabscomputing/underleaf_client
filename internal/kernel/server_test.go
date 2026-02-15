package kernel

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/ambientlabscomputing/umc_sdk/proto/ua_kernel/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestSyscallServer_StartStop(t *testing.T) {
	// Use /tmp with shorter path to avoid macOS 104-char socket path limit
	tmpDir, err := os.MkdirTemp("/tmp", "kernel-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	socketPath := filepath.Join(tmpDir, "k.sock")

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	config := Config{
		SocketPath: socketPath,
		Logger:     logger,
		NodeID:     "test-node",
		OrgID:      "test-org",
		ClusterID:  "test-cluster",
	}

	server := NewSyscallServer(config)
	if server == nil {
		t.Fatal("failed to create syscall server")
	}

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		t.Fatalf("socket not created at %s", socketPath)
	}

	conn, err := grpc.NewClient(
		"unix://"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer conn.Close()

	client := pb.NewIdentityServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := client.GetNodeIdentity(ctx, &pb.GetNodeIdentityRequest{})
	if err != nil {
		t.Fatalf("failed to call GetNodeIdentity: %v", err)
	}

	if resp.NodeId != "test-node" {
		t.Errorf("expected node_id=%s, got %s", "test-node", resp.NodeId)
	}
	if resp.OrgId != "test-org" {
		t.Errorf("expected org_id=%s, got %s", "test-org", resp.OrgId)
	}

	t.Log("✓ Syscall server started and responding correctly")

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Errorf("failed to shutdown gracefully: %v", err)
	}

	t.Log("✓ Syscall server stopped gracefully")
}

func TestSyscallServer_SocketPermissions(t *testing.T) {
	// Use /tmp with shorter path to avoid macOS 104-char socket path limit
	tmpDir, err := os.MkdirTemp("/tmp", "kernel-perm-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	socketPath := filepath.Join(tmpDir, "k.sock")

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	config := Config{
		SocketPath: socketPath,
		Logger:     logger,
		NodeID:     "test-node",
		OrgID:      "test-org",
		ClusterID:  "test-cluster",
	}

	server := NewSyscallServer(config)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.GracefulShutdown()

	time.Sleep(100 * time.Millisecond)

	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("failed to stat socket: %v", err)
	}

	mode := info.Mode()
	if mode&os.ModePerm != 0600 {
		t.Errorf("expected socket permissions 0600, got %04o", mode&os.ModePerm)
	}

	t.Log("✓ Socket has correct permissions (0600)")
}
