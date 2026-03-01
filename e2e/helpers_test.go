//go:build e2e

package e2e

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
)

const (
	envAPIURL  = "E2E_API_URL"
	envSpineEP = "E2E_SPINE_EP"
	envAuthTok = "E2E_AUTH_TOKEN"
)

func apiURL() string {
	if v := os.Getenv(envAPIURL); v != "" {
		return v
	}
	return "http://localhost:8080"
}

func spineEndpoint() string {
	if v := os.Getenv(envSpineEP); v != "" {
		return v
	}
	return "localhost:9090"
}

func authToken() string {
	return os.Getenv(envAuthTok)
}

func requireAPI(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", apiURL()+"/health", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Skipf("API unreachable at %s: %v", apiURL(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		t.Skipf("API health returned %d", resp.StatusCode)
	}
}

func requireSpine(t *testing.T) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", spineEndpoint(), 3*time.Second)
	if err != nil {
		t.Skipf("Spine unreachable at %s: %v", spineEndpoint(), err)
	}
	conn.Close()
}

func requireAuth(t *testing.T) {
	t.Helper()
	if authToken() == "" {
		t.Skip("E2E_AUTH_TOKEN not set")
	}
}

func newE2EConfigClient(t *testing.T) policy_manager.ConfigClient {
	t.Helper()
	path := filepath.Join(t.TempDir(), "e2e-config.yaml")
	c := policy_manager.NewCLIConfigClientFromPath(path)
	c.Set("api.base_url", apiURL()+"/api/v1/servers")
	if tok := authToken(); tok != "" {
		c.Set("auth.token", tok)
	}
	c.Set("mycelium_spine.endpoint", spineEndpoint())
	return c
}
