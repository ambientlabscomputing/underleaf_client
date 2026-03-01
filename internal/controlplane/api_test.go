package controlplane_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
)

// mockConfig is a simple in-memory implementation of policy_manager.ConfigClient.
type mockConfig struct {
	settings map[string]interface{}
}

func (m *mockConfig) Get(key string) (interface{}, bool) {
	v, ok := m.settings[key]
	return v, ok
}

func (m *mockConfig) Set(key string, value interface{}) error {
	m.settings[key] = value
	return nil
}

func (m *mockConfig) Delete(key string) error {
	delete(m.settings, key)
	return nil
}

func (m *mockConfig) Config() policy_manager.Configuration {
	return policy_manager.Configuration{Payload: m.settings}
}

func (m *mockConfig) ConfigClientInfo() map[string]interface{} {
	return map[string]interface{}{"type": "mock"}
}

func newMockConfig(baseURL, token string) *mockConfig {
	return &mockConfig{settings: map[string]interface{}{
		"api.base_url": baseURL,
		"auth.token":   token,
	}}
}

// --- GET ---

func TestAPIClient_GET_Success(t *testing.T) {
	want := map[string]interface{}{"id": "srv-1", "name": "worker"}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	cfg := newMockConfig(srv.URL, "test-token")
	api := controlplane.NewAPIClient(cfg, srv.Client())

	var got map[string]interface{}
	if err := api.GET(context.Background(), "", &got); err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	if got["id"] != "srv-1" {
		t.Errorf("id = %v, want srv-1", got["id"])
	}
}

func TestAPIClient_GET_MissingBaseURL(t *testing.T) {
	cfg := &mockConfig{settings: map[string]interface{}{"auth.token": "tok"}}
	api := controlplane.NewAPIClient(cfg, http.DefaultClient)
	err := api.GET(context.Background(), "/foo", nil)
	if err == nil {
		t.Fatal("expected error when api.base_url is missing")
	}
}

func TestAPIClient_GET_MissingToken(t *testing.T) {
	cfg := &mockConfig{settings: map[string]interface{}{"api.base_url": "http://localhost:9999"}}
	api := controlplane.NewAPIClient(cfg, http.DefaultClient)
	err := api.GET(context.Background(), "/foo", nil)
	if err == nil {
		t.Fatal("expected error when auth.token is missing")
	}
}

func TestAPIClient_GET_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `"not found"`, http.StatusNotFound)
	}))
	defer srv.Close()

	cfg := newMockConfig(srv.URL, "tok")
	api := controlplane.NewAPIClient(cfg, srv.Client())

	var got interface{}
	err := api.GET(context.Background(), "", &got)
	if err == nil {
		t.Fatal("expected error on 404 response")
	}
}

func TestAPIClient_GET_SendsAuthorizationHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer my-secret-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	cfg := newMockConfig(srv.URL, "my-secret-token")
	api := controlplane.NewAPIClient(cfg, srv.Client())

	var got map[string]string
	if err := api.GET(context.Background(), "", &got); err != nil {
		t.Fatalf("GET failed: %v", err)
	}
}

func TestAPIClient_GET_SendsOrgIDHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Organization-ID") != "org-42" {
			http.Error(w, `"missing org header"`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	cfg := &mockConfig{settings: map[string]interface{}{
		"api.base_url":          srv.URL,
		"auth.token":            "tok",
		"local.organization_id": "org-42",
	}}
	api := controlplane.NewAPIClient(cfg, srv.Client())

	var got map[string]string
	if err := api.GET(context.Background(), "", &got); err != nil {
		t.Fatalf("GET failed: %v", err)
	}
}

// --- GETWithParams ---

func TestAPIClient_GETWithParams_AppendsQueryString(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery == "" {
			http.Error(w, `"missing query"`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	cfg := newMockConfig(srv.URL, "tok")
	api := controlplane.NewAPIClient(cfg, srv.Client())

	params := map[string][]string{"status": {"online"}}
	var got map[string]string
	if err := api.GETWithParams(context.Background(), "", params, &got); err != nil {
		t.Fatalf("GETWithParams failed: %v", err)
	}
}

func TestAPIClient_GETWithParams_EmptyParams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	cfg := newMockConfig(srv.URL, "tok")
	api := controlplane.NewAPIClient(cfg, srv.Client())

	var got map[string]string
	if err := api.GETWithParams(context.Background(), "", nil, &got); err != nil {
		t.Fatalf("GETWithParams with nil params failed: %v", err)
	}
}

// --- POST ---

func TestAPIClient_POST_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"created": "true"})
	}))
	defer srv.Close()

	cfg := newMockConfig(srv.URL, "tok")
	api := controlplane.NewAPIClient(cfg, srv.Client())

	var got map[string]string
	if err := api.POST(context.Background(), "", map[string]string{"name": "srv"}, &got); err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	if got["created"] != "true" {
		t.Errorf("created = %q, want true", got["created"])
	}
}

func TestAPIClient_POST_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `"internal error"`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := newMockConfig(srv.URL, "tok")
	api := controlplane.NewAPIClient(cfg, srv.Client())

	err := api.POST(context.Background(), "", nil, nil)
	if err == nil {
		t.Fatal("expected error on 500 response")
	}
}

// --- PATCH ---

func TestAPIClient_PATCH_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("unexpected method %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"updated": "true"})
	}))
	defer srv.Close()

	cfg := newMockConfig(srv.URL, "tok")
	api := controlplane.NewAPIClient(cfg, srv.Client())

	var got map[string]string
	if err := api.PATCH(context.Background(), "", map[string]string{"status": "active"}, &got); err != nil {
		t.Fatalf("PATCH failed: %v", err)
	}
	if got["updated"] != "true" {
		t.Errorf("updated = %q, want true", got["updated"])
	}
}
