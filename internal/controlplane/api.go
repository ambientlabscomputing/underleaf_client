package controlplane

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/ambientlabscomputing/underleaf_client/internal/config_manager"
)

type APIClient struct {
	httpClient *http.Client
	config     config_manager.ConfigClient
}

func NewAPIClient(config config_manager.ConfigClient, h *http.Client) *APIClient {
	return &APIClient{
		httpClient: h,
		config:     config,
	}
}

func (c *APIClient) GET(path string, response interface{}) error {
	baseURL, ok := c.config.Get("api.base_url")
	token, _ := c.config.Get("auth.token")

	if !ok || baseURL == nil {
		return fmt.Errorf("api.base_url not configured")
	}
	if token == nil {
		return fmt.Errorf("auth.token not configured - please run 'ufctl local auth login' first")
	}

	slog.Debug("API GET request", "base_url", baseURL, "path", path, "full_url", baseURL.(string)+path)
	req, err := http.NewRequest("GET", baseURL.(string)+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token.(string))
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var errorResp any
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
			return err
		}
		return fmt.Errorf("API request failed with status %s: %s", resp.Status, errorResp)
	}

	if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
		return err
	}

	return nil
}

func (c *APIClient) GETWithParams(path string, params url.Values, response interface{}) error {
	baseURL, ok := c.config.Get("api.base_url")
	token, _ := c.config.Get("auth.token")

	slog.Debug("GETWithParams: checking config", "ok", ok, "baseURL", baseURL, "baseURL_type", fmt.Sprintf("%T", baseURL))

	if !ok || baseURL == nil {
		return fmt.Errorf("api.base_url not configured")
	}
	if token == nil {
		return fmt.Errorf("auth.token not configured - please run 'ufctl local auth login' first")
	}

	fullURL := baseURL.(string) + path
	if len(params) > 0 {
		fullURL += "?" + params.Encode()
	}

	slog.Debug("GETWithParams: making request", "fullURL", fullURL, "params", params.Encode())
	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token.(string))
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var errorResp any
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
			return err
		}
		return fmt.Errorf("API request failed with status %s: %s", resp.Status, errorResp)
	}

	if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
		return err
	}

	return nil
}

func (c *APIClient) POST(path string, payload interface{}, response interface{}) error {
	baseURL, _ := c.config.Get("api.base_url")
	token, _ := c.config.Get("auth.token")

	if baseURL == nil {
		return fmt.Errorf("api.base_url not configured")
	}
	if token == nil {
		return fmt.Errorf("auth.token not configured - please run 'ufctl local auth login' first")
	}

	slog.Debug("APIClient POST", "url", baseURL.(string)+path, "payload", payload)
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", baseURL.(string)+path, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token.(string))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Read the body for debugging and parsing
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	slog.Debug("APIClient POST response", "status", resp.StatusCode, "body", string(bodyBytes))

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("API request failed with status %s: %s", resp.Status, string(bodyBytes))
	}

	if err := json.Unmarshal(bodyBytes, response); err != nil {
		return fmt.Errorf("failed to parse response: %w, body: %s", err, string(bodyBytes))
	}

	return nil
}

func (c *APIClient) PUT(path string, payload interface{}, response interface{}) error {
	baseURL, _ := c.config.Get("api.base_url")
	token, _ := c.config.Get("auth.token")

	if baseURL == nil {
		return fmt.Errorf("api.base_url not configured")
	}
	if token == nil {
		return fmt.Errorf("auth.token not configured - please run 'ufctl local auth login' first")
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("PUT", baseURL.(string)+path, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token.(string))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var errorResp any
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
			return err
		}
		return fmt.Errorf("API request failed with status %s: %s", resp.Status, errorResp)
	}

	if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
		return err
	}

	return nil
}

func (c *APIClient) DELETE(path string, response interface{}) error {
	baseURL, _ := c.config.Get("api.base_url")
	token, _ := c.config.Get("auth.token")
	req, err := http.NewRequest("DELETE", baseURL.(string)+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token.(string))
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var errorResp any
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
			return err
		}
		return fmt.Errorf("API request failed with status %s: %s", resp.Status, errorResp)
	}

	if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
		return err
	}

	return nil
}

func (c *APIClient) PATCH(path string, payload interface{}, response interface{}) error {
	baseURL, _ := c.config.Get("api.base_url")
	token, _ := c.config.Get("auth.token")

	if baseURL == nil {
		return fmt.Errorf("api.base_url not configured")
	}
	if token == nil {
		return fmt.Errorf("auth.token not configured - please run 'ufctl local auth login' first")
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("PATCH", baseURL.(string)+path, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token.(string))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var errorResp any
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
			return err
		}
		return fmt.Errorf("API request failed with status %s: %s", resp.Status, errorResp)
	}

	if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
		return err
	}

	return nil
}
