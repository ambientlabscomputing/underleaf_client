# mDNS Integration Guide

Quick guide for integrating mDNS service discovery into your Underleaf deployment.

## Quick Start

### 1. Enable mDNS in Configuration

Edit your `config.yaml`:

```yaml
mdns:
  enabled: true
  port: 8080
  ttl: 2
  cluster_id: "my-cluster-name"  # Must be unique per cluster

api:
  base_url: http://localhost:8080/api/v1  # Fallback
  use_https: true
```

### 2. Server-Side Integration (Agent with Raft)

```go
package main

import (
    "context"
    "github.com/ambientlabscomputing/underleaf_client/internal/mdns"
    "github.com/ambientlabscomputing/underleaf_client/internal/raft"
    "github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
)

func main() {
    // Initialize your components
    logger := // ... your logger
    config := // ... your config
    apiClient := controlplane.NewAPIClient(config, nil)
    
    // Fetch CA certificate
    ctx := context.Background()
    caCertPEM, err := apiClient.GetCACertificate(ctx)
    if err != nil {
        logger.Error("failed to fetch CA cert", "error", err)
        return
    }
    
    // Create mDNS coordinator
    mdnsConfig := &mdns.Config{
        Enabled:       true,
        Port:          8080,
        TTL:           2 * time.Second,
        ClusterID:     config.Get("mdns.cluster_id").(string),
        CAFingerprint: mdns.ComputeCAFingerprint(caCertPEM),
        Version:       "1.0.0",
    }
    
    coordinator, err := mdns.NewCoordinator(mdnsConfig, logger)
    if err != nil {
        logger.Error("failed to create coordinator", "error", err)
        return
    }
    defer coordinator.Stop()
    
    // Create Raft node
    raftNode, err := raft.NewNode(raftConfig, logger)
    if err != nil {
        logger.Error("failed to create raft node", "error", err)
        return
    }
    defer raftNode.Stop()
    
    // Connect mDNS to Raft leadership
    raftNode.RegisterLeaderChangeCallback(coordinator.OnLeadershipChange)
    
    // Start Raft (mDNS starts automatically on leadership)
    if err := raftNode.Start(); err != nil {
        logger.Error("failed to start raft", "error", err)
        return
    }
    
    // Your application logic here...
}
```

### 3. Client-Side Integration (Discovery Only)

```go
package main

import (
    "context"
    "github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
)

func main() {
    // Initialize your components
    logger := // ... your logger
    config := // ... your config
    
    // Create API client
    apiClient := controlplane.NewAPIClient(config, nil)
    
    // Create discovery client
    discoveryClient := controlplane.NewDiscoveryClient(apiClient, config, logger)
    
    // Discover API endpoint (automatic fallback on failure)
    ctx := context.Background()
    endpoint, err := discoveryClient.DiscoverAPIEndpoint(ctx)
    if err != nil {
        logger.Error("discovery failed", "error", err)
        return
    }
    
    logger.Info("using endpoint", "endpoint", endpoint)
    
    // Make API calls using the discovered endpoint
    // The endpoint is either:
    // - "https://api.underleaf.local:8080/api/v1" (mDNS success)
    // - Your static api.base_url (mDNS fallback)
}
```

## Common Integration Patterns

### Pattern 1: Agent with Raft Cluster

**Use Case**: Edge agents forming a distributed cluster

```go
// Main agent initialization
func startAgent() {
    // 1. Load config
    config := loadConfig()
    
    // 2. Initialize Raft
    raftNode := initializeRaft(config)
    
    // 3. Initialize mDNS
    mdnsCoordinator := initializeMDNS(config, raftNode)
    
    // 4. Start services
    raftNode.Start()
    
    // mDNS automatically starts when this node becomes leader
}
```

### Pattern 2: Client-Only Discovery

**Use Case**: Client applications discovering the cluster leader

```go
// Client initialization
func startClient() {
    // 1. Load config
    config := loadConfig()
    
    // 2. Create discovery client
    discoveryClient := initializeDiscovery(config)
    
    // 3. Discover endpoint
    endpoint := discoverEndpoint(discoveryClient)
    
    // 4. Use endpoint for API calls
    makeAPICall(endpoint)
}
```

### Pattern 3: Hybrid with Failover

**Use Case**: Client that retries discovery on connection failure

```go
func makeAPICallWithRetry(discoveryClient *controlplane.DiscoveryClient) error {
    ctx := context.Background()
    
    for attempt := 0; attempt < 3; attempt++ {
        // Discover endpoint
        endpoint, err := discoveryClient.DiscoverAPIEndpoint(ctx)
        if err != nil {
            return err
        }
        
        // Try API call
        err = makeAPICall(endpoint)
        if err == nil {
            return nil // Success
        }
        
        // On connection failure, invalidate cache and retry
        logger.Warn("API call failed, invalidating cache and retrying",
            "attempt", attempt, "error", err)
        discoveryClient.InvalidateDiscoveryCache()
        
        time.Sleep(2 * time.Second)
    }
    
    return errors.New("all retry attempts failed")
}
```

## Configuration Examples

### Development Environment

