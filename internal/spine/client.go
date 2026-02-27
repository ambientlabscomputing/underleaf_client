// Package spine provides the Mycelium Spine client for the underleaf agent.
// It wraps the SDK's bidirectional streaming Client (for subscribing) and Publisher
// (for publishing) behind a simple handler-registration API.
//
// Typical usage:
//
//	c, _ := spine.NewClient(sdkClient, publisher, serverID, orgID)
//	c.Register("commands.run.server.request", func(ctx context.Context, msg spine.Message) {
//	    handler.Handle(ctx, msg.Payload)
//	})
//	c.Start(ctx)
//	defer c.Stop()
package spine

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	umsv1 "github.com/ambientlabscomputing/mycelium_spine/proto/ums/v1"
	"github.com/ambientlabscomputing/mycelium_spine/sdk"
)

// HandlerFunc is called for each incoming envelope whose Type matches the registration.
// Handlers are invoked sequentially inside the dispatch loop; if heavy work is required
// the handler should spawn its own goroutine.
type HandlerFunc func(ctx context.Context, msg Message)

// Client is the agent-side Mycelium Spine client.
// It maintains a single subscription for this server and dispatches
// incoming envelopes to registered type handlers.
type Client struct {
	subscriber *sdk.Client
	publisher  *sdk.Publisher
	serverID   string
	orgID      string

	handlersMu sync.RWMutex
	// handlers maps Envelope.Type → ordered list of HandlerFunc.
	// Multiple handlers can be registered for the same type.
	handlers map[string][]handlerEntry

	nextID uint64 // monotonic counter for handler IDs (protected by handlersMu)
}

type handlerEntry struct {
	id      uint64
	handler HandlerFunc
}

// NewClient constructs a spine Client. Call Register before Start to ensure
// no envelopes are missed.
func NewClient(subscriber *sdk.Client, publisher *sdk.Publisher, serverID, orgID string) *Client {
	return &Client{
		subscriber: subscriber,
		publisher:  publisher,
		serverID:   serverID,
		orgID:      orgID,
		handlers:   make(map[string][]handlerEntry),
	}
}

// Register subscribes handler to envelopes of the given type.
// It returns a deregister function that removes the handler; the caller should
// invoke it (e.g. via defer) when the handler is no longer needed.
func (c *Client) Register(envelopeType string, handler HandlerFunc) (deregister func()) {
	c.handlersMu.Lock()
	c.nextID++
	id := c.nextID
	c.handlers[envelopeType] = append(c.handlers[envelopeType], handlerEntry{id: id, handler: handler})
	c.handlersMu.Unlock()

	return func() {
		c.handlersMu.Lock()
		defer c.handlersMu.Unlock()
		entries := c.handlers[envelopeType]
		for i, e := range entries {
			if e.id == id {
				c.handlers[envelopeType] = append(entries[:i], entries[i+1:]...)
				break
			}
		}
		if len(c.handlers[envelopeType]) == 0 {
			delete(c.handlers, envelopeType)
		}
	}
}

// Start connects to Mycelium Spine, subscribes for this server's messages, and
// begins dispatching envelopes to registered handlers.  It returns immediately
// after the connection is established; the dispatch loop runs in the background.
//
// The caller must call Stop (or cancel the context) to shut down cleanly.
func (c *Client) Start(ctx context.Context) error {
	slog.Info("connecting to Mycelium Spine",
		"server_id", c.serverID,
		"org_id", c.orgID)

	if err := c.subscriber.Connect(ctx); err != nil {
		return fmt.Errorf("spine: connect: %w", err)
	}

	// Subscribe to all messages targeted at this server.
	target := &umsv1.Target{
		TargetType: umsv1.TargetType_TARGET_TYPE_SERVER,
		TargetId:   c.serverID,
		OrgId:      c.orgID,
	}
	if err := c.subscriber.Subscribe([]*umsv1.Target{target}); err != nil {
		return fmt.Errorf("spine: subscribe: %w", err)
	}

	slog.Info("subscribed to Mycelium Spine",
		"server_id", c.serverID,
		"target_type", "SERVER")

	go c.dispatchLoop(ctx)
	return nil
}

// dispatchLoop reads DeliverFrames from the subscriber, converts each envelope
// to a Message, routes it to registered handlers, and acks if required.
func (c *Client) dispatchLoop(ctx context.Context) {
	deliveries := c.subscriber.Deliveries()
	errors := c.subscriber.Errors()

	for {
		select {
		case <-ctx.Done():
			return

		case err, ok := <-errors:
			if !ok {
				return
			}
			slog.Warn("spine: subscriber error", "error", err)

		case frame, ok := <-deliveries:
			if !ok {
				return
			}
			for _, env := range frame.Envelopes {
				c.dispatch(ctx, env)
			}
		}
	}
}

// dispatch routes a single envelope to its registered handlers and acks it.
func (c *Client) dispatch(ctx context.Context, env *umsv1.Envelope) {
	msg := fromEnvelope(env)

	c.handlersMu.RLock()
	entries := c.handlers[env.Type]
	// Copy slice so we don't hold the lock while calling handlers.
	if len(entries) > 0 {
		copied := make([]handlerEntry, len(entries))
		copy(copied, entries)
		c.handlersMu.RUnlock()

		for _, e := range copied {
			e.handler(ctx, msg)
		}
	} else {
		c.handlersMu.RUnlock()
		slog.Debug("spine: no handler for envelope type", "type", env.Type)
	}

	// Ack after all handlers have run.
	if env.RequiresAck && env.MailboxId != "" {
		if err := c.subscriber.Ack(env.MailboxId, env.Seq); err != nil {
			slog.Warn("spine: ack failed",
				"mailbox_id", env.MailboxId,
				"seq", env.Seq,
				"error", err)
		}
	}
}

// Publish sends an envelope to Mycelium Spine.
// The QoS determines which publisher helper is used:
//   - QOS_COMMAND   → PublishCommand  (durable, requires ack)
//   - QOS_CONTROL   → PublishControl  (durable, requires ack)
//   - QOS_TELEMETRY → PublishTelemetry (best-effort, no ack)
//
// If qos is unspecified it defaults to QOS_CONTROL.
func (c *Client) Publish(ctx context.Context, envelopeType string, payload []byte, targets []*umsv1.Target, qos umsv1.QoS, orgID string) error {
	switch qos {
	case umsv1.QoS_QOS_COMMAND:
		_, err := c.publisher.PublishCommand(ctx, envelopeType, payload, orgID, targets)
		return err
	case umsv1.QoS_QOS_TELEMETRY:
		_, err := c.publisher.PublishTelemetry(ctx, envelopeType, payload, orgID, targets)
		return err
	default: // QOS_CONTROL or unspecified
		_, err := c.publisher.PublishControl(ctx, envelopeType, payload, orgID, targets)
		return err
	}
}

// Stop shuts down the subscriber and publisher connections.
func (c *Client) Stop() error {
	slog.Info("stopping Mycelium Spine client")
	subErr := c.subscriber.Close()
	pubErr := c.publisher.Close()
	if subErr != nil {
		return fmt.Errorf("spine: close subscriber: %w", subErr)
	}
	return pubErr
}
