//go:build darwin
// +build darwin

package kernel

import (
	"fmt"
	"net"
	"syscall"
	"unsafe"
)

// xucred structure for LOCAL_PEERCRED on Darwin/macOS
// Based on <sys/ucred.h> definition
type xucred struct {
	Version uint32
	UID     uint32
	NGids   int16
	Gids    [16]uint32
}

const (
	// LOCAL_PEERCRED socket option for getting peer credentials on macOS
	LOCAL_PEERCRED = 0x001
	// SOL_LOCAL is the socket level for local domain sockets on Darwin
	SOL_LOCAL = 0
	// XUCRED_VERSION is the expected version number
	XUCRED_VERSION = 0
)

// extractRawConnCredentials extracts peer credentials from a Unix socket connection on macOS.
// Uses LOCAL_PEERCRED socket option to get the UID and GID of the connecting process.
// Note: macOS does not provide PID via LOCAL_PEERCRED, so PID will be 0.
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

	var creds xucred
	var syscallErr error

	// Access the file descriptor to call getsockopt
	err = rawConn.Control(func(fd uintptr) {
		credsLen := uint32(unsafe.Sizeof(creds))
		_, _, errno := syscall.Syscall6(
			syscall.SYS_GETSOCKOPT,
			fd,
			SOL_LOCAL,
			LOCAL_PEERCRED,
			uintptr(unsafe.Pointer(&creds)),
			uintptr(unsafe.Pointer(&credsLen)),
			0,
		)
		if errno != 0 {
			syscallErr = errno
		}
	})

	if err != nil {
		return nil, fmt.Errorf("failed to access file descriptor: %w", err)
	}

	if syscallErr != nil {
		return nil, fmt.Errorf("LOCAL_PEERCRED syscall failed: %w", syscallErr)
	}

	if creds.Version != XUCRED_VERSION {
		return nil, fmt.Errorf("unexpected xucred version: got %d, expected %d", creds.Version, XUCRED_VERSION)
	}

	// Get primary GID (first entry in Gids array if NGids > 0)
	var gid int
	if creds.NGids > 0 {
		gid = int(creds.Gids[0])
	}

	return &PeerCredentials{
		PID: 0, // macOS LOCAL_PEERCRED does not provide PID
		UID: int(creds.UID),
		GID: gid,
	}, nil
}
