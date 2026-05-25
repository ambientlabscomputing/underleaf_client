package agent_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ambientlabscomputing/underleaf_client/internal/agent"
	"github.com/ambientlabscomputing/underleaf_client/internal/spine"
)

// makeLinkBindMsg constructs a spine.Message with a JSON-encoded LinkSpineBindRequest payload.
func makeLinkBindMsg(linkID, kind, orgID, role, srcID, dstID, tunnelAddr string) spine.Message {
	payload := agent.LinkSpineBindRequest{
		LinkID:           linkID,
		Kind:             kind,
		OrgID:            orgID,
		HyphaeTunnelAddr: tunnelAddr,
		Spec: agent.LinkSpineBindSpec{
			Role:           role,
			SourceServerID: srcID,
			DestServerID:   dstID,
			Purpose:        "test",
			ExpiresAt:      9999999999,
			CreatedAt:      1000000000,
		},
	}
	b, _ := json.Marshal(payload)
	return spine.Message{Payload: b}
}

// newTestEventStreamServer creates a lightweight EventStreamServer suitable for unit tests.
// It uses in-memory buffers; no network port is opened.
func newTestEventStreamServer() *agent.EventStreamServer {
	return agent.NewEventStreamServer("", "cluster-test", "node-test", nil)
}

// TestHandleLinkBindRequested_NilEventServer verifies an error is returned when
// no EventStreamServer is available to relay the event to MMA.
func TestHandleLinkBindRequested_NilEventServer(t *testing.T) {
	msg := makeLinkBindMsg("ch-001", "channel", "org-1", "listener", "srv-a", "srv-b", "hyphae:9090")
	err := agent.HandleLinkBindRequested(context.Background(), msg, nil, nil)
	if err == nil {
		t.Fatal("expected error when eventServer is nil, got nil")
	}
}

// TestHandleLinkBindRequested_MalformedPayload verifies an error is returned for
// invalid JSON in the Spine message payload.
func TestHandleLinkBindRequested_MalformedPayload(t *testing.T) {
	msg := spine.Message{Payload: []byte("{invalid json")}
	es := newTestEventStreamServer()
	err := agent.HandleLinkBindRequested(context.Background(), msg, nil, es)
	if err == nil {
		t.Fatal("expected error for malformed payload, got nil")
	}
}

// TestHandleLinkBindRequested_ChannelListenerRole verifies that a listener-role
// channel bind request is forwarded to the EventStreamServer without error.
func TestHandleLinkBindRequested_ChannelListenerRole(t *testing.T) {
	msg := makeLinkBindMsg("ch-002", "channel", "org-1", "listener", "srv-a", "srv-b", "hyphae.example.com:9090")
	es := newTestEventStreamServer()
	if err := agent.HandleLinkBindRequested(context.Background(), msg, nil, es); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestHandleLinkBindRequested_ChannelInitiatorRole verifies that an initiator-role
// channel bind request (which carries a grant JWT) is forwarded to the EventStreamServer.
func TestHandleLinkBindRequested_ChannelInitiatorRole(t *testing.T) {
	msg := makeLinkBindMsg("ch-003", "channel", "org-1", "initiator", "srv-a", "srv-b", "hyphae.example.com:9090")
	es := newTestEventStreamServer()
	if err := agent.HandleLinkBindRequested(context.Background(), msg, nil, es); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestHandleLinkBindRequested_ExposureKind verifies that an exposure-kind bind
// request is forwarded to the EventStreamServer without error.
func TestHandleLinkBindRequested_ExposureKind(t *testing.T) {
	payload := agent.LinkSpineBindRequest{
		LinkID:           "exp-001",
		Kind:             "exposure",
		OrgID:            "org-1",
		Hostname:         "my-service.underleafapp.com",
		HyphaeTunnelAddr: "hyphae.example.com:9090",
		Spec: agent.LinkSpineBindSpec{
			LeaseID:    "lease-001",
			TargetPort: 8080,
			LocalAddr:  "localhost:8080",
		},
	}
	b, _ := json.Marshal(payload)
	msg := spine.Message{Payload: b}
	es := newTestEventStreamServer()
	if err := agent.HandleLinkBindRequested(context.Background(), msg, nil, es); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// PublishChannelRouteRegister tests (unchanged — still valid)
// ---------------------------------------------------------------------------

// TestPublishChannelRouteRegister_NoError verifies that the publish helper
// constructs and emits the event without error (even when there are no
// subscribers — the EventStreamServer buffers events).
func TestPublishChannelRouteRegister_NoError(t *testing.T) {
	es := newTestEventStreamServer()
	err := es.PublishChannelRouteRegister("secret-replication", "127.0.0.1:46749")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestPublishChannelRouteRegister_EmptyPurpose verifies that the helper does
// not panic or error at the publish layer for edge-case inputs. Validation
// of empty purpose/addr happens on the MMA handler side.
func TestPublishChannelRouteRegister_EmptyPurpose(t *testing.T) {
	es := newTestEventStreamServer()
	err := es.PublishChannelRouteRegister("", "127.0.0.1:1234")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestPublishChannelRouteRegister_EmptyAddr verifies edge case.
func TestPublishChannelRouteRegister_EmptyAddr(t *testing.T) {
	es := newTestEventStreamServer()
	err := es.PublishChannelRouteRegister("secret-replication", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
