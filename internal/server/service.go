package server

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/ambientlabscomputing/underleaf_client/internal/bus"
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
	cplane   *controlplane.CPlaneClient
	config   config_manager.ConfigClient
	eventBus bus.EventClient
}

func NewServerService(cplaneClient *controlplane.CPlaneClient, configClient config_manager.ConfigClient, eventBusClient bus.EventClient) *ServerService {
	return &ServerService{
		cplane:   cplaneClient,
		config:   configClient,
		eventBus: eventBusClient,
	}
}

func (s *ServerService) GetServer(ctx context.Context, serverID string) (Server, error) {
	serverIF, err := s.cplane.Servers.GetServer(ctx, serverID)
	if err != nil {
		return Server{}, err
	}

	var server Server
	if err := convertInterface(serverIF, &server); err != nil {
		return Server{}, fmt.Errorf("failed to convert server data: %w", err)
	}
	return server, nil
}

func (s *ServerService) ListServers(ctx context.Context) ([]Server, error) {
	serversIF, err := s.cplane.Servers.ListServers(ctx)
	if err != nil {
		return nil, err
	}

	servers := make([]Server, len(serversIF))
	for i, serverIF := range serversIF {
		if err := convertInterface(serverIF, &servers[i]); err != nil {
			return nil, fmt.Errorf("failed to convert server %d: %w", i, err)
		}
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

	var server Server
	if err := convertInterface(serverIF, &server); err != nil {
		return fmt.Errorf("failed to convert registered server: %w", err)
	}

	// Save server metadata to local config (not the full server object)
	if err := s.config.Set("local.server_id", server.ID); err != nil {
		return fmt.Errorf("failed to save server ID: %w", err)
	}
	if err := s.config.Set("local.server_name", server.Name); err != nil {
		return fmt.Errorf("failed to save server name: %w", err)
	}

	return nil
}

// convertInterface converts an interface{} (typically map[string]interface{}) to a typed struct
func convertInterface(src interface{}, dest interface{}) error {
	jsonBytes, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(jsonBytes, dest)
}

// func (s *ServerService) HandleRunCommand(ctx context.Context, serverID string) error {
