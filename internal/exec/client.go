package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client provides exec functionality to CLI via the local agent
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new exec client for communicating with the local agent
func NewClient(agentPort int) *Client {
	return &Client{
		baseURL: fmt.Sprintf("http://localhost:%d", agentPort),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ExecuteLocal executes a command on the local agent (for development/testing)
func (c *Client) ExecuteLocal(ctx context.Context, req CommandRequest) (*CommandResult, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/v1/commands/execute", bytes.NewBuffer(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agent returned error: %s", string(body))
	}

	var result CommandResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// GetSettings gets the current command settings from the agent
func (c *Client) GetSettings(ctx context.Context) (*CommandSettings, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/v1/commands/settings", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("agent returned error: %s", string(body))
	}

	var settings CommandSettings
	if err := json.NewDecoder(resp.Body).Decode(&settings); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &settings, nil
}
