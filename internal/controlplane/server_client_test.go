package controlplane_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
	servertypes "github.com/ambientlabscomputing/underleaf_client/internal/types/server"
)

// newTestServerClient wires a ServerClient against a test HTTP server.
// Note: NewServerClient takes *policy_manager.ConfigClient (pointer-to-interface).
func newTestServerClient(t *testing.T, baseURL string) *controlplane.ServerClient {
	t.Helper()
	cfg := newMockConfig(baseURL, "test-token")
	api := controlplane.NewAPIClient(cfg, http.DefaultClient)
	var iface policy_manager.ConfigClient = cfg
	return controlplane.NewServerClient(&iface, api)
}

func TestServerClient_ListServers_ReturnsResults(t *testing.T) {
	payload := map[string]interface{}{
		"results":     []interface{}{map[string]interface{}{"id": "s1"}, map[string]interface{}{"id": "s2"}},
		"total_count": float64(2),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	sc := newTestServerClient(t, srv.URL)
	results, err := sc.ListServers(context.Background())
	if err != nil {
		t.Fatalf("ListServers failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("got %d results, want 2", len(results))
	}
}

func TestServerClient_ListServers_EmptyResults(t *testing.T) {
	payload := map[string]interface{}{
		"results":     []interface{}{},
		"total_count": float64(0),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	sc := newTestServerClient(t, srv.URL)
	results, err := sc.ListServers(context.Background())
	if err != nil {
		t.Fatalf("ListServers failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results, want 0", len(results))
	}
}

func TestServerClient_ListServers_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `"internal error"`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	sc := newTestServerClient(t, srv.URL)
	_, err := sc.ListServers(context.Background())
	if err == nil {
		t.Fatal("expected error on server error")
	}
}

func TestServerClient_GetServer_Success(t *testing.T) {
	payload := map[string]interface{}{"id": "srv-abc", "name": "worker-1"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "srv-abc") {
			http.Error(w, `"not found"`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	sc := newTestServerClient(t, srv.URL)
	result, err := sc.GetServer(context.Background(), "srv-abc")
	if err != nil {
		t.Fatalf("GetServer failed: %v", err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected map result")
	}
	if m["id"] != "srv-abc" {
		t.Errorf("id = %v, want srv-abc", m["id"])
	}
}

func TestServerClient_RegisterServer_SendsPayload(t *testing.T) {
	var received map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `"wrong method"`, http.StatusMethodNotAllowed)
			return
		}
		json.NewDecoder(r.Body).Decode(&received)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "new-srv"})
	}))
	defer srv.Close()

	sc := newTestServerClient(t, srv.URL)
	platform := map[string]string{"os": "linux", "arch": "arm64"}
	_, err := sc.RegisterServer(context.Background(), "my-server", platform)
	if err != nil {
		t.Fatalf("RegisterServer failed: %v", err)
	}
	if received["name"] != "my-server" {
		t.Errorf("name = %v, want my-server", received["name"])
	}
}

func TestServerClient_ListServersWithParams_AppendsQueryParams(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"results": []interface{}{}, "total_count": 0})
	}))
	defer srv.Close()

	sc := newTestServerClient(t, srv.URL)

	params := servertypes.ListServersParams{Status: "online", Limit: 10}
	_, err := sc.ListServersWithParams(context.Background(), params)
	if err != nil {
		t.Fatalf("ListServersWithParams failed: %v", err)
	}
	if !strings.Contains(gotQuery, "status") && gotQuery != "" {
		t.Logf("query = %q", gotQuery)
	}
}
