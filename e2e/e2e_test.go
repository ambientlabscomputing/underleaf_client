//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
	"github.com/ambientlabscomputing/underleaf_client/pkg/defaults"
)

func TestE2E_APIHealthCheck(t *testing.T) {
	requireAPI(t)
	resp, err := http.Get(apiURL() + "/health")
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want 200", resp.StatusCode)
	}
	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	t.Logf("API health: %v", body)
}

func TestE2E_SpineReachable(t *testing.T) {
	requireSpine(t)
	t.Log("Spine endpoint is reachable")
}

func TestE2E_DefaultsMatchLocalEndpoints(t *testing.T) {
	if defaults.APIBaseURL == "" {
		t.Error("defaults.APIBaseURL is empty")
	}
	if defaults.SpineEndpoint == "" {
		t.Error("defaults.SpineEndpoint is empty")
	}
	t.Logf("APIBaseURL=%s SpineEndpoint=%s UCRSBaseURL=%s",
		defaults.APIBaseURL, defaults.SpineEndpoint, defaults.UCRSBaseURL)
}

func TestE2E_APIClient_ListServers(t *testing.T) {
	requireAPI(t)
	requireAuth(t)
	cfg := newE2EConfigClient(t)
	api := controlplane.NewAPIClient(cfg, http.DefaultClient)
	var result struct {
		Results    []interface{} `json:"results"`
		TotalCount int           `json:"total_count"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := api.GET(ctx, "", &result); err != nil {
		t.Fatalf("ListServers GET failed: %v", err)
	}
	t.Logf("servers found: %d (total_count=%d)", len(result.Results), result.TotalCount)
}

func TestE2E_APIClient_Unauthenticated(t *testing.T) {
	requireAPI(t)
	path := filepath.Join(t.TempDir(), "cfg.yaml")
	c := policy_manager.NewCLIConfigClientFromPath(path)
	c.Set("api.base_url", apiURL()+"/api/v1/servers")
	api := controlplane.NewAPIClient(c, http.DefaultClient)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var result interface{}
	err := api.GET(ctx, "", &result)
	if err == nil {
		t.Log("API allowed unauthenticated request")
	} else {
		t.Logf("API rejected unauthenticated request: %v", err)
	}
}

func TestE2E_ServerClient_ListServers(t *testing.T) {
	requireAPI(t)
	requireAuth(t)
	cfg := newE2EConfigClient(t)
	api := controlplane.NewAPIClient(cfg, http.DefaultClient)
	var iface policy_manager.ConfigClient = cfg
	sc := controlplane.NewServerClient(&iface, api)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, err := sc.ListServers(ctx)
	if err != nil {
		t.Fatalf("ServerClient.ListServers failed: %v", err)
	}
	t.Logf("ServerClient returned %d servers", len(results))
	if len(results) > 0 {
		first, ok := results[0].(map[string]interface{})
		if !ok {
			t.Fatal("first result is not a map")
		}
		if _, hasID := first["id"]; !hasID {
			t.Error("first server missing 'id' field")
		}
		t.Logf("first server: id=%v name=%v", first["id"], first["name"])
	}
}

func TestE2E_ServerClient_GetServer_NotFound(t *testing.T) {
	requireAPI(t)
	requireAuth(t)
	cfg := newE2EConfigClient(t)
	api := controlplane.NewAPIClient(cfg, http.DefaultClient)
	var iface policy_manager.ConfigClient = cfg
	sc := controlplane.NewServerClient(&iface, api)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := sc.GetServer(ctx, "nonexistent-server-id")
	if err == nil {
		t.Log("API returned result for nonexistent server ID")
	} else {
		t.Logf("correctly returned error: %v", err)
	}
}

func TestE2E_ConfigClient_RoundTrip(t *testing.T) {
	cfg := newE2EConfigClient(t)
	testKey := fmt.Sprintf("e2e_test.marker_%d", time.Now().UnixNano())
	if err := cfg.Set(testKey, "hello-e2e"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	val, ok := cfg.Get(testKey)
	if !ok {
		t.Fatal("Get returned ok=false after Set")
	}
	if val != "hello-e2e" {
		t.Errorf("Get = %v, want hello-e2e", val)
	}
	if err := cfg.Delete(testKey); err != nil {
		t.Logf("Delete error (may be expected): %v", err)
	}
}
