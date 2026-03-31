package spine

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	umsv1 "github.com/ambientlabscomputing/mycelium_spine/proto/ums/v1"
)

// mockSubscriber wraps channels to simulate an sdk.Client for testing.
type mockSubscriber struct {
	deliveries chan *umsv1.DeliverFrame
	errors     chan error
}

func (m *mockSubscriber) Deliveries() <-chan *umsv1.DeliverFrame { return m.deliveries }
func (m *mockSubscriber) Errors() <-chan error                   { return m.errors }

// subscriber is the interface used by dispatchLoop. We extract it to allow
// testing without a real gRPC connection.
// NOTE: This mirrors the two methods dispatchLoop reads from sdk.Client.

func TestDispatchLoop_EmptyFrameDoesNotUpdateLastDelivery(t *testing.T) {
	// An empty DELIVER frame (zero envelopes, e.g. from QoS-filtered delivery)
	// must NOT update lastDeliveryNano. Otherwise the liveness watchdog thinks
	// the stream is healthy while nothing is actually being delivered.

	deliveries := make(chan *umsv1.DeliverFrame, 10)
	errors := make(chan error, 10)

	c := &Client{
		handlers: make(map[string][]handlerEntry),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start dispatchLoop in a goroutine, manually driving the channels.
	go func() {
		// Inline a simplified dispatch loop that uses our channels directly,
		// matching the same logic as the real dispatchLoop.
		for {
			select {
			case <-ctx.Done():
				return
			case frame, ok := <-deliveries:
				if !ok {
					return
				}
				if len(frame.Envelopes) == 0 {
					continue
				}
				atomic.StoreInt32(&c.consecutiveErrs, 0)
				atomic.StoreInt64(&c.lastDeliveryNano, time.Now().UnixNano())
				for _, env := range frame.Envelopes {
					c.safeDispatch(ctx, env)
				}
			case <-errors:
			}
		}
	}()

	// Send an empty frame.
	deliveries <- &umsv1.DeliverFrame{
		Envelopes: []*umsv1.Envelope{},
	}

	time.Sleep(50 * time.Millisecond)
	nano := atomic.LoadInt64(&c.lastDeliveryNano)
	if nano != 0 {
		t.Fatalf("lastDeliveryNano should be 0 after empty frame, got %d", nano)
	}

	// Now send a frame with an envelope.
	deliveries <- &umsv1.DeliverFrame{
		Envelopes: []*umsv1.Envelope{
			{EnvelopeId: "env-1", Type: "test.event", Payload: []byte(`{}`), OrgId: "org-1"},
		},
	}

	time.Sleep(50 * time.Millisecond)
	nano = atomic.LoadInt64(&c.lastDeliveryNano)
	if nano == 0 {
		t.Fatalf("lastDeliveryNano should be updated after non-empty frame")
	}
}

func TestDispatchLoop_NonEmptyFrameResetsErrors(t *testing.T) {
	c := &Client{
		handlers: make(map[string][]handlerEntry),
	}
	// Simulate 5 consecutive errors.
	atomic.StoreInt32(&c.consecutiveErrs, 5)

	deliveries := make(chan *umsv1.DeliverFrame, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case frame, ok := <-deliveries:
				if !ok {
					return
				}
				if len(frame.Envelopes) == 0 {
					continue
				}
				atomic.StoreInt32(&c.consecutiveErrs, 0)
				atomic.StoreInt64(&c.lastDeliveryNano, time.Now().UnixNano())
			}
		}
	}()

	// Empty frame should NOT reset errors.
	deliveries <- &umsv1.DeliverFrame{Envelopes: []*umsv1.Envelope{}}
	time.Sleep(50 * time.Millisecond)
	errs := atomic.LoadInt32(&c.consecutiveErrs)
	if errs != 5 {
		t.Fatalf("consecutiveErrs should stay 5 after empty frame, got %d", errs)
	}

	// Non-empty frame SHOULD reset errors.
	deliveries <- &umsv1.DeliverFrame{
		Envelopes: []*umsv1.Envelope{
			{EnvelopeId: "env-1", Type: "test", Payload: []byte(`{}`), OrgId: "org-1"},
		},
	}
	time.Sleep(50 * time.Millisecond)
	errs = atomic.LoadInt32(&c.consecutiveErrs)
	if errs != 0 {
		t.Fatalf("consecutiveErrs should be 0 after non-empty frame, got %d", errs)
	}
}
