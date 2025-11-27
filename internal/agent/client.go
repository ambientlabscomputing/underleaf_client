package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client communicates with a running agent
type Client struct {
	baseURL string
	client  *http.Client
}

// NewClient creates a new agent client
func NewClient(port int) *Client {
	return &Client{
		baseURL: fmt.Sprintf("http://localhost:%d", port),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Ping checks if the agent is responding
func (c *Client) Ping() error {
	resp, err := c.client.Get(c.baseURL + "/ping")
	if err != nil {
		return fmt.Errorf("failed to ping agent: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("agent returned status: %d", resp.StatusCode)
	}

	return nil
}

// GetStatus retrieves the agent status
func (c *Client) GetStatus() (map[string]interface{}, error) {
	resp, err := c.client.Get(c.baseURL + "/api/v1/status")
	if err != nil {
		return nil, fmt.Errorf("failed to get status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("agent returned status %d: %s", resp.StatusCode, string(body))
	}

	var status map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return status, nil
}

// GetHealth checks the agent health
func (c *Client) GetHealth() (bool, error) {
	resp, err := c.client.Get(c.baseURL + "/health")
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}
