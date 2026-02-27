package utils

import (
	"context"
	"net/http"

	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
	"github.com/ambientlabscomputing/underleaf_client/internal/server"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
)

type DependencyManager struct {
	Printer      ui.Printer
	ConfigClient policy_manager.ConfigClient
	CPlaneClient controlplane.CPlaneClient
	ServerSvc    server.ServerService
}

// getConfigValue tries to get a value with fallback to non-prefixed key for backward compatibility
func getConfigValue(config policy_manager.ConfigClient, key string) (interface{}, bool) {
	// Try with local. prefix first
	if val, ok := config.Get("local." + key); ok {
		return val, true
	}
	// Fallback to non-prefixed for backward compatibility
	return config.Get(key)
}

func NewDependencyManager(ctx context.Context) *DependencyManager {
	printer := ui.GetPrinter(ctx)
	configClient := policy_manager.NewConfigClient(policy_manager.ConfigClientTypeCLI)

	// Configure HTTP client with mTLS transport if certificate is available
	var httpClient *http.Client
	certPath, hasCert := getConfigValue(configClient, "mtls.certificate_path")
	keyPath, hasKey := getConfigValue(configClient, "mtls.private_key_path")

	if hasCert && hasKey && certPath != "" && keyPath != "" {
		// Try mTLS first - prefer certificate auth over JWT
		transport, err := controlplane.NewMTLSTransport(
			http.DefaultTransport,
			keyPath.(string),
			certPath.(string),
		)
		if err != nil {
			// Certificate loading failed, fall back to JWT auth
			printer.PrintWarning("Failed to load mTLS certificate, falling back to JWT: " + err.Error())
			httpClient = http.DefaultClient
		} else {
			// Successfully using mTLS certificate authentication
			httpClient = &http.Client{Transport: transport}
		}
	} else {
		// No certificate available, use JWT authentication
		httpClient = http.DefaultClient
	}

	cPlane := controlplane.NewCPlaneClient(&configClient, httpClient)

	service := server.NewServerService(cPlane, configClient)
	return &DependencyManager{
		Printer:      *printer,
		ConfigClient: configClient,
		CPlaneClient: *cPlane,
		ServerSvc:    *service,
	}
}
