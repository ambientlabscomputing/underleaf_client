# UMC Developer Guide

## Overview

This guide explains how to build UA-Managed Components (UMCs) using the UMC SDK. UMCs are userland subsystems supervised by the UA Kernel (UA-K) that extend edge node functionality.

## Philosophy

UA-K is the minimal trusted core running on every node. It:
- Changes slowly
- Remains minimal  
- Is security-critical
- Exposes stable primitives

Everything else runs as UA-Managed Components in userland.

## UMC Architecture

```
┌──────────────────────────────────────┐
│        UA Kernel (UA-K)              │
│  - Identity & Trust                  │
│  - Execution Authority               │
│  - Cluster State                     │
│  - Secrets Custody                   │
│  - Event Routing                     │
│  - Provider Lifecycle                │
└──────────┬───────────────────────────┘
           │ syscalls (gRPC/UDS)
       ┌───┴───┐
       │       │
    ┌──▼──┐ ┌─▼───┐
    │ MMA │ │ UMC │ ... (your component)
    └─────┘ └─────┘
```

## Communication Model

UMCs communicate with UA-K via:
- **Unix Domain Socket** (primary): `/tmp/ua_kernel.sock`
- **gRPC Protocol**: Defined in `umc_sdk/proto/ua_kernel/v1/syscall.proto`
- **Six Syscall Services**:
  1. `IdentityService` - Node identity & cryptographic operations
  2. `SecretService` - Secret storage & retrieval
  3. `ExecService` - Process execution
  4. `ClusterService` - Cluster state & KV store
  5. `EventService` - Event pub/sub
  6. `ProviderService` - Provider lifecycle management

## Building a UMC

### 1. Project Structure

```
my_umc/
├── cmd/
│   └── serve/
│       └── main.go          # Entry point
├── internal/
│   ├── config/              # Configuration
│   ├── server/              # HTTP API (optional)
│   └── worker/              # Business logic
├── go.mod
├── Makefile
└── README.md
```

### 2. Basic Template

```go
package main

import (
    "context"
    "log/slog"
    
    "github.com/ambientlabscomputing/umc_sdk/lifecycle"
    "github.com/ambientlabscomputing/umc_sdk/logging"
    "github.com/ambientlabscomputing/umc_sdk/syscall"
    "github.com/ambientlabscomputing/umc_sdk/transport"
)

func main() {
    // Setup logging
    logCfg := logging.DefaultConfig()
    logCfg.Output = "/var/log/my_umc/my_umc.log"
    logger, _ := logging.Setup(logCfg)
    
    // Connect to UA-K syscall server
    conn, _ := transport.UDSDialer("/tmp/ua_kernel.sock")
    defer conn.Close()
    
    // Create syscall clients
    identityClient := syscall.NewIdentityServiceClient(conn)
    eventClient := syscall.NewEventServiceClient(conn)
    
    // Create your components
    worker := NewWorker(identityClient, eventClient, logger)
    
    // Setup launcher with ordered initialization
    launcher := lifecycle.NewLauncher("my-umc", logger)
    launcher.Add(worker)
    
    // Run with signal handling
    runtime := lifecycle.NewRuntime(launcher)
    if err := runtime.Run(); err != nil {
        logger.Error("runtime error", "error", err)
    }
}
```

### 3. Implementing the Component Interface

```go
type Worker struct {
    identityClient syscall.IdentityServiceClient
    eventClient    syscall.EventServiceClient
    logger         *slog.Logger
}

func NewWorker(identityClient, eventClient, logger) *Worker {
    return &Worker{
        identityClient: identityClient,
        eventClient:    eventClient,
        logger:         logger,
    }
}

// Name implements lifecycle.Component
func (w *Worker) Name() string {
    return "worker"
}

// Start implements lifecycle.Component
func (w *Worker) Start(ctx context.Context) error {
    w.logger.Info("worker starting")
    
    // Get node identity from kernel
    identity, err := w.identityClient.GetNodeIdentity(ctx, &pb.GetNodeIdentityRequest{})
    if err != nil {
        return err
    }
    
    w.logger.Info("node identity", "node_id", identity.NodeId)
    
    // Subscribe to events
    go w.consumeEvents(ctx)
    
    return nil
}

// Stop implements lifecycle.Component
func (w *Worker) Stop(ctx context.Context) error {
    w.logger.Info("worker stopping")
    return nil
}

func (w *Worker) consumeEvents(ctx context.Context) {
    stream, _ := w.eventClient.SubscribeLocal(ctx, &pb.SubscribeLocalRequest{
        EventTypeFilter: []string{"deployment.created"},
    })
    
    for {
        event, err := stream.Recv()
        if err != nil {
            return
        }
        
        w.logger.Info("received event", "type", event.EventType)
        // Process event
    }
}
```

