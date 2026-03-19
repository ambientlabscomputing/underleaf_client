package agent_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ambientlabscomputing/underleaf_client/internal/agent"
	"github.com/ambientlabscomputing/underleaf_client/internal/spine"
)

// makeChannelBindMsg constructs a spine.Message with a JSON-encoded ChannelBindSpineRequest payload.
func makeChannelBindMsg(channelID, orgID, role, grant, srcID, dstID, tunnelAddr string) spine.Message {
	payload := agent.ChannelBindSpineRequest{
		ChannelID:        channelID,
		OrgID:            orgID,
		Role:             role,
		Grant:            grant,
		SourceServerID:   srcID,
		DestServerID:     dstID,
		Purpose:          "test",
		HyphaeTunnelAddr: tunnelAddr,
		ExpiresAt:        9999999999,
		CreatedAt:        1000000000,
	}
	b, _ := json.Marshal(payload)
	return spine.Message{Payload: b}
}

// newTestEventStreamServer creates a lightweight EventStreamServer suitable for unit tests.
// It uses in-memory buffers; no network port is opened.
func newTestEventStreamServer() *agent.EventStreamServer {
	return agent.NewEventStreamServer("", "cluster-test", "node-test", nil)
}

// TestHandleChannelBindRequested_NilEventServer verifies an error is returned when
// no EventStreamServer is available to relay the event to MMA.
func TestHandleChannelBindRequested_NilEventServer(t *testing.T) {
	msg := makeChannelBindMsg("ch-001", "org-1", "listener", "", "srv-a", "srv-b", "hyphae:9090")
	err := agent.HandleChannelBindRequested(context.Background(), msg, nil)
	if err == nil {
		t.Fatal("expected error when eventServer is nil, got nil")
	}
}

// TestHandleChannelBindRequested_MalformedPayload verifies an error is returned for
// invalid JSON in the Spine message payload.
func TestHandleChannelBindRequested_MalformedPayload(t *testing.T) {
	msg := spine.Message{Payload: []byte("{invalid json")}
	es := newTestEventStreamServer()
	err := agent.HandleChannelBindRequested(context.Background(), msg, es)
	if err == nil {
		t.Fatal("expected error for malformed payload, got nil")
	}
}

// TestHandleChannelBindRequested_ForwardsListenerRole verifies that a listener-role
// bind request is forwarded to the EventStreamServer without error.
func TestHandleChannelBindRequested_ForwardsListenerRole(t *testing.T) {
	msg := makeChannelBindMsg("ch-002", "org-1", "listener", "", "srv-a", "srv-b", "hyphae.example.com:9090")
	es := newTestEventStreamServer()
	if err := agent.HandleChannelBindRequested(context.Background(), msg, es); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestHandleChannelBindRequested_ForwardsInitiatorRole verifies that an initiator-role
// bind request (which carries a grant JWT) is forwarded to the EventStreamServer.
func TestHandleChannelBindRequested_ForwardsInitiatorRole(t *testing.T) {
	msg := makeChannelBindMsg("ch-003", "org-1", "initiator", "eyJ.example.jwt", "srv-a", "srv-b", "hyphae.example.com:9090")
	es := newTestEventStreamServer()
	if err := agent.HandleChannelBindRequested(context.Background(), msg, es); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
