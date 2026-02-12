package kernel

import (
	"fmt"
	"log/slog"
	"net"
	"os"

	pb "github.com/ambientlabscomputing/umc_sdk/proto/ua_kernel/v1"
	"github.com/ambientlabscomputing/underleaf_client/internal/bus"
	"github.com/ambientlabscomputing/underleaf_client/internal/capability"
	"github.com/ambientlabscomputing/underleaf_client/internal/crypto/keymanager"
	"github.com/ambientlabscomputing/underleaf_client/internal/exec"
	"github.com/ambientlabscomputing/underleaf_client/internal/raft"
	"google.golang.org/grpc"
)

// SyscallServer is the main kernel syscall server.
type SyscallServer struct {
	config   Config
	server   *grpc.Server
	listener net.Listener
	logger   *slog.Logger
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
	Logger           *slog.Logger
}

// NewSyscallServer creates a new syscall server.
func NewSyscallServer(config Config) *SyscallServer {
	server := grpc.NewServer()

	// Create and register all service implementations
	pb.RegisterIdentityServiceServer(server, NewIdentityServer(config.KeyManager, config.NodeID, config.OrgID, config.ClusterID))
	pb.RegisterSecretServiceServer(server, NewSecretServer(config.SecretStore))
	pb.RegisterExecServiceServer(server, NewExecServer(config.ExecRunner))
	pb.RegisterClusterServiceServer(server, NewClusterServer(config.RaftNode))
	pb.RegisterEventServiceServer(server, NewEventServer(config.BusClient))
	pb.RegisterProviderServiceServer(server, NewProviderServer(config.LifecycleManager, config.Supervisor))

	return &SyscallServer{
		config: config,
		server: server,
		logger: config.Logger,
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
