package controlplane

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/ambientlabscomputing/mycelium_spine/sdk"
	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
)

type APIClient struct {
	httpClient *http.Client
	config     policy_manager.ConfigClient

	// CA certificate caching
	caCertPEM       []byte
	caCert          *x509.Certificate
	caCertFetchedAt time.Time
	caCertMu        sync.RWMutex
}

func NewAPIClient(config policy_manager.ConfigClient, h *http.Client) *APIClient {
	return &APIClient{
		httpClient: h,
		config:     config,
	}
}

func (c *APIClient) GET(ctx context.Context, path string, response interface{}) error {
	baseURL, ok := c.config.Get("api.base_url")
	token, _ := c.config.Get("auth.token")

	if !ok || baseURL == nil {
		return fmt.Errorf("api.base_url not configured")
	}
	if token == nil {
		return fmt.Errorf("auth.token not configured - please run 'ufctl auth login' first")
	}

	slog.Debug("API GET request", "base_url", baseURL, "path", path, "full_url", baseURL.(string)+path)
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL.(string)+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token.(string))

	// Add X-Trace-ID header if present in context
	if traceID := sdk.TraceIDFromContext(ctx); traceID != "" {
		req.Header.Set("X-Trace-ID", traceID)
	}

	// Add X-Organization-ID header if org context is set
	if orgID, ok := c.config.Get("local.organization_id"); ok && orgID != nil && orgID != "" {
		req.Header.Set("X-Organization-ID", orgID.(string))
		slog.Debug("Adding org context to request", "org_id", orgID.(string))
	}

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

func (c *APIClient) GETWithParams(ctx context.Context, path string, params url.Values, response interface{}) error {
	baseURL, ok := c.config.Get("api.base_url")
	token, _ := c.config.Get("auth.token")

	slog.Debug("GETWithParams: checking config", "ok", ok, "baseURL", baseURL, "baseURL_type", fmt.Sprintf("%T", baseURL))

	if !ok || baseURL == nil {
		return fmt.Errorf("api.base_url not configured")
	}
	if token == nil {
		return fmt.Errorf("auth.token not configured - please run 'ufctl auth login' first")
	}

	fullURL := baseURL.(string) + path
	if len(params) > 0 {
		fullURL += "?" + params.Encode()
	}

	slog.Debug("GETWithParams: making request", "fullURL", fullURL, "params", params.Encode())
	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token.(string))

	// Add X-Trace-ID header if present in context
	if traceID := sdk.TraceIDFromContext(ctx); traceID != "" {
		req.Header.Set("X-Trace-ID", traceID)
	}

	// Add X-Organization-ID header if org context is set
	if orgID, ok := c.config.Get("local.organization_id"); ok && orgID != nil && orgID != "" {
		req.Header.Set("X-Organization-ID", orgID.(string))
		slog.Debug("Adding org context to request", "org_id", orgID.(string))
	}

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

func (c *APIClient) POST(ctx context.Context, path string, payload interface{}, response interface{}) error {
	baseURL, _ := c.config.Get("api.base_url")
	token, _ := c.config.Get("auth.token")

	if baseURL == nil {
		return fmt.Errorf("api.base_url not configured")
	}
	if token == nil {
		return fmt.Errorf("auth.token not configured - please run 'ufctl auth login' first")
	}

	slog.Debug("APIClient POST", "url", baseURL.(string)+path, "payload", payload)
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL.(string)+path, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token.(string))
	req.Header.Set("Content-Type", "application/json")

	// Add X-Trace-ID header if present in context
	if traceID := sdk.TraceIDFromContext(ctx); traceID != "" {
		req.Header.Set("X-Trace-ID", traceID)
	}

	// Add X-Organization-ID header if org context is set
	if orgID, ok := c.config.Get("local.organization_id"); ok && orgID != nil && orgID != "" {
		req.Header.Set("X-Organization-ID", orgID.(string))
		slog.Debug("Adding org context to request", "org_id", orgID.(string))
	}

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

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("API request failed with status %s: %s", resp.Status, string(bodyBytes))
	}

	if response != nil && len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, response); err != nil {
			return fmt.Errorf("failed to parse response: %w, body: %s", err, string(bodyBytes))
		}
	}

	return nil
}

