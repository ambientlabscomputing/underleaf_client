package agent_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ambientlabscomputing/underleaf_client/internal/agent"
	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/policy_manager"
	"github.com/ambientlabscomputing/underleaf_client/internal/spine"
)

type mockAgentCfg struct{ settings map[string]interface{} }

func (m *mockAgentCfg) Get(key string) (interface{}, bool) {
	v, ok := m.settings[key]
	return v, ok
}

func (m *mockAgentCfg) Set(key string, value interface{}) error {
	m.settings[key] = value
	return nil
}

func (m *mockAgentCfg) Delete(key string) error {
	delete(m.settings, key)
	return nil
}

func (m *mockAgentCfg) Config() policy_manager.Configuration {
	return policy_manager.Configuration{Payload: m.settings}
}

func (m *mockAgentCfg) ConfigClientInfo() map[string]interface{} {
	return map[string]interface{}{"type": "mock"}
}

func newExposureClientFor(t *testing.T, srv *httptest.Server) *controlplane.CPlaneExposureClient {
	t.Helper()
	cfg := &mockAgentCfg{settings: map[string]interface{}{
		"api.base_url": srv.URL,
		"auth.token":   "test-token",
	}}
	api := controlplane.NewAPIClient(cfg, srv.Client())
	return controlplane.NewCPlaneExposureClient(api)
}

func makeBindMsg(exposureID, status, publicURL, errMsg string) spine.Message {
	payload := agent.ExposureBindCompletedPayload{
		ExposureID: exposureID,
		LeaseID:    "lease-001",
		Status:     status,
		PublicURL:  publicURL,
		Error:      errMsg,
	}
	b, _ := json.Marshal(payload)
	return spine.Message{Payload: b}
}

func makeUnbindMsg(exposureID, errMsg string) spine.Message {
	payload := agent.ExposureUnbindCompletedPayload{ExposureID: exposureID, Error: errMsg}
	b, _ := json.Marshal(payload)
	return spine.Message{Payload: b}
}

func TestHandleExposureBindCompleted_NilRaftNilClient(t *testing.T) {
	msg := makeBindMsg("exp-001", "bound", "https://web-abc12345.underleafapp.com", "")
	if err := agent.HandleExposureBindCompleted(context.Background(), msg, nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleExposureBindCompleted_PostsResultToServerAPI(t *testing.T) {
	var capturedBody map[string]interface{}
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.WriteHeader(204)
	}))
	defer srv.Close()
	ec := newExposureClientFor(t, srv)
	msg := makeBindMsg("exp-001", "bound", "https://web-abc12345.underleafapp.com", "")
	if err := agent.HandleExposureBindCompleted(context.Background(), msg, nil, ec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedPath != "/exposures/exp-001/results" {
		t.Errorf("expected path /exposures/exp-001/results, got %q", capturedPath)
	}
	if capturedBody["status"] != "bound" {
		t.Errorf("expected status=bound, got %v", capturedBody["status"])
	}
	if capturedBody["public_url"] != "https://web-abc12345.underleafapp.com" {
		t.Errorf("expected public_url in body, got %v", capturedBody["public_url"])
	}
}

func TestHandleExposureBindCompleted_ErrorStatus(t *testing.T) {
	var capturedBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.WriteHeader(204)
	}))
	defer srv.Close()
	ec := newExposureClientFor(t, srv)
	msg := makeBindMsg("exp-002", "error", "", "tunnel dial failed")
	if err := agent.HandleExposureBindCompleted(context.Background(), msg, nil, ec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedBody["status"] != "error" {
		t.Errorf("expected status=error, got %v", capturedBody["status"])
	}
	if capturedBody["error"] != "tunnel dial failed" {
		t.Errorf("expected error in body, got %v", capturedBody["error"])
	}
}

func TestHandleExposureBindCompleted_ServerAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	ec := newExposureClientFor(t, srv)
	msg := makeBindMsg("exp-003", "bound", "https://web-abc12345.underleafapp.com", "")
	if err := agent.HandleExposureBindCompleted(context.Background(), msg, nil, ec); err == nil {
		t.Fatal("expected error when server_api returns 500, got nil")
	}
}

func TestHandleExposureBindCompleted_BadPayload(t *testing.T) {
	badMsg := spine.Message{Payload: []byte("not-json")}
	if err := agent.HandleExposureBindCompleted(context.Background(), badMsg, nil, nil); err == nil {
		t.Fatal("expected error for bad JSON payload, got nil")
	}
}

func TestHandleExposureUnbindCompleted_NilRaft(t *testing.T) {
	msg := makeUnbindMsg("exp-004", "")
	if err := agent.HandleExposureUnbindCompleted(context.Background(), msg, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleExposureUnbindCompleted_WithError(t *testing.T) {
	msg := makeUnbindMsg("exp-005", "tunnel close timed out")
	if err := agent.HandleExposureUnbindCompleted(context.Background(), msg, nil); err != nil {
		t.Fatalf("expected nil error for unbind with error payload; got %v", err)
	}
}

func TestHandleExposureUnbindCompleted_BadPayload(t *testing.T) {
	badMsg := spine.Message{Payload: []byte("{{invalid")}
	if err := agent.HandleExposureUnbindCompleted(context.Background(), badMsg, nil); err == nil {
		t.Fatal("expected error for bad JSON payload, got nil")
	}
}
