package agent

import (
	"bytes"
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

// DoRequest performs a generic HTTP request to the agent
func (c *Client) DoRequest(method, path string, body []byte) ([]byte, error) {
	var req *http.Request
	var err error

	if body != nil {
		req, err = http.NewRequest(method, c.baseURL+path, bytes.NewReader(body))
	} else {
		req, err = http.NewRequest(method, c.baseURL+path, nil)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// MMA Event Stream Client Methods

// GetMMeshStatus retrieves the MMA event stream server status
func (c *Client) GetMMeshStatus() (map[string]interface{}, error) {
	resp, err := c.client.Get(c.baseURL + "/api/v1/mmesh/status")
	if err != nil {
		return nil, fmt.Errorf("failed to get MMA status: %w", err)
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

// GetMMeshSubscribers retrieves information about active MMA subscribers
func (c *Client) GetMMeshSubscribers() (map[string]interface{}, error) {
	resp, err := c.client.Get(c.baseURL + "/api/v1/mmesh/subscribers")
	if err != nil {
		return nil, fmt.Errorf("failed to get MMA subscribers: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("agent returned status %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result, nil
}

// GetMMeshBuffer retrieves MMA event stream buffer statistics
func (c *Client) GetMMeshBuffer() (map[string]interface{}, error) {
	resp, err := c.client.Get(c.baseURL + "/api/v1/mmesh/buffer")
	if err != nil {
		return nil, fmt.Errorf("failed to get buffer stats: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("agent returned status %d: %s", resp.StatusCode, string(body))
	}

	var stats map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return stats, nil
}

// PublishMMeshEvent publishes a test event to the MMA event stream
func (c *Client) PublishMMeshEvent(eventType string, payload map[string]interface{}, entityKind, entityID string) (map[string]interface{}, error) {
	req := map[string]interface{}{
		"event_type":  eventType,
		"payload":     payload,
		"entity_kind": entityKind,
		"entity_id":   entityID,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.client.Post(c.baseURL+"/api/v1/mmesh/publish", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to publish event: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("agent returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result, nil
}
