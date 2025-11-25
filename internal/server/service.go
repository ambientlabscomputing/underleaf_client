package server

import (
	"context"
	"runtime"

	"github.com/ambientlabscomputing/underleaf_client/internal/config_manager"
	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
)

type Service interface {
	GetServer(ctx context.Context, serverID string) (Server, error)
	ListServers(ctx context.Context) ([]Server, error)
	RegisterServer(ctx context.Context, name string) error
	GetLocal(ctx context.Context) (*Server, error)
}
type ServerService struct {
	cplane *controlplane.CPlaneClient
	config config_manager.ConfigClient
}

func NewServerService(cplaneClient *controlplane.CPlaneClient, configClient config_manager.ConfigClient) *ServerService {
	return &ServerService{
		cplane: cplaneClient,
		config: configClient,
	}
}

func (s *ServerService) GetServer(ctx context.Context, serverID string) (Server, error) {
	serverIF, err := s.cplane.Servers.GetServer(ctx, serverID)
	if err != nil {
		return Server{}, err
	}
	return serverIF.(Server), nil
}

func (s *ServerService) ListServers(ctx context.Context) ([]Server, error) {
	serversIF, err := s.cplane.Servers.ListServers(ctx)
	if err != nil {
		return nil, err
	}
	servers := make([]Server, len(serversIF))
	for i, serverIF := range serversIF {
		servers[i] = serverIF.(Server)
	}
	return servers, nil
}

func (s *ServerService) RegisterServer(ctx context.Context, name string) error {
	serverIF, err := s.cplane.Servers.RegisterServer(ctx, name, ServerPlatform{
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	})
	if err != nil {
		return err
	}
	server := serverIF.(Server)
	return s.config.Set("server", server)
}
