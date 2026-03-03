package agent

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"

	"google.golang.org/grpc"

	"github.com/ambientlabscomputing/underleaf_client/internal/eventstream"
	pb "github.com/ambientlabscomputing/underleaf_client/proto/ua_mma/v1"
)

// EventStreamServer manages the UA→MMA event stream gRPC server.
type EventStreamServer struct {
	server    *eventstream.Server
	grpcSrv   *grpc.Server
	listener  net.Listener
	logger    *slog.Logger
	address   string // Unix socket path or TCP address
	clusterID string
	nodeID    string
}

// NewEventStreamServer creates a new UA event stream server.
// address can be a Unix socket path (e.g., "/tmp/ua_mma.sock") or TCP address (e.g., "localhost:50051").
func NewEventStreamServer(address, clusterID, nodeID string, logger *slog.Logger) *EventStreamServer {
	if logger == nil {
		logger = slog.Default()
	}

	// Create the event stream implementation
	streamServer := eventstream.NewServer(clusterID, nodeID, logger)

	return &EventStreamServer{
		server:    streamServer,
		logger:    logger,
		address:   address,
		clusterID: clusterID,
		nodeID:    nodeID,
	}
}

// Start starts the gRPC server and begins accepting connections.
func (e *EventStreamServer) Start(ctx context.Context) error {
	var listener net.Listener
	var err error

	// Determine if we're using Unix socket or TCP
	if filepath.IsAbs(e.address) || e.address[0] == '/' {
		// Unix domain socket
		// Clean up existing socket file if it exists
		if err := os.RemoveAll(e.address); err != nil {
			return fmt.Errorf("failed to remove existing socket: %w", err)
		}

		listener, err = net.Listen("unix", e.address)
		if err != nil {
			return fmt.Errorf("failed to listen on unix socket %s: %w", e.address, err)
		}

		// Set socket permissions to allow MMA to connect
		if err := os.Chmod(e.address, 0600); err != nil {
			listener.Close()
			return fmt.Errorf("failed to set socket permissions: %w", err)
		}

		e.logger.Info("UA event stream listening on Unix socket", "path", e.address)
	} else {
		// TCP socket
		listener, err = net.Listen("tcp", e.address)
		if err != nil {
			return fmt.Errorf("failed to listen on tcp %s: %w", e.address, err)
		}

		e.logger.Info("UA event stream listening on TCP", "address", e.address)
	}

	e.listener = listener

	// Create gRPC server
	// TODO: Add TLS credentials when needed
	e.grpcSrv = grpc.NewServer()

	// Register the event stream service
	pb.RegisterUAEventStreamServiceServer(e.grpcSrv, e.server)

	// Start serving in background
	go func() {
		if err := e.grpcSrv.Serve(listener); err != nil {
			e.logger.Error("event stream gRPC server error", "error", err)
		}
	}()

	e.logger.Info("UA event stream server started", "cluster_id", e.clusterID, "node_id", e.nodeID)
	return nil
}

// Stop gracefully stops the gRPC server.
func (e *EventStreamServer) Stop() error {
	e.logger.Info("stopping UA event stream server")

	if e.grpcSrv != nil {
		e.grpcSrv.GracefulStop()
	}

	if e.listener != nil {
		e.listener.Close()
	}

	// Clean up Unix socket file if applicable
	if filepath.IsAbs(e.address) || e.address[0] == '/' {
		os.Remove(e.address)
	}

	e.logger.Info("UA event stream server stopped")
	return nil
}

// PublishEvent publishes an event to all connected MMA subscribers.
func (e *EventStreamServer) PublishEvent(eventType string, payload map[string]interface{}, entityKind, entityID string) error {
	return e.server.PublishEvent(eventType, payload, entityKind, entityID)
}

// SubscriberCount returns the number of active MMA subscribers.
func (e *EventStreamServer) SubscriberCount() int {
	return e.server.SubscriberCount()
}

// BufferStats returns ring buffer statistics.
func (e *EventStreamServer) BufferStats() (size, capacity int) {
	return e.server.BufferStats()
}

// PublishExposureBindRequested emits an exposure.bind.requested UA event to MMA.
// Called when the agent receives an exposure.bind.request from Spine.
func (e *EventStreamServer) PublishExposureBindRequested(
	exposureID string,
	hostname string,
	targetPort int,
	localAddr string,
) error {
	payload := map[string]interface{}{
		"exposure_id": exposureID,
		"hostname":    hostname,
		"target_port": targetPort,
		"local_addr":  localAddr,
	}
	return e.PublishEvent("exposure.bind.requested", payload, "exposure", exposureID)
}

// PublishExposureUnbindRequested emits an exposure.unbind.requested UA event to MMA.
// Called when the agent receives an exposure.unbind.request from Spine.
func (e *EventStreamServer) PublishExposureUnbindRequested(exposureID string) error {
	payload := map[string]interface{}{
		"exposure_id": exposureID,
	}
	return e.PublishEvent("exposure.unbind.requested", payload, "exposure", exposureID)
}
