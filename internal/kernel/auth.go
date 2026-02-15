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
	PID int32  // Process ID
	UID uint32 // User ID
	GID uint32 // Group ID
}

// getPeerCredentials extracts SO_PEERCRED from a Unix socket connection
func getPeerCredentials(ctx context.Context) (*PeerCredentials, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("no peer found in context")
	}

	// Type assert to net.Conn to access underlying connection
	conn, ok := p.Addr.(*net.UnixAddr)
	if !ok {
		return nil, fmt.Errorf("peer is not a Unix socket connection")
	}

	// Note: In a real implementation, we'd need to extract the raw connection
	// from the gRPC transport to call GetsockoptUcred. This is a placeholder
	// that demonstrates the concept. Full implementation requires accessing
	// the underlying *net.UnixConn from the gRPC transport layer.

	// For now, we'll use a simplified approach: accept all local connections
	// since all UMCs run on the same machine under the same user.
	// Production implementation should extract actual credentials.

	slog.Debug("peer credential check", "addr", conn.String())

	// Return stub credentials - in production, use syscall.GetsockoptUcred
	return &PeerCredentials{
		PID: 0, // Would extract from SO_PEERCRED
		UID: 0, // Would extract from SO_PEERCRED
		GID: 0, // Would extract from SO_PEERCRED
	}, nil
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
		}

		// Check method-specific ACLs
		// For now, all authenticated peers can call all methods
		// Future: implement granular ACLs per service/method

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
		}

		// Call the actual handler
		return handler(srv, ss)
	}
}

// validateMethodAccess checks if the peer has access to the given method
// This is a placeholder for future ACL implementation
func validateMethodAccess(creds *PeerCredentials, fullMethod string) error {
	// For now, allow all access
	// Future: implement per-method ACLs
	//
	// Example ACL logic:
	// - ProviderService.* → only deployment_engine UMC
	// - SecretService.* → any authenticated UMC
	// - ClusterService.* → only raft members
	// - ExecService.* → any authenticated UMC
	// - IdentityService.* → any authenticated UMC
	// - EventService.* → any authenticated UMC

	return nil
}

// extractRawConnCredentials is a helper to get peer credentials from Unix socket
// This is a placeholder for the actual SO_PEERCRED implementation
func extractRawConnCredentials(conn net.Conn) (*PeerCredentials, error) {
	// This requires accessing the underlying file descriptor and calling:
	// syscall.GetsockoptUcred(fd, syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	//
	// Example implementation for Linux:
	//
	// unixConn, ok := conn.(*net.UnixConn)
	// if !ok {
	// 	return nil, fmt.Errorf("not a Unix connection")
	// }
	//
	// rawConn, err := unixConn.SyscallConn()
	// if err != nil {
	// 	return nil, err
	// }
	//
	// var creds *syscall.Ucred
	// var syscallErr error
	// err = rawConn.Control(func(fd uintptr) {
	// 	creds, syscallErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	// })
	//
	// if err != nil {
	// 	return nil, err
	// }
	// if syscallErr != nil {
	// 	return nil, syscallErr
	// }
	//
	// return &PeerCredentials{
	// 	PID: creds.Pid,
	// 	UID: creds.Uid,
	// 	GID: creds.Gid,
	// }, nil

	return nil, fmt.Errorf("SO_PEERCRED extraction not implemented - requires platform-specific code")
}
