package kernel

import (
	"context"
	"fmt"

	pb "github.com/ambientlabscomputing/umc_sdk/proto/ua_kernel/v1"
	"github.com/ambientlabscomputing/underleaf_client/internal/capability"
)

// ProviderServer implements the ProviderService for UMCs.
type ProviderServer struct {
	pb.UnimplementedProviderServiceServer
	lifecycleManager *capability.LifecycleManager
	supervisor       *capability.ProcessSupervisor
}

// NewProviderServer creates a new provider server.
func NewProviderServer(lm *capability.LifecycleManager, sup *capability.ProcessSupervisor) *ProviderServer {
	return &ProviderServer{
		lifecycleManager: lm,
		supervisor:       sup,
	}
}

// InstallProvider installs a new capability provider.
func (s *ProviderServer) InstallProvider(ctx context.Context, req *pb.InstallProviderRequest) (*pb.InstallProviderResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// StartProvider starts a capability provider.
func (s *ProviderServer) StartProvider(ctx context.Context, req *pb.StartProviderRequest) (*pb.StartProviderResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// StopProvider stops a capability provider.
func (s *ProviderServer) StopProvider(ctx context.Context, req *pb.StopProviderRequest) (*pb.StopProviderResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// GrantCapability grants a capability to a UMC.
func (s *ProviderServer) GrantCapability(ctx context.Context, req *pb.GrantCapabilityRequest) (*pb.GrantCapabilityResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// RevokeCapability revokes a capability from a UMC.
func (s *ProviderServer) RevokeCapability(ctx context.Context, req *pb.RevokeCapabilityRequest) (*pb.RevokeCapabilityResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// ListProviders lists all installed providers.
func (s *ProviderServer) ListProviders(ctx context.Context, req *pb.ListProvidersRequest) (*pb.ListProvidersResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
