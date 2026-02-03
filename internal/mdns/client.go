package mdns

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/hashicorp/mdns"
)

// Client is an mDNS client that discovers Underleaf API services
type Client struct {
	logger *slog.Logger
}

// NewClient creates a new mDNS client
func NewClient(logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		logger: logger,
	}
}

// Discover searches for Underleaf API services via mDNS
// Returns the first service found or an error if none found within timeout
func (c *Client) Discover(ctx context.Context, timeout time.Duration) (*ServiceInfo, error) {
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Channel to receive discovered services
	entries := make(chan *mdns.ServiceEntry, 10)

	// Start the query
	params := &mdns.QueryParam{
		Service:             ServiceName,
		Domain:              ServiceDomain,
		Timeout:             timeout,
		Entries:             entries,
		DisableIPv6:         true, // Only use IPv4
		WantUnicastResponse: false,
	}

	// Run the query in a goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := mdns.Query(params); err != nil {
			// Check if this is a network error (UDP 5353 blocked)
			if isNetworkError(err) {
				c.logger.Warn("mDNS query failed (UDP 5353 may be blocked), gracefully degrading", "error", err)
				errChan <- fmt.Errorf("network error during mDNS query (UDP 5353 may be unavailable): %w", err)
			} else {
				errChan <- fmt.Errorf("mDNS query failed: %w", err)
			}
		}
		close(errChan)
	}()

	// Wait for first result or timeout
	select {
	case entry := <-entries:
		if entry == nil {
			return nil, fmt.Errorf("received nil mDNS entry")
		}
		service := c.parseServiceEntry(entry)
		c.logger.Info("Discovered Underleaf API service via mDNS",
			"hostname", service.Hostname,
			"ip", service.IPAddr,
			"port", service.Port,
			"cluster_id", service.ClusterID,
			"ca_fingerprint", service.CAFingerprint)
		return service, nil
	case err := <-errChan:
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("no Underleaf API service found via mDNS")
	case <-ctx.Done():
		return nil, fmt.Errorf("mDNS discovery timeout after %v", timeout)
	}
}

// DiscoverAll searches for all Underleaf API services via mDNS
// Returns all services found within the timeout period
func (c *Client) DiscoverAll(ctx context.Context, timeout time.Duration) ([]*ServiceInfo, error) {
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Channel to receive discovered services
	entries := make(chan *mdns.ServiceEntry, 100)

	// Start the query
	params := &mdns.QueryParam{
		Service:             ServiceName,
		Domain:              ServiceDomain,
		Timeout:             timeout,
		Entries:             entries,
		DisableIPv6:         true, // Only use IPv4
		WantUnicastResponse: false,
	}

	// Run the query in a goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := mdns.Query(params); err != nil {
			if isNetworkError(err) {
				c.logger.Warn("mDNS query failed (UDP 5353 may be blocked), gracefully degrading", "error", err)
				errChan <- fmt.Errorf("network error during mDNS query (UDP 5353 may be unavailable): %w", err)
			} else {
				errChan <- fmt.Errorf("mDNS query failed: %w", err)
			}
		}
		close(errChan)
	}()

	// Collect all results until timeout
	var services []*ServiceInfo
	for {
		select {
		case entry := <-entries:
			if entry == nil {
				continue
			}
			services = append(services, c.parseServiceEntry(entry))
		case err := <-errChan:
			if err != nil {
				return nil, err
			}
			if len(services) == 0 {
				return nil, fmt.Errorf("no Underleaf API services found via mDNS")
			}
			return services, nil
		case <-ctx.Done():
			if len(services) == 0 {
				return nil, fmt.Errorf("mDNS discovery timeout after %v, no services found", timeout)
			}
			c.logger.Info("mDNS discovery complete", "count", len(services))
			return services, nil
		}
	}
}

// parseServiceEntry converts an mDNS service entry to ServiceInfo
func (c *Client) parseServiceEntry(entry *mdns.ServiceEntry) *ServiceInfo {
	service := &ServiceInfo{
		Hostname:     entry.Name,
		Port:         entry.Port,
		DiscoveredAt: time.Now(),
	}

	// Get the first IPv4 address
	if entry.AddrV4 != nil {
		service.IPAddr = entry.AddrV4.String()
	}

	// Parse TXT records
	for _, txt := range entry.InfoFields {
		if strings.HasPrefix(txt, "cluster_id=") {
			service.ClusterID = strings.TrimPrefix(txt, "cluster_id=")
		} else if strings.HasPrefix(txt, "ca_fingerprint=") {
			service.CAFingerprint = strings.TrimPrefix(txt, "ca_fingerprint=")
		} else if strings.HasPrefix(txt, "version=") {
			service.Version = strings.TrimPrefix(txt, "version=")
		}
	}

	return service
}

// isNetworkError checks if an error is related to network/permission issues
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	// Check for common network permission errors
	errStr := err.Error()
	return strings.Contains(errStr, "permission denied") ||
		strings.Contains(errStr, "bind") ||
		strings.Contains(errStr, "address already in use") ||
		strings.Contains(errStr, "network is unreachable")
}
