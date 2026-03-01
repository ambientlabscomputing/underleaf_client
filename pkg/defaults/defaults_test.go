package defaults_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/ambientlabscomputing/underleaf_client/pkg/defaults"
)

func TestDefaultValues_NonEmpty(t *testing.T) {
	if defaults.APIBaseURL == "" {
		t.Error("APIBaseURL must not be empty")
	}
	if defaults.SpineEndpoint == "" {
		t.Error("SpineEndpoint must not be empty")
	}
	if defaults.UCRSBaseURL == "" {
		t.Error("UCRSBaseURL must not be empty")
	}
}

func TestDefaultAPIBaseURL_IsValidURL(t *testing.T) {
	u, err := url.Parse(defaults.APIBaseURL)
	if err != nil {
		t.Fatalf("APIBaseURL invalid: %v", err)
	}
	if u.Scheme == "" {
		t.Errorf("APIBaseURL missing scheme: %q", defaults.APIBaseURL)
	}
	if u.Host == "" {
		t.Errorf("APIBaseURL missing host: %q", defaults.APIBaseURL)
	}
}

func TestDefaultUCRSBaseURL_IsValidURL(t *testing.T) {
	u, err := url.Parse(defaults.UCRSBaseURL)
	if err != nil {
		t.Fatalf("UCRSBaseURL invalid: %v", err)
	}
	if u.Scheme == "" {
		t.Errorf("UCRSBaseURL missing scheme: %q", defaults.UCRSBaseURL)
	}
	if u.Host == "" {
		t.Errorf("UCRSBaseURL missing host: %q", defaults.UCRSBaseURL)
	}
}

func TestDefaultSpineEndpoint_IsHostPort(t *testing.T) {
	ep := defaults.SpineEndpoint
	if strings.HasPrefix(ep, "http://") || strings.HasPrefix(ep, "https://") || strings.HasPrefix(ep, "wss://") {
		t.Errorf("SpineEndpoint must be host:port, not a URL: %q", ep)
	}
	if !strings.Contains(ep, ":") {
		t.Errorf("SpineEndpoint must contain port (host:port): %q", ep)
	}
}

func TestDefaultLocalValues(t *testing.T) {
	if defaults.APIBaseURL != "http://localhost:8080/api/v1/servers" {
		t.Errorf("local-dev APIBaseURL = %q, want http://localhost:8080/api/v1/servers", defaults.APIBaseURL)
	}
	if defaults.SpineEndpoint != "localhost:9090" {
		t.Errorf("local-dev SpineEndpoint = %q, want localhost:9090", defaults.SpineEndpoint)
	}
	if defaults.UCRSBaseURL != "http://localhost:8080/api/v1/registry" {
		t.Errorf("local-dev UCRSBaseURL = %q, want http://localhost:8080/api/v1/registry", defaults.UCRSBaseURL)
	}
}
