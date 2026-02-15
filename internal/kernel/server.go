package kernel

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	pb "github.com/ambientlabscomputing/umc_sdk/proto/ua_kernel/v1"
	"github.com/ambientlabscomputing/underleaf_client/internal/bus"
	"github.com/ambientlabscomputing/underleaf_client/internal/capability"
	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/crypto/keymanager"
	"github.com/ambientlabscomputing/underleaf_client/internal/exec"
	"github.com/ambientlabscomputing/underleaf_client/internal/raft"
	"google.golang.org/grpc"
)

// SyscallServer is the main kernel syscall server.
type SyscallServer struct {
	config         Config
	server         *grpc.Server
	listener       net.Listener
	logger         *slog.Logger
	providerServer *ProviderServer // Keep reference for late wiring
	secretServer   *SecretServer   // Keep reference for late SecretStore updates
}

// Config holds the configuration for the syscall server.
type Config struct {
	SocketPath       string
	KeyManager       keymanager.KeyManager
	RaftNode         *raft.Node
	SecretStore      *raft.SecretStore
	ExecRunner       *exec.LocalRunner
	BusClient        *bus.Client
	LifecycleManager *capability.LifecycleManager
	Supervisor       *capability.ProcessSupervisor
	NodeID           string
	OrgID            string
	ClusterID        string
	APIClient        *controlplane.APIClient
	ServerID         string
	Logger           *slog.Logger
}

// NewSyscallServer creates a new syscall server.
func NewSyscallServer(config Config) *SyscallServer {
	// Create server with auth interceptors
	server := grpc.NewServer(
		grpc.UnaryInterceptor(UnaryAuthInterceptor(config.Logger)),
		grpc.StreamInterceptor(StreamAuthInterceptor(config.Logger)),
	)

	// Create and register all service implementations
	pb.RegisterIdentityServiceServer(server, NewIdentityServer(config.KeyManager, config.NodeID, config.OrgID, config.ClusterID, config.APIClient, config.ServerID))

	secretServer := NewSecretServer(config.SecretStore)
	pb.RegisterSecretServiceServer(server, secretServer)

	pb.RegisterExecServiceServer(server, NewExecServer(config.ExecRunner))
	pb.RegisterClusterServiceServer(server, NewClusterServer(config.RaftNode))
	pb.RegisterEventServiceServer(server, NewEventServer(config.BusClient))

	// Keep reference to provider server for late wiring
	providerServer := NewProviderServer(config.LifecycleManager, config.Supervisor, config.RaftNode)
	pb.RegisterProviderServiceServer(server, providerServer)

	return &SyscallServer{
		config:         config,
		server:         server,
		logger:         config.Logger,
		providerServer: providerServer,
		secretServer:   secretServer,
	}
}

// Start starts the syscall server on a Unix domain socket.
func (s *SyscallServer) Start() error {
	// Remove existing socket if it exists
	if err := os.RemoveAll(s.config.SocketPath); err != nil {
		return fmt.Errorf("failed to remove existing socket: %w", err)
	}

	// Create Unix domain socket listener
	listener, err := net.Listen("unix", s.config.SocketPath)
	if err != nil {
		return fmt.Errorf("failed to create socket listener: %w", err)
	}

	// Set socket permissions (owner only)
	if err := os.Chmod(s.config.SocketPath, 0600); err != nil {
		listener.Close()
		return fmt.Errorf("failed to set socket permissions: %w", err)
	}

	s.listener = listener

	go func() {
		if err := s.server.Serve(listener); err != nil {
			s.logger.Error("syscall server error", "error", err)
		}
	}()

	s.logger.Info("syscall server started", "socket", s.config.SocketPath)
	return nil
}

// Stop stops the syscall server.
func (s *SyscallServer) Stop() error {
	s.logger.Info("stopping syscall server")
	s.server.GracefulStop()

	if s.listener != nil {
		s.listener.Close()
	}

	if err := os.RemoveAll(s.config.SocketPath); err != nil {
		s.logger.Warn("failed to remove socket file", "error", err)
	}

	return nil
}

// Shutdown performs a graceful shutdown of the syscall server with a context timeout.
// It ensures all in-flight RPCs complete before shutting down the server.
func (s *SyscallServer) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down syscall server gracefully")

	// Create a channel to signal when graceful stop completes
	done := make(chan struct{})

	go func() {
		s.server.GracefulStop()
		close(done)
	}()

	// Wait for graceful stop or context timeout
	select {
	case <-done:
		s.logger.Info("syscall server stopped gracefully")
	case <-ctx.Done():
		s.logger.Warn("syscall server shutdown timeout, forcing stop")
		s.server.Stop() // Force stop if graceful stop times out
	}

	// Close listener
	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			s.logger.Warn("error closing listener", "error", err)
		}
	}

	// Clean up socket file
	if err := os.RemoveAll(s.config.SocketPath); err != nil {
		s.logger.Warn("failed to remove socket file", "error", err)
	}

	s.logger.Info("syscall server shutdown complete")
	return nil
}

// GracefulShutdown performs a graceful shutdown with a default 30-second timeout.
func (s *SyscallServer) GracefulShutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.Shutdown(ctx)
}

// UpdateSecretStore updates the secret store after raft initialization.
// This is used when raft is initialized asynchronously after server creation.
func (s *SyscallServer) UpdateSecretStore(secretStore *raft.SecretStore) {
	if s.secretServer != nil {
		s.secretServer.SetSecretStore(secretStore)
		s.logger.Info("secret store updated in syscall server")
	}
}

// WireCapabilityManager wires the capability manager's lifecycle manager and supervisor
// into the provider server after initialization.
func (s *SyscallServer) WireCapabilityManager(mgr *capability.Manager) {
	if s.providerServer != nil && mgr != nil {
		// Get lifecycle manager and supervisor from capability manager
		lifecycleManager := mgr.GetLifecycleManager()
		var supervisor *capability.ProcessSupervisor
		if lifecycleManager != nil {
			supervisor = lifecycleManager.GetSupervisor()
		}
		s.providerServer.UpdateDependencies(lifecycleManager, supervisor)
		s.logger.Info("provider server dependencies updated",
			"has_lifecycle", lifecycleManager != nil,
			"has_supervisor", supervisor != nil)
	}
}