### 4. Using SDK Packages

#### Config Management

```go
import "github.com/ambientlabscomputing/umc_sdk/config"

type MyConfig struct {
    Port int
    Endpoint string
}

store := config.NewStore(MyConfig{Port: 8080})

// Thread-safe read
cfg := store.Get().(MyConfig)

// Thread-safe write
store.Set(MyConfig{Port: 9090})

// Atomic update
store.Update(func(v interface{}) interface{} {
    cfg := v.(MyConfig)
    cfg.Port++
    return cfg
})
```

#### Telemetry

```go
import "github.com/ambientlabscomputing/umc_sdk/telemetry"

// Create buffer
buffer := telemetry.NewBuffer(1000)

// Create flusher (implements Flusher interface)
flusher := &MyKernelFlusher{eventClient: eventClient}

// Create collector with 30s flush interval
collector := telemetry.NewCollector("my-umc", buffer, flusher, 30*time.Second, logger)
launcher.Add(collector)

// Record events
collector.Record("audit", "user.login", map[string]interface{}{
    "user_id": "123",
    "ip": "192.168.1.1",
})
```

#### Event Consumer

```go
import "github.com/ambientlabscomputing/umc_sdk/events"

consumer := events.NewConsumer(events.ConsumerConfig{
    Name: "deployment-consumer",
    DialFunc: func() (*grpc.ClientConn, error) {
        return transport.UDSDialer("/tmp/ua_kernel.sock")
    },
    StreamFunc: func(conn *grpc.ClientConn) (events.StreamClient, error) {
        client := syscall.NewEventServiceClient(conn)
        return client.SubscribeLocal(context.Background(), &pb.SubscribeLocalRequest{
            EventTypeFilter: []string{"deployment.*"},
        })
    },
    Handler: func(ctx context.Context, event interface{}) error {
        evt := event.(*pb.LocalEvent)
        logger.Info("event", "type", evt.EventType)
        return nil
    },
    Logger: logger,
})

launcher.Add(consumer)
```

## Available Syscalls

### IdentityService

```go
// Get node identity
resp, err := identityClient.GetNodeIdentity(ctx, &pb.GetNodeIdentityRequest{})
// resp.NodeId, resp.OrgId, resp.ClusterId, resp.PublicKeyPem

// Sign payload
sig, err := identityClient.SignPayload(ctx, &pb.SignPayloadRequest{
    Payload: []byte("data to sign"),
})
// sig.Signature, sig.Algorithm
```

### SecretService

```go
// Store secret
_, err := secretClient.StoreSecret(ctx, &pb.StoreSecretRequest{
    Key:   "my-secret",
    Value: []byte("secret-value"),
    Metadata: map[string]string{"owner": "my-umc"},
})

// Get secret
resp, err := secretClient.GetSecret(ctx, &pb.GetSecretRequest{
    Key: "my-secret",
})
// resp.Value
```

### ExecService

```go
// Run process
resp, err := execClient.RunProcess(ctx, &pb.RunProcessRequest{
    Command:        "/bin/echo",
    Args:           []string{"hello"},
    TimeoutSeconds: 30,
})
// resp.ExitCode, resp.Stdout, resp.Stderr
```

### ClusterService

```go
// Get cluster state
state, err := clusterClient.GetClusterState(ctx, &pb.GetClusterStateRequest{})
// state.ClusterId, state.LeaderId, state.Nodes, state.Health

// KV operations
value, err := clusterClient.GetKV(ctx, &pb.GetKVRequest{Key: "config"})
clusterClient.PutKV(ctx, &pb.PutKVRequest{Key: "config", Value: []byte("data")})
```

