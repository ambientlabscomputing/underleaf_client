package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ambientlabscomputing/mycelium_spine/sdk"
)

// OrgMembership represents a user's membership in an organization (summary view)
type OrgMembership struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	Role     string `json:"role"`
	Status   string `json:"status,omitempty"`
	JoinedAt string `json:"joined_at,omitempty"`
}

// CurrentOrgContext contains current organization context with permissions
type CurrentOrgContext struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Slug        string   `json:"slug"`
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
}

// User represents user profile information
type User struct {
	ID            string `json:"id"`
	Auth0ID       string `json:"auth0_id"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	Picture       string `json:"picture,omitempty"`
	Status        string `json:"status"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	LastLogin     string `json:"last_login,omitempty"`
	EmailVerified bool   `json:"email_verified"`
}

// UserSettings contains user preferences
type UserSettings struct {
	UserID       string `json:"user_id"`
	Theme        string `json:"theme"`
	DefaultOrgID string `json:"default_org_id,omitempty"`
	Timezone     string `json:"timezone"`
	Locale       string `json:"locale"`
	UpdatedAt    string `json:"updated_at"`
}

// GetMeResponse represents the response for GET /me
type GetMeResponse struct {
	User                User               `json:"user"`
	Organizations       []OrgMembership    `json:"organizations"`
	CurrentOrganization *CurrentOrgContext `json:"current_organization,omitempty"`
}

// CPlaneUserClient handles user operations with the control plane
type CPlaneUserClient struct {
	apiClient *APIClient
}

// NewUserClient creates a new user client
func NewUserClient(apiClient *APIClient) *CPlaneUserClient {
	return &CPlaneUserClient{
		apiClient: apiClient,
	}
}

// GetMe retrieves the current user's profile and organization memberships
func (c *CPlaneUserClient) GetMe(ctx context.Context) (*GetMeResponse, error) {
	baseURL, ok := c.apiClient.config.Get("api.base_url")
	if !ok || baseURL == nil {
		return nil, fmt.Errorf("api.base_url not configured")
	}

	token, ok := c.apiClient.config.Get("auth.token")
	if !ok || token == nil {
		return nil, fmt.Errorf("auth.token not configured - please run 'ufctl auth login' first")
	}

	// The /me endpoint is at /api/v1/users/me
	// We need to adjust the URL since base_url includes /servers
	apiBaseURL := baseURL.(string)
	// Remove /servers suffix if present to get the API root
	if len(apiBaseURL) >= 8 && apiBaseURL[len(apiBaseURL)-8:] == "/servers" {
		apiBaseURL = apiBaseURL[:len(apiBaseURL)-8]
	}

	url := apiBaseURL + "/users/me"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token.(string))
	req.Header.Set("X-Trace-ID", sdk.TraceIDFromContext(ctx))
	req = req.WithContext(ctx)

	resp, err := c.apiClient.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get user profile: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get user profile failed with status %d", resp.StatusCode)
	}

	var meResp GetMeResponse
	if err := json.NewDecoder(resp.Body).Decode(&meResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &meResp, nil
}

// GetMySettings retrieves the current user's settings
func (c *CPlaneUserClient) GetMySettings(ctx context.Context) (*UserSettings, error) {
	baseURL, ok := c.apiClient.config.Get("api.base_url")
	if !ok || baseURL == nil {
		return nil, fmt.Errorf("api.base_url not configured")
	}

	token, ok := c.apiClient.config.Get("auth.token")
	if !ok || token == nil {
		return nil, fmt.Errorf("auth.token not configured")
	}

	// The /me/settings endpoint is at /api/v1/users/me/settings
	apiBaseURL := baseURL.(string)
	if len(apiBaseURL) >= 8 && apiBaseURL[len(apiBaseURL)-8:] == "/servers" {
		apiBaseURL = apiBaseURL[:len(apiBaseURL)-8]
	}

	url := apiBaseURL + "/users/me/settings"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token.(string))
	req.Header.Set("X-Trace-ID", sdk.TraceIDFromContext(ctx))
	req = req.WithContext(ctx)

	resp, err := c.apiClient.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get user settings: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get user settings failed with status %d", resp.StatusCode)
	}

	var settings UserSettings
	if err := json.NewDecoder(resp.Body).Decode(&settings); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &settings, nil
}

// Organization represents an organization
type Organization struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	OwnerID   string `json:"owner_id"`
}

// AutoProvisionOrganization creates a default organization for the user if they don't have one
func (c *CPlaneUserClient) AutoProvisionOrganization(ctx context.Context) (*Organization, error) {
	baseURL, ok := c.apiClient.config.Get("api.base_url")
	if !ok || baseURL == nil {
		return nil, fmt.Errorf("api.base_url not configured")
	}

	token, ok := c.apiClient.config.Get("auth.token")
	if !ok || token == nil {
		return nil, fmt.Errorf("auth.token not configured - please run 'ufctl auth login' first")
	}

	// The /me/auto-provision endpoint is at /api/v1/users/me/auto-provision
	apiBaseURL := baseURL.(string)
	if len(apiBaseURL) >= 8 && apiBaseURL[len(apiBaseURL)-8:] == "/servers" {
		apiBaseURL = apiBaseURL[:len(apiBaseURL)-8]
	}

	url := apiBaseURL + "/users/me/auto-provision"

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token.(string))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Trace-ID", sdk.TraceIDFromContext(ctx))
	req = req.WithContext(ctx)

	resp, err := c.apiClient.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to auto-provision organization: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auto-provision organization failed with status %d", resp.StatusCode)
	}

	var org Organization
	if err := json.NewDecoder(resp.Body).Decode(&org); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &org, nil
}
