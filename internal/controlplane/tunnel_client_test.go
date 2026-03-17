package controlplane_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
)

// newTestTunnelClient wires a CPlaneTunnelClient backed by the given test server.
func newTestTunnelClient(t *testing.T, srv *httptest.Server) *controlplane.CPlaneTunnelClient {
	t.Helper()
	cfg := newExposureMockConfig(srv.URL, "test-token")
	api := controlplane.NewAPIClient(cfg, srv.Client())
	return controlplane.NewCPlaneTunnelClient(api)
}

// ===================== CreateTunnel =====================

func TestCPlaneTunnelClient_CreateTunnel_Success(t *testing.T) {
	want := controlplane.CreateTunnelResponse{
		TunnelRecord: controlplane.TunnelRecord{
			ID:         "tun-001",
			Target:     "8080",
			TargetType: "port",
			Hostname:   "tunnel-aabbccdd",
			Status:     "pending",
		},
		HyphaeTunnelAddr: "hyphae.internal:9000",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/tunnels" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	client := newTestTunnelClient(t, srv)
	got, err := client.CreateTunnel(context.Background(), controlplane.CreateTunnelRequest{Target: "8080"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "tun-001" {
		t.Errorf("ID = %q, want tun-001", got.ID)
	}
	if got.HyphaeTunnelAddr != "hyphae.internal:9000" {
		t.Errorf("HyphaeTunnelAddr = %q, want hyphae.internal:9000", got.HyphaeTunnelAddr)
	}
}

func TestCPlaneTunnelClient_CreateTunnel_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := newTestTunnelClient(t, srv)
	_, err := client.CreateTunnel(context.Background(), controlplane.CreateTunnelRequest{Target: "bad"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ===================== GetTunnel =====================

func TestCPlaneTunnelClient_GetTunnel_Success(t *testing.T) {
	want := controlplane.TunnelRecord{ID: "tun-002", Target: "9090", Status: "bound"}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tunnels/tun-002" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	client := newTestTunnelClient(t, srv)
	got, err := client.GetTunnel(context.Background(), "tun-002")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "tun-002" {
		t.Errorf("ID = %q, want tun-002", got.ID)
	}
	if got.Status != "bound" {
		t.Errorf("Status = %q, want bound", got.Status)
	}
}

func TestCPlaneTunnelClient_GetTunnel_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := newTestTunnelClient(t, srv)
	_, err := client.GetTunnel(context.Background(), "ghost")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ===================== QueryTunnels =====================

func TestCPlaneTunnelClient_QueryTunnels_NoFilters(t *testing.T) {
	want := controlplane.QueryTunnelsResponse{
		Tunnels: []*controlplane.TunnelRecord{
			{ID: "tun-001", Status: "bound"},
			{ID: "tun-002", Status: "pending"},
		},
		Total: 2,
	}

	var capturedURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	client := newTestTunnelClient(t, srv)
	got, err := client.QueryTunnels(context.Background(), "", "", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Total != 2 {
		t.Errorf("Total = %d, want 2", got.Total)
	}
	if capturedURL != "/tunnels" {
		t.Errorf("URL = %q, want /tunnels", capturedURL)
	}
}

func TestCPlaneTunnelClient_QueryTunnels_WithFilters(t *testing.T) {
	var capturedURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(controlplane.QueryTunnelsResponse{Total: 0})
	}))
	defer srv.Close()

	client := newTestTunnelClient(t, srv)
	_, err := client.QueryTunnels(context.Background(), "srv-1", "bound", 10, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, param := range []string{"server_id=srv-1", "status=bound", "limit=10", "offset=20"} {
		if !strings.Contains(capturedURL, param) {
			t.Errorf("URL %q missing expected param %q", capturedURL, param)
		}
	}
}

// ===================== CloseTunnel =====================

func TestCPlaneTunnelClient_CloseTunnel_Success(t *testing.T) {
	var capturedMethod, capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := newTestTunnelClient(t, srv)
	err := client.CloseTunnel(context.Background(), "tun-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", capturedMethod)
	}
	if capturedPath != "/tunnels/tun-001" {
		t.Errorf("path = %q, want /tunnels/tun-001", capturedPath)
	}
}

func TestCPlaneTunnelClient_CloseTunnel_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := newTestTunnelClient(t, srv)
	err := client.CloseTunnel(context.Background(), "tun-bad")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ===================== PostTunnelResult =====================

func TestCPlaneTunnelClient_PostTunnelResult_Bound(t *testing.T) {
	var captured map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tunnels/tun-001/results" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := newTestTunnelClient(t, srv)
	err := client.PostTunnelResult(context.Background(), "tun-001", "bound", "https://tun.example.com", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured["status"] != "bound" {
		t.Errorf("status = %v, want bound", captured["status"])
	}
	if captured["public_url"] != "https://tun.example.com" {
		t.Errorf("public_url = %v", captured["public_url"])
	}
}

func TestCPlaneTunnelClient_PostTunnelResult_Error(t *testing.T) {
	var captured map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := newTestTunnelClient(t, srv)
	err := client.PostTunnelResult(context.Background(), "tun-002", "error", "", "dial timeout")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured["status"] != "error" {
		t.Errorf("status = %v, want error", captured["status"])
	}
	if captured["error"] != "dial timeout" {
		t.Errorf("error = %v, want dial timeout", captured["error"])
	}
}
