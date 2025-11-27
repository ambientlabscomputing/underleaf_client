package utils

import (
	"context"
	"net/http"

	"github.com/ambientlabscomputing/event_bus_client"
	"github.com/ambientlabscomputing/underleaf_client/internal/bus"
	"github.com/ambientlabscomputing/underleaf_client/internal/config_manager"
	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/server"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
)

type DependencyManager struct {
	Printer      ui.Printer
	ConfigClient config_manager.ConfigClient
	CPlaneClient controlplane.CPlaneClient
	ServerSvc    server.ServerService
}

// getConfigValue tries to get a value with fallback to non-prefixed key for backward compatibility
func getConfigValue(config config_manager.ConfigClient, key string) (interface{}, bool) {
	// Try with local. prefix first
	if val, ok := config.Get("local." + key); ok {
		return val, true
	}
	// Fallback to non-prefixed for backward compatibility
	return config.Get(key)
}

func NewDependencyManager(ctx context.Context) *DependencyManager {
	printer := ui.GetPrinter(ctx)
	configClient := config_manager.NewConfigClient(config_manager.ConfigClientTypeCLI)
	h := http.DefaultClient
	cPlane := controlplane.NewCPlaneClient(&configClient, h)

	// Event bus is optional - only initialize if all required config is present
	var appEventClient bus.EventClient
	endpoint, hasEndpoint := getConfigValue(configClient, "event_bus.endpoint")
	token, hasToken := getConfigValue(configClient, "auth.token")
	serverID, hasServerID := getConfigValue(configClient, "server_id")

	if hasEndpoint && hasToken && hasServerID {
		commitInterval, _ := getConfigValue(configClient, "event_bus.commit_interval")
		if commitInterval == "" || commitInterval == nil {
			commitInterval = "5s"
		}

		eventBusClient := event_bus_client.NewEventClient(event_bus_client.EventClientOpts{
			Endpoint:       endpoint.(string),
			CommitInterval: commitInterval.(string),
			AuthToken:      token.(string),
			GroupID:        serverID.(string),
		})

		busClient, err := bus.NewClient(eventBusClient)
		if err != nil {
			printer.PrintWarning("Failed to create event bus client: " + err.Error())
			appEventClient = nil
		} else {
			if err := busClient.Start(ctx, serverID.(string)); err != nil {
				printer.PrintWarning("Failed to start event bus client: " + err.Error())
				appEventClient = nil
			} else {
				appEventClient = busClient
			}
		}
	}

	if appEventClient == nil {
		printer.PrintInfo("Event bus not configured - some features may be limited")
	}

	service := server.NewServerService(cPlane, configClient, appEventClient)
	return &DependencyManager{
		Printer:      *printer,
		ConfigClient: configClient,
		CPlaneClient: *cPlane,
		ServerSvc:    *service,
	}
}
