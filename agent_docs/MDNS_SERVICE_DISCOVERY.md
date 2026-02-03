# mDNS Service Discovery

This document describes the mDNS (multicast DNS) service discovery feature for Underleaf, which enables automatic discovery of the cluster leader via `api.underleaf.local`.

## Overview

The mDNS feature provides zero-configuration service discovery for Underleaf clusters. When enabled, the Raft cluster leader automatically announces itself as `api.underleaf.local`, allowing clients to automatically discover and connect to the active leader without manual configuration.

## Architecture

### Components

1. **mDNS Server (Announcer)**: Runs on the Raft leader node and announces `_underleaf._tcp.local` service
2. **mDNS Client (Resolver)**: Discovers available services and validates them
3. **Coordinator**: Manages mDNS server lifecycle based on Raft leadership changes
4. **Discovery Client**: Integrates mDNS discovery with the API client, providing fallback to static configuration

### Service Details

- **Service Type**: `_underleaf._tcp.local`
- **Hostname**: `api.underleaf.local`
- **Protocol**: UDP multicast on port 5353 (standard mDNS port)
- **TTL**: 2 seconds (configurable) for fast leader failover
- **TXT Records**:
  - `cluster_id`: Unique identifier for the cluster
  - `ca_fingerprint`: SHA-256 fingerprint of the CA certificate
  - `version`: Semantic version of the service

### Leadership Integration

The mDNS announcements are tightly integrated with Raft leadership:

- **Only the leader announces**: When a node becomes the Raft leader, it starts mDNS announcements
- **Automatic failover**: When leadership changes, the old leader stops announcing and the new leader starts
- **Fast updates**: With a 2-second TTL, clients detect leadership changes within seconds

## Network Requirements

### Required Ports

- **UDP 5353**: mDNS multicast traffic (224.0.0.251 for IPv4)

### Firewall Configuration

For mDNS to function properly, the following must be allowed:

```bash
# Allow outbound multicast DNS
iptables -A OUTPUT -p udp --dport 5353 -d 224.0.0.251 -j ACCEPT

# Allow inbound multicast DNS
iptables -A INPUT -p udp --dport 5353 -d 224.0.0.251 -j ACCEPT
```

On macOS, mDNS traffic is typically unrestricted by default.

### Network Limitations

mDNS has inherent limitations:

1. **Local Network Only**: mDNS is designed for local network discovery and does not route across subnets by default
2. **Multicast Support**: The network must support multicast traffic
3. **Firewall Rules**: Corporate firewalls may block UDP port 5353 or multicast traffic

## Graceful Degradation

The system is designed to **gracefully degrade** when mDNS is unavailable:

### Automatic Fallback

When mDNS discovery fails (due to firewall, network restrictions, or service unavailability), the system automatically falls back to the static `api.base_url` configuration without errors or interruptions.

### Error Handling

- **Port 5353 Blocked**: If UDP 5353 is blocked, the mDNS server logs a warning and continues without starting. Clients fall back to static configuration.
- **No Service Found**: If no mDNS service is discovered within the timeout, clients use the static endpoint.
- **CA Fingerprint Mismatch**: If the discovered service has an incorrect CA fingerprint, clients fall back to static configuration for security.

### Logging

All mDNS failures are logged at the `WARN` level with clear messages:

```
mDNS server failed to start (UDP 5353 may be blocked), gracefully degrading
mDNS discovery failed, falling back to static configuration
```

This allows administrators to diagnose issues without disrupting service.

## Configuration

### Enable mDNS

In `config.yaml`:

```yaml
mdns:
  enabled: true
  port: 8080
  ttl: 2
  cluster_id: "my-prod-cluster"
```

### Client Discovery

Clients automatically use mDNS when enabled:

```yaml
mdns:
  enabled: true

api:
  base_url: http://localhost:8080/api/v1  # Fallback
  use_https: true  # Use HTTPS for discovered endpoints
```

## Usage

### Server-Side (Raft Leader)

The mDNS coordinator automatically manages announcements:

```go
import (
    "github.com/ambientlabscomputing/underleaf_client/internal/mdns"
    "github.com/ambientlabscomputing/underleaf_client/internal/raft"
)

// Create mDNS coordinator
mdnsConfig := &mdns.Config{
    Enabled:       true,
    Port:          8080,
    TTL:           2 * time.Second,
    ClusterID:     "my-cluster",
    CAFingerprint: mdns.ComputeCAFingerprint(caCertPEM),
    Version:       "1.0.0",
}

coordinator, err := mdns.NewCoordinator(mdnsConfig, logger)
if err != nil {
    log.Fatal(err)
}

// Register with Raft node
raftNode.RegisterLeaderChangeCallback(coordinator.OnLeadershipChange)
```

