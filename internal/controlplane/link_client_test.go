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

func newTestLinkClient(t *testing.T, srv *httptest.Server) *controlplane.CPlaneLinkClient {
	t.Helper()
	cfg := newExposureMockConfig(srv.URL, "test-token")
	api := controlplane.NewAPIClient(cfg, srv.Client())
	return controlplane.NewCPlaneLinkClient(api)
}

func TestCPlaneLinkClient_QueryLinks_NoFilters(t *testing.T) {
	want := controlplane.QueryLinksResponse{
		Links: []*controlplane.LinkRecord{
			{ID: "exp-1", Kind: "exposure", Visibility: "public", Status: "success"},
			{ID: "tun-1", Kind: "tunnel", Visibility: "public", Status: "success"},
			{ID: "ch-1", Kind: "channel", Visibility: "peer", Status: "success"},
		},
		Total:        3,
		CountsByKind: map[string]int{"exposure": 1, "tunnel": 1, "channel": 1},
	}

	var capturedURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	client := newTestLinkClient(t, srv)
	got, err := client.QueryLinks(context.Background(), "", "", "", "", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Total != 3 {
		t.Errorf("Total = %d, want 3", got.Total)
	}
	if len(got.Links) != 3 {
		t.Errorf("len(Links) = %d, want 3", len(got.Links))
	}
	if capturedURL != "/links" {
		t.Errorf("URL = %q, want /links", capturedURL)
	}
	if got.CountsByKind["exposure"] != 1 {
		t.Errorf("CountsByKind = %v", got.CountsByKind)
	}
}

func TestCPlaneLinkClient_QueryLinks_WithFilters(t *testing.T) {
	var capturedURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(controlplane.QueryLinksResponse{Total: 0})
	}))
	defer srv.Close()

	client := newTestLinkClient(t, srv)
	_, err := client.QueryLinks(context.Background(), "channel", "peer", "success", "srv-1", 10, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, param := range []string{"kind=channel", "visibility=peer", "status=success", "server_id=srv-1", "limit=10", "offset=20"} {
		if !strings.Contains(capturedURL, param) {
			t.Errorf("URL %q missing expected param %q", capturedURL, param)
		}
	}
}

func TestCPlaneLinkClient_QueryLinks_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := newTestLinkClient(t, srv)
	_, err := client.QueryLinks(context.Background(), "", "", "", "", 0, 0)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
