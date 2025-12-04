package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// DeviceAuthResponse represents the response from the device authorization endpoint
type DeviceAuthResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// TokenResponse represents the response from the token polling endpoint
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

// TokenErrorResponse represents error responses from the token endpoint
type TokenErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

// CPlaneAuthClient handles authentication operations with the control plane
type CPlaneAuthClient struct {
	apiClient *APIClient
}

// NewAuthClient creates a new authentication client
func NewAuthClient(apiClient *APIClient) *CPlaneAuthClient {
	return &CPlaneAuthClient{
		apiClient: apiClient,
	}
}

// StartDeviceFlow initiates the device authorization flow
func (c *CPlaneAuthClient) StartDeviceFlow(ctx context.Context) (*DeviceAuthResponse, error) {
	baseURL, ok := c.apiClient.config.Get("api.base_url")
	if !ok || baseURL == nil {
		return nil, fmt.Errorf("api.base_url not configured")
	}

	// The device authorize endpoint is at /api/v1/users/device/authorize
	// We need to adjust the URL since base_url includes /servers
	apiBaseURL := baseURL.(string)
	// Remove /servers suffix if present to get the API root
	if len(apiBaseURL) >= 8 && apiBaseURL[len(apiBaseURL)-8:] == "/servers" {
		apiBaseURL = apiBaseURL[:len(apiBaseURL)-8]
	}

	url := apiBaseURL + "/users/device/authorize"

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ctx)

	resp, err := c.apiClient.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to start device flow: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp TokenErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Error != "" {
			return nil, fmt.Errorf("device flow error: %s - %s", errResp.Error, errResp.ErrorDescription)
		}
		return nil, fmt.Errorf("device flow failed with status %d", resp.StatusCode)
	}

	var authResp DeviceAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &authResp, nil
}

// PollForToken polls the token endpoint until authorization is complete
func (c *CPlaneAuthClient) PollForToken(ctx context.Context, deviceCode string) (*TokenResponse, error) {
	baseURL, ok := c.apiClient.config.Get("api.base_url")
	if !ok || baseURL == nil {
		return nil, fmt.Errorf("api.base_url not configured")
	}

	// The token endpoint is at /api/v1/users/device/token
	apiBaseURL := baseURL.(string)
	// Remove /servers suffix if present to get the API root
	if len(apiBaseURL) >= 8 && apiBaseURL[len(apiBaseURL)-8:] == "/servers" {
		apiBaseURL = apiBaseURL[:len(apiBaseURL)-8]
	}

	url := fmt.Sprintf("%s/users/device/token?device_code=%s", apiBaseURL, deviceCode)
	fmt.Printf("DEBUG [auth_client]: Polling token endpoint: %s\n", url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req = req.WithContext(ctx)

	resp, err := c.apiClient.httpClient.Do(req)
	if err != nil {
		fmt.Printf("DEBUG [auth_client]: HTTP request failed: %v\n", err)
		return nil, fmt.Errorf("failed to poll token: %w", err)
	}
	defer resp.Body.Close()
	fmt.Printf("DEBUG [auth_client]: Response status: %d\n", resp.StatusCode)

	// Handle different status codes
	switch resp.StatusCode {
	case http.StatusOK:
		// Decode the response - could be success or pending
		fmt.Println("DEBUG [auth_client]: Status 200 OK, decoding response")
		var tokenResp TokenResponse
		if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
			fmt.Printf("DEBUG [auth_client]: Failed to decode response: %v\n", err)
			return nil, fmt.Errorf("failed to decode token response: %w", err)
		}

		fmt.Printf("DEBUG [auth_client]: Decoded response - AccessToken length=%d, TokenType=%s\n", len(tokenResp.AccessToken), tokenResp.TokenType)

		// If access_token is empty, this means authorization is still pending
		// The backend returns 200 OK with empty token while waiting for user authorization
		if tokenResp.AccessToken == "" {
			fmt.Println("DEBUG [auth_client]: AccessToken empty, returning authorization_pending")
			return nil, &TokenError{
				Code:        "authorization_pending",
				Description: "User has not yet authorized the device",
			}
		}

		// Authorization complete - we have a token!
		fmt.Println("DEBUG [auth_client]: Authorization complete, returning token")
		return &tokenResp, nil

	case http.StatusBadRequest:
		// Check for specific error types
		var errResp TokenErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			return nil, fmt.Errorf("failed to decode error response: %w", err)
		}

		// Return the error as-is so the caller can handle it appropriately
		return nil, &TokenError{
			Code:        errResp.Error,
			Description: errResp.ErrorDescription,
		}

	default:
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
}

// TokenError represents a structured token endpoint error
type TokenError struct {
	Code        string
	Description string
}

func (e *TokenError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Description)
	}
	return e.Code
}

// IsAuthorizationPending returns true if the error indicates the user hasn't authorized yet
func (e *TokenError) IsAuthorizationPending() bool {
	return e.Code == "authorization_pending"
}

// IsSlowDown returns true if the error indicates we should slow down polling
func (e *TokenError) IsSlowDown() bool {
	return e.Code == "slow_down"
}

// IsExpired returns true if the error indicates the device code has expired
func (e *TokenError) IsExpired() bool {
	return e.Code == "expired_token"
}

// IsAccessDenied returns true if the error indicates the user denied authorization
func (e *TokenError) IsAccessDenied() bool {
	return e.Code == "access_denied"
}
