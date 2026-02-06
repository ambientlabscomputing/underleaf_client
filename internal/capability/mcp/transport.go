package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
)

// Transport represents an MCP transport layer
type Transport interface {
	Send(message interface{}) error
	Receive() ([]byte, error)
	Close() error
}

// UnixSocketTransport implements MCP transport over Unix sockets
type UnixSocketTransport struct {
	conn   net.Conn
	reader *bufio.Reader
	writer *bufio.Writer
	mu     sync.Mutex
}

// NewUnixSocketTransport creates a new Unix socket transport
func NewUnixSocketTransport(socketPath string) (*UnixSocketTransport, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Unix socket: %w", err)
	}

	return &UnixSocketTransport{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
	}, nil
}

// NewUnixSocketTransportFromConn creates a transport from an existing connection
func NewUnixSocketTransportFromConn(conn net.Conn) *UnixSocketTransport {
	return &UnixSocketTransport{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
	}
}

// Send sends a JSON-RPC message over the transport
func (t *UnixSocketTransport) Send(message interface{}) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Marshal message to JSON
	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Write JSON + newline
	if _, err := t.writer.Write(data); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}
	if err := t.writer.WriteByte('\n'); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	// Flush buffer
	if err := t.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush writer: %w", err)
	}

	return nil
}

// Receive receives a JSON-RPC message from the transport
func (t *UnixSocketTransport) Receive() ([]byte, error) {
	// Read until newline
	line, err := t.reader.ReadBytes('\n')
	if err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("connection closed")
		}
		return nil, fmt.Errorf("failed to read message: %w", err)
	}

	return line, nil
}

// Close closes the transport connection
func (t *UnixSocketTransport) Close() error {
	return t.conn.Close()
}
