package mdns

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/hashicorp/mdns"
)

// Server is an mDNS responder that announces the Underleaf API service
type Server struct {
	config        *Config
	nodeServer    *mdns.Server // Always announces {nodeID}.underleaf.local
	leaderServer  *mdns.Server // Only announces api.underleaf.local when leader
	logger        *slog.Logger
	mu            sync.RWMutex
	nodeRunning   bool
	leaderRunning bool
}

// NewServer creates a new mDNS server
func NewServer(config *Config, logger *slog.Logger) (*Server, error) {
	if config == nil {
		config = DefaultConfig()
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &Server{
		config:        config,
		logger:        logger,
		nodeRunning:   false,
		leaderRunning: false,
	}, nil
}

// Start begins announcing the service via mDNS
// This should only be called when the node is the cluster leader
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.config.Enabled {
		s.logger.Info("mDNS server is disabled")
		return nil
	}

	if s.leaderRunning {
		s.logger.Warn("mDNS leader announcement is already running")
		return nil
	}

	// Get the primary IPv4 address
	ipAddr, err := s.getPrimaryIPv4()
	if err != nil {
		s.logger.Warn("Failed to get primary IPv4 address, mDNS will gracefully degrade", "error", err)
		return fmt.Errorf("failed to get primary IPv4: %w", err)
	}

	// Create leader service instance (api.underleaf.local)
	service, err := s.createService(ipAddr, APIHostname)
	if err != nil {
		return fmt.Errorf("failed to create mDNS leader service: %w", err)
	}

	// Start mDNS server for leader announcement
	server, err := mdns.NewServer(&mdns.Config{Zone: service})
	if err != nil {
		// Check if this is a network permission error (UDP 5353 blocked)
		if isNetworkError(err) {
			s.logger.Warn("Failed to start mDNS leader server (UDP 5353 may be blocked), gracefully degrading", "error", err)
			return fmt.Errorf("network error starting mDNS (UDP 5353 may be unavailable): %w", err)
		}
		return fmt.Errorf("failed to start mDNS leader server: %w", err)
	}

	s.leaderServer = server
	s.leaderRunning = true
	s.logger.Info("mDNS leader announcement started",
		"hostname", APIHostname,
		"ip", ipAddr,
		"port", s.config.Port,
		"cluster_id", s.config.ClusterID,
		"ca_fingerprint", s.config.CAFingerprint)

	// Monitor context cancellation
	go func() {
		<-ctx.Done()
		if err := s.Stop(); err != nil {
			s.logger.Error("Error stopping mDNS leader server", "error", err)
		}
	}()

	return nil
}

// Stop stops the mDNS server
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.leaderRunning {
		return nil
	}

	if s.leaderServer != nil {
		if err := s.leaderServer.Shutdown(); err != nil {
			return fmt.Errorf("failed to shutdown mDNS leader server: %w", err)
		}
	}

	s.leaderRunning = false
	s.logger.Info("mDNS leader announcement stopped")
	return nil
}