// POSTRaw sends raw bytes (e.g., YAML) to the server without JSON marshaling
func (c *APIClient) POSTRaw(ctx context.Context, path string, payloadBytes []byte, response interface{}) error {
	baseURL, _ := c.config.Get("api.base_url")
	token, _ := c.config.Get("auth.token")

	if baseURL == nil {
		return fmt.Errorf("api.base_url not configured")
	}
	if token == nil {
		return fmt.Errorf("auth.token not configured - please run 'ufctl auth login' first")
	}

	slog.Debug("APIClient POSTRaw", "url", baseURL.(string)+path, "payload_size", len(payloadBytes))
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL.(string)+path, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token.(string))
	req.Header.Set("Content-Type", "application/x-yaml")

	// Add X-Trace-ID header if present in context
	if traceID := sdk.TraceIDFromContext(ctx); traceID != "" {
		req.Header.Set("X-Trace-ID", traceID)
	}

	// Add X-Organization-ID header if org context is set
	if orgID, ok := c.config.Get("local.organization_id"); ok && orgID != nil && orgID != "" {
		req.Header.Set("X-Organization-ID", orgID.(string))
		slog.Debug("Adding org context to request", "org_id", orgID.(string))
	}

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
	slog.Debug("APIClient POSTRaw response", "status", resp.StatusCode, "body", string(bodyBytes))

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("API request failed with status %s: %s", resp.Status, string(bodyBytes))
	}

	if err := json.Unmarshal(bodyBytes, response); err != nil {
		return fmt.Errorf("failed to parse response: %w, body: %s", err, string(bodyBytes))
	}

	return nil
}

