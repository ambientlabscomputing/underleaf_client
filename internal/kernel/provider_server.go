package kernel

import (
	"context"
	"fmt"
	"strings"

	pb "github.com/ambientlabscomputing/umc_sdk/proto/ua_kernel/v1"
	"github.com/ambientlabscomputing/underleaf_client/internal/capability"
	"github.com/ambientlabscomputing/underleaf_client/internal/raft"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ProviderServer implements the ProviderService for UMCs.
type ProviderServer struct {
	pb.UnimplementedProviderServiceServer
	lifecycleManager *capability.LifecycleManager
	supervisor       *capability.ProcessSupervisor
	raftNode         *raft.Node
}

// NewProviderServer creates a new provider server.
func NewProviderServer(lm *capability.LifecycleManager, sup *capability.ProcessSupervisor, node *raft.Node) *ProviderServer {
	return &ProviderServer{
		lifecycleManager: lm,
		supervisor:       sup,
		raftNode:         node,
	}
}

// InstallProvider installs a new capability provider.
func (s *ProviderServer) InstallProvider(ctx context.Context, req *pb.InstallProviderRequest) (*pb.InstallProviderResponse, error) {
	if s.lifecycleManager == nil {
		return nil, status.Error(codes.Unavailable, "lifecycle manager not available")
	}

	// TODO: Full implementation requires fetching provider metadata from UCRS
	return nil, status.Error(codes.Unimplemented, "provider installation via syscall requires UCRS integration (use capability manager directly)")
}

// StartProvider starts a capability provider.
func (s *ProviderServer) StartProvider(ctx context.Context, req *pb.StartProviderRequest) (*pb.StartProviderResponse, error) {
	if s.lifecycleManager == nil {
		return &pb.StartProviderResponse{
			Success: false,
			Error:   "lifecycle manager not available",
		}, nil
	}

	err := s.lifecycleManager.Start(ctx, req.ProviderId, req.Version)
	if err != nil {
		return &pb.StartProviderResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to start provider: %v", err),
		}, nil
	}

	// Try to get the process ID from the supervisor
	var processID string
	if s.supervisor != nil {
		processes := s.supervisor.List()
		for _, proc := range processes {
			if proc.ProviderID == req.ProviderId && proc.Version == req.Version {
				processID = fmt.Sprintf("%d", proc.PID)
				break
			}
		}
	}

	return &pb.StartProviderResponse{
		Success:   true,
		ProcessId: processID,
	}, nil
}

// StopProvider stops a capability provider.
// Stops all running versions of the provider with the given ID.
func (s *ProviderServer) StopProvider(ctx context.Context, req *pb.StopProviderRequest) (*pb.StopProviderResponse, error) {
	if s.lifecycleManager == nil {
		return nil, status.Error(codes.Unavailable, "lifecycle manager not available")
	}

	// Enumerate all running versions of this provider
	var versions []string
	if s.supervisor != nil {
		processes := s.supervisor.List()
		for _, proc := range processes {
			if proc.ProviderID == req.ProviderId {
				versions = append(versions, proc.Version)
			}
		}
	}

	// If no running versions found, return success (idempotent)
	if len(versions) == 0 {
		return &pb.StopProviderResponse{
			Success: true,
		}, nil
	}

	// Stop all versions
	var errors []string
	for _, version := range versions {
		if err := s.lifecycleManager.Stop(ctx, req.ProviderId, version); err != nil {
			errors = append(errors, fmt.Sprintf("version %s: %v", version, err))
		}
	}

	if len(errors) > 0 {
		return &pb.StopProviderResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to stop some versions: %s", strings.Join(errors, "; ")),
		}, nil
	}

	return &pb.StopProviderResponse{
		Success: true,
	}, nil
}

// GrantCapability grants a capability to a UMC.
func (s *ProviderServer) GrantCapability(ctx context.Context, req *pb.GrantCapabilityRequest) (*pb.GrantCapabilityResponse, error) {
	if s.raftNode == nil {
		return &pb.GrantCapabilityResponse{
			Success: false,
			Error:   "raft node not available for ACL storage",
		}, nil
	}

	// Store capability grant in Raft KV under /acl/provider/{provider_id}/{capability}
	aclKey := fmt.Sprintf("/acl/provider/%s/%s", req.ProviderId, req.Capability)
	kv := raft.NewKV(s.raftNode)
	err := kv.Put(aclKey, []byte("granted"), "kernel")
	if err != nil {
		return &pb.GrantCapabilityResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to store grant: %v", err),
		}, nil
	}

	return &pb.GrantCapabilityResponse{
		Success: true,
	}, nil
}

// RevokeCapability revokes a capability from a UMC.
func (s *ProviderServer) RevokeCapability(ctx context.Context, req *pb.RevokeCapabilityRequest) (*pb.RevokeCapabilityResponse, error) {
	if s.raftNode == nil {
		return &pb.RevokeCapabilityResponse{
			Success: false,
			Error:   "raft node not available for ACL storage",
		}, nil
	}

	// Delete capability grant from Raft KV
	aclKey := fmt.Sprintf("/acl/provider/%s/%s", req.ProviderId, req.Capability)
	kv := raft.NewKV(s.raftNode)
	err := kv.Delete(aclKey)
	if err != nil {
		return &pb.RevokeCapabilityResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to revoke grant: %v", err),
		}, nil
	}

	return &pb.RevokeCapabilityResponse{
		Success: true,
	}, nil
}

// UpdateDependencies updates the lifecycle manager and supervisor (for late wiring).
func (s *ProviderServer) UpdateDependencies(lm *capability.LifecycleManager, sup *capability.ProcessSupervisor) {
	s.lifecycleManager = lm
	s.supervisor = sup
}

// ListProviders lists all installed providers.
func (s *ProviderServer) ListProviders(ctx context.Context, req *pb.ListProvidersRequest) (*pb.ListProvidersResponse, error) {
	if s.lifecycleManager == nil {
		return &pb.ListProvidersResponse{
			Providers: []*pb.ProviderInfo{},
		}, nil
	}

	// Get provider instances from the supervisor
	var providers []*pb.ProviderInfo

	if s.supervisor != nil {
		processes := s.supervisor.List()
		for _, proc := range processes {
			// Map internal state to proto ProviderState enum
			var state pb.ProviderState
			switch proc.State {
			case capability.ProcessStateStarting:
				state = pb.ProviderState_PROVIDER_STATE_STARTING
			case capability.ProcessStateRunning:
				state = pb.ProviderState_PROVIDER_STATE_RUNNING
			case capability.ProcessStateStopped:
				state = pb.ProviderState_PROVIDER_STATE_STOPPED
			case capability.ProcessStateFailed, capability.ProcessStateCrashed:
				state = pb.ProviderState_PROVIDER_STATE_FAILED
			default:
				state = pb.ProviderState_PROVIDER_STATE_UNSPECIFIED
			}

			providers = append(providers, &pb.ProviderInfo{
				ProviderId: proc.ProviderID,
				Version:    proc.Version,
				State:      state,
				StartedAt:  timestamppb.New(proc.StartTime),
				// TODO: Populate trust_tier from ACL KV store
				// TODO: Populate capabilities from ACL KV store
				// TODO: Populate installed_at from provider metadata
			})
		}
	}

	return &pb.ListProvidersResponse{
		Providers: providers,
	}, nil
}