// StartNode starts the node-specific announcement ({nodeID}.underleaf.local)
// This should always be running regardless of leadership status
func (s *Server) StartNode(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info("DEBUG: StartNode() called", "enabled", s.config.Enabled, "node_id", s.config.NodeID, "already_running", s.nodeRunning)

	if !s.config.Enabled {
		s.logger.Info("mDNS server is disabled")
		return nil
	}

	if s.nodeRunning {
		s.logger.Warn("mDNS node announcement is already running")
		return nil
	}

	if s.config.NodeID == "" {
		s.logger.Warn("NodeID is not configured, skipping node-specific mDNS announcement")
		return nil
	}

	// Get the primary IPv4 address
	ipAddr, err := s.getPrimaryIPv4()
	if err != nil {
		s.logger.Warn("Failed to get primary IPv4 address, mDNS will gracefully degrade", "error", err)
		return fmt.Errorf("failed to get primary IPv4: %w", err)
	}

	// Create node-specific service instance ({nodeID}.underleaf.local)
	nodeHostname := fmt.Sprintf("%s.underleaf.local", s.config.NodeID)
	s.logger.Info("DEBUG: Creating mDNS service", "hostname", nodeHostname, "ip", ipAddr, "port", s.config.Port)
	service, err := s.createService(ipAddr, nodeHostname)
	if err != nil {
		return fmt.Errorf("failed to create mDNS node service: %w", err)
	}

	// Start mDNS server for node announcement
	s.logger.Info("DEBUG: Calling mdns.NewServer() to start node announcement")
	server, err := mdns.NewServer(&mdns.Config{Zone: service})
	if err != nil {
		// Check if this is a network permission error (UDP 5353 blocked)
		if isNetworkError(err) {
			s.logger.Warn("Failed to start mDNS node server (UDP 5353 may be blocked), gracefully degrading", "error", err)
			return fmt.Errorf("network error starting mDNS (UDP 5353 may be unavailable): %w", err)
		}
		return fmt.Errorf("failed to start mDNS node server: %w", err)
	}

	s.nodeServer = server
	s.nodeRunning = true
	s.logger.Info("mDNS node announcement started",
		"hostname", nodeHostname,
		"node_id", s.config.NodeID,
		"ip", ipAddr,
		"port", s.config.Port,
		"cluster_id", s.config.ClusterID)

	// Monitor context cancellation
	go func() {
		<-ctx.Done()
		if err := s.StopNode(); err != nil {
			s.logger.Error("Error stopping mDNS node server", "error", err)
		}
	}()

	return nil
}

// StopNode stops the node-specific mDNS announcement
func (s *Server) StopNode() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.nodeRunning {
		return nil
	}

	if s.nodeServer != nil {
		if err := s.nodeServer.Shutdown(); err != nil {
			return fmt.Errorf("failed to shutdown mDNS node server: %w", err)
		}
	}

	s.nodeRunning = false
	s.logger.Info("mDNS node announcement stopped")
	return nil
}

// IsRunning returns true if the server is currently running
func (s *Server) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.leaderRunning
}

// IsNodeRunning returns true if the node announcement is currently running
func (s *Server) IsNodeRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.nodeRunning
}

// createService creates an mDNS service instance
func (s *Server) createService(ipAddr string, hostname string) (mdns.Zone, error) {
	// Create TXT records with service metadata
	txtRecords := []string{
		fmt.Sprintf("cluster_id=%s", s.config.ClusterID),
		fmt.Sprintf("ca_fingerprint=%s", s.config.CAFingerprint),
		fmt.Sprintf("version=%s", s.config.Version),
	}

	// Create service
	service, err := mdns.NewMDNSService(
		hostname,                      // Instance name (use provided hostname parameter)
		ServiceName,                   // Service type
		ServiceDomain,                 // Domain
		"",                            // Host name (empty = use instance name)
		s.config.Port,                 // Port
		[]net.IP{net.ParseIP(ipAddr)}, // IPs
		txtRecords,                    // TXT records
	)
	if err != nil {
		return nil, err
	}

	return service, nil
}

// getPrimaryIPv4 returns the primary IPv4 address of this host
func (s *Server) getPrimaryIPv4() (string, error) {
	// If a specific interface is configured, use it
	if s.config.Interface != "" {
		iface, err := net.InterfaceByName(s.config.Interface)
		if err != nil {
			return "", fmt.Errorf("interface %s not found: %w", s.config.Interface, err)
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return "", fmt.Errorf("failed to get addresses for interface %s: %w", s.config.Interface, err)
		}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				return ipnet.IP.String(), nil
			}
		}
		return "", fmt.Errorf("no IPv4 address found on interface %s", s.config.Interface)
	}

	// Otherwise, get all interfaces and find the first non-loopback IPv4 address
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("failed to get network interfaces: %w", err)
	}

	for _, iface := range ifaces {
		// Skip loopback and down interfaces
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				return ipnet.IP.String(), nil
			}
		}
	}

	return "", fmt.Errorf("no suitable IPv4 address found")
}