```yaml
mdns:
  enabled: true
  port: 8080
  ttl: 2
  cluster_id: "dev-local"

api:
  base_url: http://localhost:8080/api/v1
  use_https: false  # HTTP for local development

raft:
  enabled: true
  node_id: node1
  bind_addr: 0.0.0.0:7946
  bootstrap: true
```

### Production Environment

```yaml
mdns:
  enabled: true
  port: 8080
  ttl: 2
  cluster_id: "prod-cluster-01"
  interface: "eth0"  # Specific interface for mDNS

api:
  base_url: https://api.underleaf.example.com/api/v1
  use_https: true  # Always HTTPS in production

raft:
  enabled: true
  node_id: node1
  bind_addr: 10.0.1.10:7946
  bootstrap: false
  bootstrap_peers:
    - "node2@10.0.1.11:7946"
    - "node3@10.0.1.12:7946"
```

### Firewall-Restricted Environment

```yaml
mdns:
  enabled: false  # Disable mDNS if UDP 5353 is blocked

api:
  base_url: https://api.underleaf.example.com/api/v1  # Must use static config
  use_https: true
```

## Troubleshooting Integration

### Problem: mDNS not starting

**Check**:
```go
// In your code, add logging
logger.Info("mDNS config", 
    "enabled", config.Enabled,
    "port", config.Port,
    "cluster_id", config.ClusterID)

if coordinator.IsRunning() {
    logger.Info("mDNS is running")
} else {
    logger.Warn("mDNS is not running")
}
```

### Problem: Discovery not working

**Check**:
```go
// Test discovery manually
ctx := context.Background()
endpoint, err := discoveryClient.DiscoverAPIEndpoint(ctx)
if err != nil {
    logger.Error("discovery failed", "error", err)
} else {
    logger.Info("discovered", "endpoint", endpoint)
}

// Check cluster info
if info := discoveryClient.GetDiscoveredClusterInfo(); info != nil {
    logger.Info("cluster info",
        "ip", info.IPAddr,
        "cluster_id", info.ClusterID)
}
```

### Problem: Leadership not triggering mDNS

**Check**:
```go
// Verify callback is registered
logger.Info("registering mDNS callback")
raftNode.RegisterLeaderChangeCallback(func(isLeader bool) {
    logger.Info("leadership changed", "is_leader", isLeader)
    coordinator.OnLeadershipChange(isLeader)
})

// Check Raft state
logger.Info("raft state",
    "is_leader", raftNode.IsLeader(),
    "role", raftNode.GetRole())
```

## Dependencies

Add to your `go.mod`:

```go
require (
    github.com/hashicorp/mdns v1.0.5
    github.com/hashicorp/raft v1.5.0
    github.com/sirupsen/logrus v1.9.3
)
```

## Testing

### Unit Tests

```go
func TestMDNSIntegration(t *testing.T) {
    config := &mdns.Config{
        Enabled:   true,
        Port:      8080,
        ClusterID: "test-cluster",
    }
    
    coordinator, err := mdns.NewCoordinator(config, nil)
    require.NoError(t, err)
    defer coordinator.Stop()
    
    // Simulate leadership acquisition
    coordinator.OnLeadershipChange(true)
    assert.True(t, coordinator.IsRunning())
    
    // Simulate leadership loss
    coordinator.OnLeadershipChange(false)
    assert.False(t, coordinator.IsRunning())
}
```

### Integration Tests

```go
func TestEndToEndDiscovery(t *testing.T) {
    // Start server with mDNS
    coordinator := startMDNSServer(t)
    defer coordinator.Stop()
    
    // Wait for announcement
    time.Sleep(1 * time.Second)
    
    // Client discovers
    client := mdns.NewClient(nil)
    ctx := context.Background()
    service, err := client.Discover(ctx, 5*time.Second)
    
    require.NoError(t, err)
    assert.Equal(t, "api.underleaf.local", service.Hostname)
    assert.Equal(t, 8080, service.Port)
}
```

## Best Practices

1. **Always configure static fallback**: Never rely solely on mDNS
2. **Use unique cluster IDs**: Prevent cross-cluster interference
3. **Monitor leadership changes**: Log all mDNS state transitions
4. **Invalidate cache on failure**: Force rediscovery after connection errors
5. **Test firewall scenarios**: Ensure graceful degradation works

## Performance Tips

1. **Cache discovery results**: Use 5-second cache to reduce queries
2. **Tune TTL carefully**: Balance failover speed vs network overhead
3. **Limit network interfaces**: Specify `interface` in config if possible
4. **Monitor discovery latency**: Track time from query to result

## Security Checklist

- [ ] CA certificate is fetched securely from server
- [ ] CA fingerprint is validated in mDNS TXT records
- [ ] All API endpoints use mTLS authentication
- [ ] Certificates include `api.underleaf.local` in SANs
- [ ] JWT tokens are not logged
- [ ] mDNS cluster_id is unique per cluster

## Additional Resources

- [MDNS_ARCHITECTURE.md](./MDNS_ARCHITECTURE.md) - Detailed architecture
- [MDNS_SERVICE_DISCOVERY.md](./MDNS_SERVICE_DISCOVERY.md) - Full documentation
- [Example code](../internal/mdns/example_test.go) - Working examples
