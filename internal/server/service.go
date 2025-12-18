package server

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/bus"
	"github.com/ambientlabscomputing/underleaf_client/internal/config_manager"
	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	servertypes "github.com/ambientlabscomputing/underleaf_client/internal/types/server"
)

type Service interface {
	GetServer(ctx context.Context, serverID string) (servertypes.Server, error)
	ListServers(ctx context.Context) ([]servertypes.Server, error)
	ListServersWithParams(ctx context.Context, params servertypes.ListServersParams) ([]servertypes.Server, error)
	RegisterServer(ctx context.Context, name string) error
	GetLocal(ctx context.Context) (*servertypes.Server, error)
	UpdateServer(ctx context.Context, serverID string, updates servertypes.UpdateServerRequest) (servertypes.Server, error)
	GetServerMetrics(ctx context.Context, serverID string) (*servertypes.ServerMetrics, error)
	UpdateServerMetrics(ctx context.Context, serverID string, metrics servertypes.MetricsUpdateRequest) error
	GetMetricsHistory(ctx context.Context, serverID string, period string, resolution string) (*servertypes.MetricsHistoryResponse, error)
	GetServerActivity(ctx context.Context, serverID string, params servertypes.GetActivityParams) (*servertypes.ActivityResponse, error)
	FindExistingServer(ctx context.Context, nameOrID string) (*servertypes.Server, error)
	DownloadServerConfig(ctx context.Context, server *servertypes.Server) error
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

func (s *ServerService) GetServer(ctx context.Context, serverID string) (servertypes.Server, error) {
	serverIF, err := s.cplane.Servers.GetServer(ctx, serverID)
	if err != nil {
		return servertypes.Server{}, err
	}

	var server servertypes.Server
	if err := convertInterface(serverIF, &server); err != nil {
		return servertypes.Server{}, fmt.Errorf("failed to convert server data: %w", err)
	}
	return server, nil
}

func (s *ServerService) ListServers(ctx context.Context) ([]servertypes.Server, error) {
	return s.ListServersWithParams(ctx, servertypes.ListServersParams{})
}

func (s *ServerService) ListServersWithParams(ctx context.Context, params servertypes.ListServersParams) ([]servertypes.Server, error) {
	serversIF, err := s.cplane.Servers.ListServersWithParams(ctx, params)
	if err != nil {
		return nil, err
	}

	servers := make([]servertypes.Server, len(serversIF))
	for i, serverIF := range serversIF {
		if err := convertInterface(serverIF, &servers[i]); err != nil {
			return nil, fmt.Errorf("failed to convert server %d: %w", i, err)
		}
	}
	return servers, nil
}

func (s *ServerService) RegisterServer(ctx context.Context, name string) error {
	// Check if we already have a server_id in config (idempotent registration)
	existingIDRaw, hasID := s.config.Get("local.server_id")
	if hasID && existingIDRaw != nil {
		// Type assert to string
		existingID, ok := existingIDRaw.(string)
		if ok && existingID != "" {
			// Verify the existing server still exists on the backend
			existingServer, err := s.GetServer(ctx, existingID)
			if err == nil && existingServer.ID != "" {
				// servertypes.Server exists and is valid, reuse it
				// No need to update name - the server already exists
				// Just ensure config has the correct name
				if err := s.config.Set("local.server_name", existingServer.Name); err != nil {
					return fmt.Errorf("failed to save server name: %w", err)
				}
				return nil // Registration already complete (idempotent)
			}
			// If server doesn't exist on backend, continue with new registration
		}
	}

	// Create new server registration
	serverIF, err := s.cplane.Servers.RegisterServer(ctx, name, servertypes.ServerPlatform{
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	})
	if err != nil {
		return err
	}

	var server servertypes.Server
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

func (s *ServerService) UpdateServer(ctx context.Context, serverID string, updates servertypes.UpdateServerRequest) (servertypes.Server, error) {
	serverIF, err := s.cplane.Servers.UpdateServer(ctx, serverID, updates)
	if err != nil {
		return servertypes.Server{}, err
	}

	var server servertypes.Server
	if err := convertInterface(serverIF, &server); err != nil {
		return servertypes.Server{}, fmt.Errorf("failed to convert server data: %w", err)
	}
	return server, nil
}

func (s *ServerService) GetServerMetrics(ctx context.Context, serverID string) (*servertypes.ServerMetrics, error) {
	return s.cplane.Servers.GetServerMetrics(ctx, serverID)
}

func (s *ServerService) UpdateServerMetrics(ctx context.Context, serverID string, metrics servertypes.MetricsUpdateRequest) error {
	return s.cplane.Servers.UpdateServerMetrics(ctx, serverID, metrics)
}

func (s *ServerService) GetMetricsHistory(ctx context.Context, serverID string, period string, resolution string) (*servertypes.MetricsHistoryResponse, error) {
	return s.cplane.Servers.GetMetricsHistory(ctx, serverID, period, resolution)
}

func (s *ServerService) GetServerActivity(ctx context.Context, serverID string, params servertypes.GetActivityParams) (*servertypes.ActivityResponse, error) {
	return s.cplane.Servers.GetServerActivity(ctx, serverID, params)
}

// FindExistingServer searches for a server by name or ID and verifies it hasn't checked in within the last 5 minutes
func (s *ServerService) FindExistingServer(ctx context.Context, nameOrID string) (*servertypes.Server, error) {
	// First try to get by ID
	server, err := s.GetServer(ctx, nameOrID)
	if err == nil {
		// Found by ID, now check if it hasn't checked in within 5 minutes
		if server.LastCheckIn != nil && server.LastCheckIn.Valid {
			timeSinceCheckIn := time.Since(server.LastCheckIn.Time)
			if timeSinceCheckIn < 5*time.Minute {
				return nil, fmt.Errorf("server '%s' checked in %v ago (within last 5 minutes), cannot claim", server.Name, timeSinceCheckIn.Round(time.Second))
			}
		}
		return &server, nil
	}

	// Not found by ID, search by name
	servers, err := s.ListServersWithParams(ctx, servertypes.ListServersParams{
		Search: nameOrID,
		Limit:  100, // Get enough results to find matches
	})
	if err != nil {
		return nil, fmt.Errorf("failed to search for server: %w", err)
	}

	// Find exact name match
	var matchedServer *servertypes.Server
	for _, srv := range servers {
		if srv.Name == nameOrID {
			// Check if it hasn't checked in within 5 minutes
			if srv.LastCheckIn != nil && srv.LastCheckIn.Valid {
				timeSinceCheckIn := time.Since(srv.LastCheckIn.Time)
				if timeSinceCheckIn < 5*time.Minute {
					return nil, fmt.Errorf("server '%s' checked in %v ago (within last 5 minutes), cannot claim", srv.Name, timeSinceCheckIn.Round(time.Second))
				}
			}
			matchedServer = &srv
			break
		}
	}

	if matchedServer == nil {
		return nil, fmt.Errorf("no server found with name or ID '%s'", nameOrID)
	}

	return matchedServer, nil
}

// DownloadServerConfig downloads an existing server's configuration to the local config
func (s *ServerService) DownloadServerConfig(ctx context.Context, server *servertypes.Server) error {
	if server == nil {
		return fmt.Errorf("server cannot be nil")
	}

	// Save server metadata to local config
	if err := s.config.Set("local.server_id", server.ID); err != nil {
		return fmt.Errorf("failed to save server ID: %w", err)
	}
	if err := s.config.Set("local.server_name", server.Name); err != nil {
		return fmt.Errorf("failed to save server name: %w", err)
	}

	// Optionally save other metadata if available
	if server.Location != "" {
		if err := s.config.Set("local.location", server.Location); err != nil {
			return fmt.Errorf("failed to save server location: %w", err)
		}
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