### Client-Side (Discovery)

The discovery client handles automatic endpoint resolution:

```go
import (
    "github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
)

// Create discovery client
discoveryClient := controlplane.NewDiscoveryClient(apiClient, config, logger)

// Discover endpoint (uses mDNS or falls back to static)
endpoint, err := discoveryClient.DiscoverAPIEndpoint(ctx)
if err != nil {
    log.Fatal(err)
}

// Use endpoint for API calls
// endpoint is either "https://api.underleaf.local:8080/api/v1" (mDNS)
// or the static "api.base_url" from config (fallback)
```

## Security Considerations

### CA Fingerprint Validation

The mDNS service includes a CA certificate fingerprint in its TXT records. Clients validate this fingerprint against the actual CA certificate fetched from the server to prevent man-in-the-middle attacks.

### mTLS Protection

All API endpoints are protected by mTLS using X.509 certificates signed by the cluster CA. This is independent of mDNS and provides strong authentication even when using mDNS for discovery.

### Certificate SANs

Certificates should include `api.underleaf.local` in their Subject Alternative Names when using mDNS. The CA in `server_api` already supports this - clients just need to request it in their CSR:

```go
csr := &x509.CertificateRequest{
    Subject: pkix.Name{
        CommonName: serverID,
    },
    DNSNames: []string{
        "api.underleaf.local",  // For mDNS
        "localhost",             // For local access
    },
}
```

## Monitoring

### Health Checks

Check if mDNS is functioning:

```bash
# Query mDNS service (requires avahi-browse or dns-sd)
avahi-browse -r _underleaf._tcp

# On macOS:
dns-sd -B _underleaf._tcp local
```

### Logs

Monitor mDNS activity in logs:

```bash
# Look for mDNS announcements
grep "mDNS server started" underleaf_agent.log

# Check for failures
grep "mDNS.*failed" underleaf_agent.log
```

### Metrics

The mDNS coordinator exposes metrics for monitoring:

- mDNS server running status
- Discovery success/failure rates
- Cached endpoint information
- Leadership transitions

## Troubleshooting

### Problem: mDNS discovery not working

**Symptoms**: Clients always fall back to static configuration

**Diagnosis**:
1. Check if mDNS is enabled in config
2. Verify UDP 5353 is not blocked by firewall
3. Ensure multicast is supported on the network
4. Check logs for "mDNS server started" on the leader

**Solution**:
```bash
# Test multicast connectivity
ping 224.0.0.251

# Check firewall rules
sudo iptables -L -n | grep 5353

# Enable mDNS traffic
sudo iptables -I INPUT -p udp --dport 5353 -j ACCEPT
sudo iptables -I OUTPUT -p udp --dport 5353 -j ACCEPT
```

### Problem: CA fingerprint mismatch

**Symptoms**: Discovery succeeds but falls back to static config

**Diagnosis**: Check logs for "CA fingerprint mismatch"

**Solution**: Ensure all nodes use the same CA certificate and that the `ca_fingerprint` in mDNS config matches the actual CA.

### Problem: Multiple clusters interfering

**Symptoms**: Wrong cluster being discovered

**Diagnosis**: Check `cluster_id` in TXT records

**Solution**: Ensure each cluster has a unique `cluster_id` in configuration.

## Best Practices

1. **Always configure static fallback**: Even with mDNS enabled, always set `api.base_url` as a fallback
2. **Use unique cluster IDs**: Avoid conflicts when running multiple clusters on the same network
3. **Monitor discovery**: Track discovery success rates and failover times
4. **Test failover**: Regularly test leader failover to ensure mDNS updates correctly
5. **Document firewall rules**: Ensure operations teams know UDP 5353 must be allowed

## Performance

- **Discovery latency**: 1-3 seconds on initial discovery
- **Failover time**: 2-4 seconds (depends on TTL and client cache)
- **Network overhead**: Minimal (~100 bytes/announcement, every 2 seconds for leader only)
- **CPU overhead**: Negligible (<0.1% CPU)

## Future Enhancements

Potential improvements for future versions:

1. **DNS-SD via unicast DNS**: Fallback to unicast DNS-SD for networks without multicast
2. **Service browsing UI**: Web UI to show discovered services
3. **Health status in TXT records**: Include cluster health metrics in announcements
4. **Multi-region support**: Extend beyond local networks using DNS-based service discovery
