//go:build linux
// +build linux

package kernel

import (
	"fmt"
	"net"
	"syscall"
)

// extractRawConnCredentials extracts peer credentials from a Unix socket connection on Linux.
// Uses SO_PEERCRED socket option to get the PID, UID, and GID of the connecting process.
func extractRawConnCredentials(conn net.Conn) (*PeerCredentials, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return nil, fmt.Errorf("not a Unix connection")
	}

	// Get the raw file descriptor from the connection
	rawConn, err := unixConn.SyscallConn()
	if err != nil {
		return nil, fmt.Errorf("failed to get raw connection: %w", err)
	}

	var creds *syscall.Ucred
	var syscallErr error

	// Access the file descriptor to call getsockopt
	err = rawConn.Control(func(fd uintptr) {
		creds, syscallErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	})

	if err != nil {
		return nil, fmt.Errorf("failed to access file descriptor: %w", err)
	}

	if syscallErr != nil {
		return nil, fmt.Errorf("SO_PEERCRED syscall failed: %w", syscallErr)
	}

	if creds == nil {
		return nil, fmt.Errorf("SO_PEERCRED returned nil credentials")
	}

	return &PeerCredentials{
		PID: int(creds.Pid),
		UID: int(creds.Uid),
		GID: int(creds.Gid),
	}, nil
}
