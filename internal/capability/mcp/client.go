package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Client represents an MCP client
type Client struct {
	transport Transport
	nextID    atomic.Int64
	timeout   time.Duration

	// Pending requests
	pending map[int64]chan *Response
	mu      sync.RWMutex
}

// NewClient creates a new MCP client
func NewClient(transport Transport) *Client {
	return &Client{
		transport: transport,
		timeout:   30 * time.Second,
		pending:   make(map[int64]chan *Response),
	}
}

// SetTimeout sets the request timeout
func (c *Client) SetTimeout(timeout time.Duration) {
	c.timeout = timeout
}

// Initialize performs the MCP handshake
func (c *Client) Initialize(ctx context.Context, clientInfo ClientInfo) (*InitializeResult, error) {
	params := InitializeParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo:      clientInfo,
	}

	var result InitializeResult
	if err := c.Call(ctx, MethodInitialize, params, &result); err != nil {
		return nil, fmt.Errorf("initialize failed: %w", err)
	}

	return &result, nil
}

// ListTools requests the list of available tools from the provider
func (c *Client) ListTools(ctx context.Context) (*ToolsListResult, error) {
	var result ToolsListResult
	if err := c.Call(ctx, MethodToolsList, nil, &result); err != nil {
		return nil, fmt.Errorf("list tools failed: %w", err)
	}

	return &result, nil
}

// CallTool invokes a tool on the provider
func (c *Client) CallTool(ctx context.Context, toolName string, arguments map[string]interface{}) (*ToolsCallResult, error) {
	params := ToolsCallParams{
		Name:      toolName,
		Arguments: arguments,
	}

	var result ToolsCallResult
	if err := c.Call(ctx, MethodToolsCall, params, &result); err != nil {
		return nil, fmt.Errorf("call tool failed: %w", err)
	}

	return &result, nil
}

// Shutdown gracefully shuts down the MCP connection
func (c *Client) Shutdown(ctx context.Context) error {
	if err := c.Call(ctx, MethodShutdown, nil, nil); err != nil {
		return fmt.Errorf("shutdown failed: %w", err)
	}

	return nil
}

// Call makes a JSON-RPC method call
func (c *Client) Call(ctx context.Context, method string, params interface{}, result interface{}) error {
	// Generate request ID
	id := c.nextID.Add(1)

	// Create request
	req := Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	// Create response channel
	respChan := make(chan *Response, 1)
	c.mu.Lock()
	c.pending[id] = respChan
	c.mu.Unlock()

	// Ensure cleanup
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	// Send request
	if err := c.transport.Send(req); err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	// Start response reader if not already running
	go c.readResponses()

	// Wait for response with timeout
	select {
	case resp := <-respChan:
		if resp.Error != nil {
			return fmt.Errorf("RPC error %d: %s", resp.Error.Code, resp.Error.Message)
		}

		// Unmarshal result if provided
		if result != nil && resp.Result != nil {
			resultBytes, err := json.Marshal(resp.Result)
			if err != nil {
				return fmt.Errorf("failed to marshal result: %w", err)
			}

			if err := json.Unmarshal(resultBytes, result); err != nil {
				return fmt.Errorf("failed to unmarshal result: %w", err)
			}
		}

		return nil

	case <-ctx.Done():
		return fmt.Errorf("request cancelled: %w", ctx.Err())

	case <-time.After(c.timeout):
		return fmt.Errorf("request timeout after %v", c.timeout)
	}
}

// readResponses reads responses from the transport and routes them to pending requests
func (c *Client) readResponses() {
	for {
		data, err := c.transport.Receive()
		if err != nil {
			// Connection closed or error
			return
		}

		// Parse response
		var resp Response
		if err := json.Unmarshal(data, &resp); err != nil {
			continue // Skip invalid responses
		}

		// Route to pending request
		c.mu.RLock()
		ch, exists := c.pending[resp.ID.(int64)]
		c.mu.RUnlock()

		if exists {
			select {
			case ch <- &resp:
			default:
				// Channel full, skip
			}
		}
	}
}

// Close closes the MCP client connection
func (c *Client) Close() error {
	return c.transport.Close()
}
