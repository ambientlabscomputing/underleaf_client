package mdns

import (
	"time"
)

const (
	// ServiceName is the mDNS service type for Underleaf API discovery
	ServiceName = "_underleaf._tcp"
	// ServiceDomain is the mDNS domain for service discovery (must end with period for FQDN)
	ServiceDomain = "local."
	// APIHostname is the hostname that will resolve to the cluster leader
	APIHostname = "api.underleaf.local"
	// DefaultTTL is the default TTL for mDNS records (2 seconds for fast failover)
	DefaultTTL = 2 * time.Second
	// DefaultPort is the default port for the Underleaf API
	DefaultPort = 8080
)

// ServiceInfo contains metadata about a discovered Underleaf API service
type ServiceInfo struct {
	// Hostname is the hostname of the service (api.underleaf.local for leader, {nodeID}.underleaf.local for specific nodes)
	Hostname string
	// IPAddr is the IPv4 address of the service
	IPAddr string
	// Port is the TCP port of the service
	Port int
	// ClusterID is the unique identifier for the cluster
	ClusterID string
	// CAFingerprint is the SHA-256 fingerprint of the CA certificate
	CAFingerprint string
	// Version is the semantic version of the service
	Version string
	// DiscoveredAt is the timestamp when this service was discovered
	DiscoveredAt time.Time
}

// Config contains configuration for mDNS service
type Config struct {
	// Enabled determines if mDNS is enabled
	Enabled bool
	// NodeID is the unique identifier for this node (used for {nodeID}.underleaf.local)
	NodeID string
	// Port is the port where the API is served
	Port int
	// TTL is the time-to-live for mDNS records
	TTL time.Duration
	// Interface is the network interface to bind to (empty for all interfaces)
	Interface string
	// ClusterID is the unique identifier for this cluster
	ClusterID string
	// CAFingerprint is the SHA-256 fingerprint of the CA certificate
	CAFingerprint string
	// Version is the semantic version of the service
	Version string
}

// DefaultConfig returns a default mDNS configuration
func DefaultConfig() *Config {
	return &Config{
		Enabled:   false,
		Port:      DefaultPort,
		TTL:       DefaultTTL,
		Interface: "",
	}
}