### EventService

```go
// Emit event
_, err := eventClient.EmitEvent(ctx, &pb.EmitEventRequest{
    EventType:  "my_umc.action.completed",
    EntityKind: "task",
    EntityId:   "task-123",
    Payload:    structpb.NewStructFromMap(map[string]interface{}{"status": "ok"}),
})

// Subscribe to events (server-streaming)
stream, err := eventClient.SubscribeLocal(ctx, &pb.SubscribeLocalRequest{
    EventTypeFilter: []string{"deployment.*", "provider.*"},
})

for {
    event, err := stream.Recv()
    // event.EventType, event.Payload
}
```

### ProviderService

```go
// Install provider
_, err := providerClient.InstallProvider(ctx, &pb.InstallProviderRequest{
    ProviderId:  "my-provider",
    Version:     "1.0.0",
    ArtifactUrl: "https://...",
    TrustTier:   pb.TrustTier_TRUST_TIER_TRUSTED,
})

// Start provider
_, err := providerClient.StartProvider(ctx, &pb.StartProviderRequest{
    ProviderId:   "my-provider",
    Version:      "1.0.0",
    Capabilities: []string{"compute.run"},
})
```

## Best Practices

### 1. Error Handling
Always check syscall errors and handle gracefully:

```go
resp, err := identityClient.GetNodeIdentity(ctx, &pb.GetNodeIdentityRequest{})
if err != nil {
    logger.Error("syscall failed", "error", err)
    return err
}
```

### 2. Context Propagation
Pass context through all calls for cancellation and timeouts:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
```

### 3. Graceful Shutdown
Implement proper Stop() cleanup:

```go
func (w *Worker) Stop(ctx context.Context) error {
    w.cancel() // Cancel background goroutines
    w.wg.Wait() // Wait for completion
    return nil
}
```

### 4. Event Idempotency
Handle duplicate events (reconnections cause replays):

```go
if w.processedEvents[event.EventId] {
    return nil // Skip duplicate
}
w.processedEvents[event.EventId] = true
```

### 5. Logging
Use structured logging with context:

```go
logger.Info("processing event",
    "event_type", event.EventType,
    "event_id", event.EventId,
)
```

## Deployment

UMCs are deployed and supervised by UA-K. Add your UMC to the node configuration:

```yaml
# config.yaml
managed_components:
  - name: my-umc
    binary_path: /usr/local/bin/my-umc
    health_endpoint: http://localhost:8080/health
    restart_policy:
      enabled: true
      max_restarts: 10
    granted_syscalls:
      - EventService
      - ClusterService
```

UA-K will:
1. Install the binary
2. Start the process
3. Monitor health
4. Restart on failure
5. Enforce syscall permissions

## Examples

See existing UMCs for reference:
- **MMA** (`mycelium_mesh_agent/`) - Service mesh runtime
- **Deployment Engine** (`deployment_engine/`) - Deployment orchestration
- **Metrics Collector** (`metrics_collector/`) - Observability

## Testing

Test your UMC with a mock kernel:

```go
import "google.golang.org/grpc/test/bufconn"

// Create in-memory connection
listener := bufconn.Listen(1024 * 1024)
server := grpc.NewServer()

// Register mock services
pb.RegisterIdentityServiceServer(server, &mockIdentityServer{})

go server.Serve(listener)

// Connect UMC to mock
conn, _ := grpc.DialContext(ctx, "",
    grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
        return listener.Dial()
    }),
    grpc.WithTransportCredentials(insecure.NewCredentials()),
)
```

## Support

- Architecture RFC: [UA_KERNELIZATION_SPEC.md](UA_KERNELIZATION_SPEC.md)
- Syscall Reference: [umc_sdk/proto/ua_kernel/v1/syscall.proto](../../../umc_sdk/proto/ua_kernel/v1/syscall.proto)
- SDK Documentation: [umc_sdk/README.md](../../../umc_sdk/README.md)
