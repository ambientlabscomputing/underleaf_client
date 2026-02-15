package kernel

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"
)

// PeerCredentials holds the Unix process credentials from SO_PEERCRED
type PeerCredentials struct {
	PID int // Process ID
	UID int // User ID
	GID int // Group ID
}

// getPeerCredentials extracts SO_PEERCRED from a Unix socket connection
func getPeerCredentials(ctx context.Context) (*PeerCredentials, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("no peer found in context")
	}

	// Check if the peer is a Unix socket connection
	_, ok = p.Addr.(*net.UnixAddr)
	if !ok {
		return nil, fmt.Errorf("peer is not a Unix socket connection")
	}

	// Note: gRPC does not expose the underlying net.Conn directly.
	// The extractRawConnCredentials function in platform-specific files
	// (auth_linux.go, auth_darwin.go) shows how to extract credentials
	// when you have access to the raw connection.
	//
	// For a complete implementation, you would need to:
	// 1. Access the gRPC transport internals to get the net.Conn
	// 2. Call extractRawConnCredentials with that connection
	//
	// For now, return an error indicating this needs full implementation.
	// Currently, interceptors will log this error but still allow the call.

	return nil, fmt.Errorf("credential extraction requires access to raw connection (not exposed by gRPC peer API)")
}

// UnaryAuthInterceptor creates a gRPC interceptor that validates peer credentials
func UnaryAuthInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		// Extract peer credentials
		creds, err := getPeerCredentials(ctx)
		if err != nil {
			logger.Warn("failed to extract peer credentials",
				"method", info.FullMethod,
				"error", err,
			)
			// For now, allow the call to proceed
			// In strict mode, would return: status.Error(codes.Unauthenticated, "failed to authenticate peer")
		} else {
			logger.Debug("peer authenticated",
				"method", info.FullMethod,
				"pid", creds.PID,
				"uid", creds.UID,
			)

			// Check method-specific ACLs
			if err := validateMethodAccess(creds, info.FullMethod); err != nil {
				logger.Warn("method access denied",
					"method", info.FullMethod,
					"pid", creds.PID,
					"uid", creds.UID,
					"error", err,
				)
				// For now, log but allow - strict enforcement would return error
			}
		}

		// Call the actual handler
		return handler(ctx, req)
	}
}

// StreamAuthInterceptor creates a gRPC interceptor for streaming RPCs
func StreamAuthInterceptor(logger *slog.Logger) grpc.StreamServerInterceptor {
	return func(
		srv interface{},
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		// Extract peer credentials
		ctx := ss.Context()
		creds, err := getPeerCredentials(ctx)
		if err != nil {
			logger.Warn("failed to extract peer credentials",
				"method", info.FullMethod,
				"error", err,
			)
			// For now, allow the call to proceed
			// In strict mode, would return: status.Error(codes.Unauthenticated, "failed to authenticate peer")
		} else {
			logger.Debug("peer authenticated for stream",
				"method", info.FullMethod,
				"pid", creds.PID,
				"uid", creds.UID,
			)

			// Check method-specific ACLs
			if err := validateMethodAccess(creds, info.FullMethod); err != nil {
				logger.Warn("stream method access denied",
					"method", info.FullMethod,
					"pid", creds.PID,
					"uid", creds.UID,
					"error", err,
				)
				// For now, log but allow - strict enforcement would return error
			}
		}

		// Call the actual handler
		return handler(srv, ss)
	}
}

// validateMethodAccess checks if the peer has access to the given method
// Returns an error if access should be denied.
//
// ACL Rules:
// - ProviderService.* → restricted to privileged UMCs (deployment_engine)
// - ClusterService.* → restricted to raft members
// - All other services → allow any authenticated UMC
//
// Note: Currently logs violations but does not enforce (strict enforcement requires
// a UMC registry to map PIDs/UIDs to UMC identities). Enable strict enforcement by
// uncommenting the return statements in the interceptors.
func validateMethodAccess(creds *PeerCredentials, fullMethod string) error {
	// TODO: Implement UMC registry to map PID/UID to UMC identity
	// For now, apply basic service-level rules based on method name patterns

	// Check if accessing ProviderService methods
	if len(fullMethod) > len("/ambient.umc.ProviderService/") &&
		fullMethod[:len("/ambient.umc.ProviderService/")] == "/ambient.umc.ProviderService/" {
		// ProviderService requires privileged access
		// TODO: Check if PID/UID belongs to deployment_engine UMC
		return fmt.Errorf("ProviderService methods restricted to deployment_engine UMC")
	}

	// Check if accessing ClusterService methods
	if len(fullMethod) > len("/ambient.umc.ClusterService/") &&
		fullMethod[:len("/ambient.umc.ClusterService/")] == "/ambient.umc.ClusterService/" {
		// ClusterService requires raft member privileges
		// TODO: Check if PID/UID belongs to a raft member
		return fmt.Errorf("ClusterService methods restricted to raft members")
	}

	// All other services (ExecService, SecretService, IdentityService, EventService, KVStorageService)
	// are accessible to any authenticated UMC
	return nil
}

// extractRawConnCredentials is implemented in platform-specific files:
// - auth_linux.go: Uses SO_PEERCRED to extract PID, UID, GID
// - auth_darwin.go: Uses LOCAL_PEERCRED to extract UID, GID (PID not available on macOS)
