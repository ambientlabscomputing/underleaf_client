package controlplane_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
)

type exposureMockConfig struct {
	baseURL string
	token   string
}

func newExposureMockConfig(baseURL, token string) *exposureMockConfig {
	return &exposureMockConfig{baseURL: baseURL, token: token}
}

func (c *exposureMockConfig) Get(key string) (interface{}, bool) {
	switch key {
	case "api.base_url":
		if c.baseURL == "" {
			return nil, false
		}
		return c.baseURL, true
	case "auth.token":
		if c.token == "" {
			return nil, false
		}
		return c.token, true
	}
	return nil, false
}

func (c *exposureMockConfig) Set(key string, value interface{}) error { return nil }
func (c *exposureMockConfig) Delete(key string) error                 { return nil }
func (c *exposureMockConfig) Config() policy_manager.Configuration {
	return policy_manager.Configuration{}
}
func (c *exposureMockConfig) ConfigClientInfo() map[string]interface{} {
	return map[string]interface{}{"type": "mock"}
}

func newTestExposureClient(t *testing.T, srv *httptest.Server) *controlplane.CPlaneExposureClient {
	t.Helper()
	cfg := newExposureMockConfig(srv.URL, "test-token")
	api := controlplane.NewAPIClient(cfg, srv.Client())
	return controlplane.NewCPlaneExposureClient(api)
}

func TestPostExposureResult_Success_Bound(t *testing.T) {
	var capturedBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.WriteHeader(204)
	}))
	defer srv.Close()
	client := newTestExposureClient(t, srv)
	err := client.PostExposureResult(context.Background(), "exp-001", "success", "https://pub.example.com", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedBody["status"] != "success" {
		t.Errorf("expected status=success, got %v", capturedBody["status"])
	}
	if capturedBody["public_url"] != "https://pub.example.com" {
		t.Errorf("expected public_url, got %v", capturedBody["public_url"])
	}
}

func TestPostExposureResult_Success_Error(t *testing.T) {
	var capturedBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.WriteHeader(204)
	}))
	defer srv.Close()
	client := newTestExposureClient(t, srv)
	err := client.PostExposureResult(context.Background(), "exp-002", "failure", "", "tunnel failed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedBody["status"] != "failure" {
		t.Errorf("expected status=failure, got %v", capturedBody["status"])
	}
	if capturedBody["error"] != "tunnel failed" {
		t.Errorf("expected error field, got %v", capturedBody["error"])
	}
}

func TestPostExposureResult_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	client := newTestExposureClient(t, srv)
	err := client.PostExposureResult(context.Background(), "exp-003", "success", "https://pub.example.com", "")
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
}

func TestPostExposureResult_URLContainsExposureID(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(204)
	}))
	defer srv.Close()
	client := newTestExposureClient(t, srv)
	_ = client.PostExposureResult(context.Background(), "exp-xyz", "success", "https://pub.example.com", "")
	expected := fmt.Sprintf("/exposures/%s/results", "exp-xyz")
	if capturedPath != expected {
		t.Errorf("expected path %q, got %q", expected, capturedPath)
	}
}

func TestPostExposureResult_MissingBaseURL(t *testing.T) {
	cfg := newExposureMockConfig("", "tok")
	api := controlplane.NewAPIClient(cfg, nil)
	client := controlplane.NewCPlaneExposureClient(api)
	err := client.PostExposureResult(context.Background(), "exp-004", "success", "https://pub.example.com", "")
	if err == nil {
		t.Fatal("expected error when base_url is empty, got nil")
	}
}
