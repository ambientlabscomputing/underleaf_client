package utils

import (
	"context"
	"net/http"

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

	// Configure HTTP client with mTLS transport if certificate is available
	var httpClient *http.Client
	certPath, hasCert := getConfigValue(configClient, "mtls.certificate_path")
	keyPath, hasKey := getConfigValue(configClient, "mtls.private_key_path")

	if hasCert && hasKey && certPath != "" && keyPath != "" {
		// Create mTLS transport with certificate
		transport, err := controlplane.NewMTLSTransport(
			http.DefaultTransport,
			keyPath.(string),
			certPath.(string),
		)
		if err != nil {
			// Certificate loading failed, fall back to JWT auth
			printer.PrintWarning("Failed to load mTLS certificate: " + err.Error())
			httpClient = http.DefaultClient
		} else {
			httpClient = &http.Client{Transport: transport}
		}
	} else {
		// No certificate available, use default HTTP client (JWT auth)
		httpClient = http.DefaultClient
	}

	cPlane := controlplane.NewCPlaneClient(&configClient, httpClient)

	// Event bus disabled for CLI to avoid race conditions in event_bus_client library
	// The CLI only makes REST API calls and doesn't need real-time event subscriptions
	// Event bus is only needed for the agent which runs continuously
	var appEventClient bus.EventClient = nil

	service := server.NewServerService(cPlane, configClient, appEventClient)
	return &DependencyManager{
		Printer:      *printer,
		ConfigClient: configClient,
		CPlaneClient: *cPlane,
		ServerSvc:    *service,
	}
}