func (c *APIClient) PUT(ctx context.Context, path string, payload interface{}, response interface{}) error {
	baseURL, _ := c.config.Get("api.base_url")
	token, _ := c.config.Get("auth.token")

	if baseURL == nil {
		return fmt.Errorf("api.base_url not configured")
	}
	if token == nil {
		return fmt.Errorf("auth.token not configured - please run 'ufctl auth login' first")
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "PUT", baseURL.(string)+path, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token.(string))
	req.Header.Set("Content-Type", "application/json")

	// Add X-Trace-ID header if present in context
	if traceID := sdk.TraceIDFromContext(ctx); traceID != "" {
		req.Header.Set("X-Trace-ID", traceID)
	}

	// Add X-Organization-ID header if org context is set
	if orgID, ok := c.config.Get("local.organization_id"); ok && orgID != nil && orgID != "" {
		req.Header.Set("X-Organization-ID", orgID.(string))
		slog.Debug("Adding org context to request", "org_id", orgID.(string))
	}

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

func (c *APIClient) DELETE(ctx context.Context, path string, response interface{}) error {
	baseURL, _ := c.config.Get("api.base_url")
	token, _ := c.config.Get("auth.token")

	if baseURL == nil {
		return fmt.Errorf("api.base_url not configured")
	}
	if token == nil {
		return fmt.Errorf("auth.token not configured - please run 'ufctl auth login' first")
	}

	req, err := http.NewRequestWithContext(ctx, "DELETE", baseURL.(string)+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token.(string))

	// Add X-Trace-ID header if present in context
	if traceID := sdk.TraceIDFromContext(ctx); traceID != "" {
		req.Header.Set("X-Trace-ID", traceID)
	}

	// Add X-Organization-ID header if org context is set
	if orgID, ok := c.config.Get("local.organization_id"); ok && orgID != nil && orgID != "" {
		req.Header.Set("X-Organization-ID", orgID.(string))
		slog.Debug("Adding org context to request", "org_id", orgID.(string))
	}

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

func (c *APIClient) PATCH(ctx context.Context, path string, payload interface{}, response interface{}) error {
	baseURL, _ := c.config.Get("api.base_url")
	token, _ := c.config.Get("auth.token")

	if baseURL == nil {
		return fmt.Errorf("api.base_url not configured")
	}
	if token == nil {
		return fmt.Errorf("auth.token not configured - please run 'ufctl auth login' first")
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "PATCH", baseURL.(string)+path, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token.(string))
	req.Header.Set("Content-Type", "application/json")

	// Add X-Trace-ID header if present in context
	if traceID := sdk.TraceIDFromContext(ctx); traceID != "" {
		req.Header.Set("X-Trace-ID", traceID)
	}

	// Add X-Organization-ID header if org context is set
	if orgID, ok := c.config.Get("local.organization_id"); ok && orgID != nil && orgID != "" {
		req.Header.Set("X-Organization-ID", orgID.(string))
		slog.Debug("Adding org context to request", "org_id", orgID.(string))
	}

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

// GetServerConfig fetches the current configuration for a server from the control plane
// Implements the ControlPlanePolicyClient interface for config_manager
func (c *APIClient) GetServerConfig(ctx context.Context, serverID string) (map[string]interface{}, int, error) {
	type ServerResponse struct {
		ID            string `json:"id"`
		Configuration struct {
			Version int                    `json:"version"`
			Payload map[string]interface{} `json:"payload"`
		} `json:"configuration"`
	}

	var resp ServerResponse
	if err := c.GET(ctx, "/servers/servers/"+serverID, &resp); err != nil {
		return nil, 0, fmt.Errorf("failed to get server config: %w", err)
	}

	return resp.Configuration.Payload, resp.Configuration.Version, nil
}

// GetCACertificate fetches and caches the CA certificate from the server API
// The certificate is cached for 1 hour to reduce unnecessary requests
func (c *APIClient) GetCACertificate(ctx context.Context) ([]byte, error) {
	c.caCertMu.RLock()
	// Check if we have a cached cert that's less than 1 hour old
	if c.caCertPEM != nil && time.Since(c.caCertFetchedAt) < time.Hour {
		defer c.caCertMu.RUnlock()
		return c.caCertPEM, nil
	}
	c.caCertMu.RUnlock()

	// Need to fetch the certificate
	c.caCertMu.Lock()
	defer c.caCertMu.Unlock()

	// Double-check after acquiring write lock
	if c.caCertPEM != nil && time.Since(c.caCertFetchedAt) < time.Hour {
		return c.caCertPEM, nil
	}

	baseURL, ok := c.config.Get("api.base_url")
	if !ok || baseURL == nil {
		return nil, fmt.Errorf("api.base_url not configured")
	}

	// Fetch CA certificate (this is a public endpoint, no auth required)
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL.(string)+"/ca/certificate", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create CA cert request: %w", err)
	}

	req.Header.Set("X-Trace-ID", sdk.TraceIDFromContext(ctx))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch CA certificate: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch CA certificate: status %d: %s", resp.StatusCode, string(body))
	}

	// Read the certificate
	certPEM, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	// Validate that it's a valid PEM-encoded certificate
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("invalid CA certificate: not a valid PEM-encoded certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	// Cache the certificate
	c.caCertPEM = certPEM
	c.caCert = cert
	c.caCertFetchedAt = time.Now()

	slog.Info("CA certificate fetched and cached",
		"subject", cert.Subject.String(),
		"valid_until", cert.NotAfter,
		"cache_duration", time.Hour)

	return certPEM, nil
}

// GetCACertificateParsed returns the parsed CA certificate
// Fetches from server if not cached
func (c *APIClient) GetCACertificateParsed(ctx context.Context) (*x509.Certificate, error) {
	if _, err := c.GetCACertificate(ctx); err != nil {
		return nil, err
	}

	c.caCertMu.RLock()
	defer c.caCertMu.RUnlock()

	return c.caCert, nil
}

// InvalidateCACertificateCache clears the cached CA certificate
// Useful for testing or when the CA certificate is rotated
func (c *APIClient) InvalidateCACertificateCache() {
	c.caCertMu.Lock()
	defer c.caCertMu.Unlock()

	c.caCertPEM = nil
	c.caCert = nil
	c.caCertFetchedAt = time.Time{}

	slog.Info("CA certificate cache invalidated")
}

// POSTMultipartToURL uploads a file via multipart/form-data to a full URL.
// fields is a map of additional form fields to include in the request.
// fileField is the name of the file form field.
// fileName is the filename to use in the Content-Disposition header.
// fileData is the raw file bytes.
func (c *APIClient) POSTMultipartToURL(ctx context.Context, fullURL string, fields map[string]string, fileField, fileName string, fileData []byte) error {
	token, _ := c.config.Get("auth.token")
	if token == nil {
		return fmt.Errorf("auth.token not configured - please run 'ufctl auth login' first")
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	// Write additional fields first
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			return fmt.Errorf("failed to write form field %q: %w", k, err)
		}
	}

	// Write the file
	fw, err := mw.CreateFormFile(fileField, fileName)
	if err != nil {
		return fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := fw.Write(fileData); err != nil {
		return fmt.Errorf("failed to write file data: %w", err)
	}

	if err := mw.Close(); err != nil {
		return fmt.Errorf("failed to close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token.(string))
	req.Header.Set("Content-Type", mw.FormDataContentType())

	// Add X-Trace-ID header if present in context
	if traceID := sdk.TraceIDFromContext(ctx); traceID != "" {
		req.Header.Set("X-Trace-ID", traceID)
	}

	// Add X-Organization-ID header if org context is set
	if orgID, ok := c.config.Get("local.organization_id"); ok && orgID != nil && orgID != "" {
		req.Header.Set("X-Organization-ID", orgID.(string))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("multipart upload failed with status %s: %s", resp.Status, string(respBody))
	}

	slog.Debug("POSTMultipartToURL success", "url", fullURL, "status", resp.StatusCode)
	return nil
}

// Exposure API Methods

// CreateExposure creates a new public exposure for a deployment service
func (c *APIClient) CreateExposure(ctx context.Context, deploymentID, serviceName string, targetPort int, hostname, serverID string) (interface{}, error) {
	payload := map[string]interface{}{
		"deployment_id": deploymentID,
		"service_name":  serviceName,
		"target_port":   targetPort,
	}
	if hostname != "" {
		payload["hostname"] = hostname
	}
	if serverID != "" {
		payload["server_id"] = serverID
	}

	var response interface{}
	if err := c.POST(ctx, "/exposures", payload, &response); err != nil {
		return nil, err
	}
	return response, nil
}

// GetExposure retrieves a single exposure by ID
func (c *APIClient) GetExposure(ctx context.Context, exposureID string) (interface{}, error) {
	var response interface{}
	if err := c.GET(ctx, "/exposures/"+exposureID, &response); err != nil {
		return nil, err
	}
	return response, nil
}

// QueryExposures lists exposures with optional filters
func (c *APIClient) QueryExposures(ctx context.Context, deploymentID, serverID, status string, limit, offset int) (interface{}, error) {
	params := url.Values{}
	if deploymentID != "" {
		params.Set("deployment_id", deploymentID)
	}
	if serverID != "" {
		params.Set("server_id", serverID)
	}
	if status != "" {
		params.Set("status", status)
	}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	if offset > 0 {
		params.Set("offset", fmt.Sprintf("%d", offset))
	}

	var response interface{}
	if err := c.GETWithParams(ctx, "/exposures", params, &response); err != nil {
		return nil, err
	}
	return response, nil
}

// RevokeExposure revokes a public exposure by ID
func (c *APIClient) RevokeExposure(ctx context.Context, exposureID string) error {
	var response interface{}
	return c.DELETE(ctx, "/exposures/"+exposureID, &response)
}

// GetDeploymentExposures retrieves all exposures for a deployment
func (c *APIClient) GetDeploymentExposures(ctx context.Context, deploymentID string) (interface{}, error) {
	var response interface{}
	if err := c.GET(ctx, "/deployments/"+deploymentID+"/exposures", &response); err != nil {
		return nil, err
	}
	return response, nil
}
