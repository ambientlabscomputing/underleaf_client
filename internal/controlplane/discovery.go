package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/mdns"
	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
)

// DiscoveryClient handles service discovery for the Underleaf API
type DiscoveryClient struct {
	mdnsClient *mdns.Client
	apiClient  *APIClient
	config     policy_manager.ConfigClient
	logger     *slog.Logger

	// Cached discovery information
	lastDiscoveredService *mdns.ServiceInfo
	lastDiscoveryTime     time.Time
	discoveryMu           sync.RWMutex

	// Discovery settings
	discoveryTimeout time.Duration
	cacheTimeout     time.Duration
}

// NewDiscoveryClient creates a new discovery client
func NewDiscoveryClient(apiClient *APIClient, config policy_manager.ConfigClient, logger *slog.Logger) *DiscoveryClient {
	if logger == nil {
		logger = slog.Default()
	}

	return &DiscoveryClient{
		mdnsClient:       mdns.NewClient(logger),
		apiClient:        apiClient,
		config:           config,
		logger:           logger,
		discoveryTimeout: 3 * time.Second,
		cacheTimeout:     5 * time.Second, // Cache discovery results for 5 seconds
	}
}

// DiscoverAPIEndpoint discovers the Underleaf API endpoint using mDNS
// Falls back to static configuration if mDNS discovery fails
func (d *DiscoveryClient) DiscoverAPIEndpoint(ctx context.Context) (string, error) {
	// Check if mDNS is enabled in config
	mdnsEnabled := false
	if val, ok := d.config.Get("mdns.enabled"); ok && val != nil {
		if enabled, ok := val.(bool); ok {
			mdnsEnabled = enabled
		}
	}

	if !mdnsEnabled {
		d.logger.Debug("mDNS discovery is disabled, using static configuration")
		return d.getStaticAPIEndpoint()
	}

	// Check cache first
	d.discoveryMu.RLock()
	if d.lastDiscoveredService != nil && time.Since(d.lastDiscoveryTime) < d.cacheTimeout {
		endpoint := d.buildEndpointURL(d.lastDiscoveredService)
		d.discoveryMu.RUnlock()
		d.logger.Debug("using cached mDNS discovery result", "endpoint", endpoint)
		return endpoint, nil
	}
	d.discoveryMu.RUnlock()

	// Perform mDNS discovery
	d.logger.Debug("attempting mDNS discovery", "timeout", d.discoveryTimeout)
	service, err := d.mdnsClient.Discover(ctx, d.discoveryTimeout)
	if err != nil {
		d.logger.Warn("mDNS discovery failed, falling back to static configuration", "error", err)
		return d.getStaticAPIEndpoint()
	}

	// Validate CA fingerprint if we have the CA certificate
	if err := d.validateCAFingerprint(ctx, service); err != nil {
		d.logger.Warn("CA fingerprint validation failed, falling back to static configuration", "error", err)
		return d.getStaticAPIEndpoint()
	}

	// Cache the discovery result
	d.discoveryMu.Lock()
	d.lastDiscoveredService = service
	d.lastDiscoveryTime = time.Now()
	d.discoveryMu.Unlock()

	endpoint := d.buildEndpointURL(service)
	d.logger.Info("mDNS discovery successful",
		"endpoint", endpoint,
		"cluster_id", service.ClusterID,
		"version", service.Version)

	return endpoint, nil
}

// InvalidateDiscoveryCache clears the cached discovery information
// Useful when a connection fails and we want to force a new discovery
func (d *DiscoveryClient) InvalidateDiscoveryCache() {
	d.discoveryMu.Lock()
	defer d.discoveryMu.Unlock()

	d.lastDiscoveredService = nil
	d.lastDiscoveryTime = time.Time{}
	d.logger.Debug("discovery cache invalidated")
}

// getStaticAPIEndpoint returns the statically configured API endpoint
func (d *DiscoveryClient) getStaticAPIEndpoint() (string, error) {
	baseURL, ok := d.config.Get("api.base_url")
	if !ok || baseURL == nil {
		return "", fmt.Errorf("api.base_url not configured and mDNS discovery failed")
	}

	return baseURL.(string), nil
}

// buildEndpointURL constructs the API endpoint URL from service info
func (d *DiscoveryClient) buildEndpointURL(service *mdns.ServiceInfo) string {
	// Use HTTPS for production, HTTP for local development
	scheme := "https"
	if val, ok := d.config.Get("api.use_https"); ok && val != nil {
		if useHTTPS, ok := val.(bool); ok && !useHTTPS {
			scheme = "http"
		}
	}

	// Use the mDNS hostname for TLS SNI to work correctly
	return fmt.Sprintf("%s://%s:%d/api/v1", scheme, mdns.APIHostname, service.Port)
}

// validateCAFingerprint validates that the discovered service's CA fingerprint
// matches the CA certificate we have
func (d *DiscoveryClient) validateCAFingerprint(ctx context.Context, service *mdns.ServiceInfo) error {
	if service.CAFingerprint == "" {
		d.logger.Warn("service does not provide CA fingerprint, skipping validation")
		return nil
	}

	// Fetch the CA certificate
	caCertPEM, err := d.apiClient.GetCACertificate(ctx)
	if err != nil {
		d.logger.Warn("failed to fetch CA certificate for fingerprint validation", "error", err)
		// Don't fail validation if we can't fetch the cert - it might not be critical
		return nil
	}

	// Calculate fingerprint
	hash := sha256.Sum256(caCertPEM)
	fingerprint := hex.EncodeToString(hash[:])

	if fingerprint != service.CAFingerprint {
		return fmt.Errorf("CA fingerprint mismatch: expected %s, got %s", service.CAFingerprint, fingerprint)
	}

	d.logger.Debug("CA fingerprint validated successfully", "fingerprint", fingerprint)
	return nil
}

// GetDiscoveredClusterInfo returns information about the discovered cluster
func (d *DiscoveryClient) GetDiscoveredClusterInfo() *mdns.ServiceInfo {
	d.discoveryMu.RLock()
	defer d.discoveryMu.RUnlock()

	if d.lastDiscoveredService == nil {
		return nil
	}

	// Return a copy to prevent external modification
	info := *d.lastDiscoveredService
	return &info
}
