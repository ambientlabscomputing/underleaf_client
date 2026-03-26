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
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

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

	// Spine health metrics — updated atomically by dispatchLoop.
	// lastDeliveryNano is Unix nanoseconds of the last successfully dispatched
	// frame (0 = never). consecutiveErrs counts subscriber errors since the
	// last successful delivery; it is reset to 0 on each delivery.
	// When consecutiveErrs reaches errWarnThreshold the severity escalates from
	// WARN → ERROR so operators can distinguish transient blips from a stuck
	// reconnect loop.
	lastDeliveryNano int64 // accessed via sync/atomic
	consecutiveErrs  int32 // accessed via sync/atomic
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
	go c.livenessWatchdog(ctx)
	return nil
}

// errWarnThreshold is the number of consecutive subscriber errors after which
// the dispatchLoop escalates its log severity from WARN to ERROR. This lets
// operators easily spot a stuck reconnect loop vs. a momentary blip.
const errWarnThreshold = 10

// SpineStatusSnapshot is a point-in-time view of the Spine client's health.
type SpineStatusSnapshot struct {
	// ConsecutiveErrors is the number of subscriber errors since the last
	// successful delivery. Resets to zero on each delivery.
	ConsecutiveErrors int32
	// LastDeliveryAt is when the last envelope was successfully dispatched.
	// Zero value means no delivery has ever been observed.
	LastDeliveryAt time.Time
	// IsHealthy is false when ConsecutiveErrors >= errWarnThreshold, indicating
	// the SDK is likely stuck in a failed reconnect loop.
	IsHealthy bool
}

// SpineStatus returns a point-in-time health snapshot for monitoring/alerting.
func (c *Client) SpineStatus() SpineStatusSnapshot {
	errs := atomic.LoadInt32(&c.consecutiveErrs)
	nano := atomic.LoadInt64(&c.lastDeliveryNano)
	var lastDel time.Time
	if nano != 0 {
		lastDel = time.Unix(0, nano)
	}
	return SpineStatusSnapshot{
		ConsecutiveErrors: errs,
		LastDeliveryAt:    lastDel,
		IsHealthy:         errs < errWarnThreshold,
	}
}

// dispatchLoop reads DeliverFrames from the subscriber, converts each envelope
// to a Message, routes it to registered handlers, and acks if required.
// It is resilient to transient errors and handler panics — individual failures
// are logged but the loop continues running.
func (c *Client) dispatchLoop(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("spine: dispatchLoop recovered from panic — restarting",
				"panic", r,
				"stack", string(debug.Stack()))
			// Restart the dispatch loop so the agent keeps receiving commands.
			go c.dispatchLoop(ctx)
		}
	}()

	deliveries := c.subscriber.Deliveries()
	errors := c.subscriber.Errors()

	for {
		select {
		case <-ctx.Done():
			return

		case err, ok := <-errors:
			if !ok {
				if ctx.Err() != nil {
					return // context cancelled — normal shutdown
				}
				// Error channel closed unexpectedly (e.g. Spine reconnect).
				// Restart so the next invocation picks up fresh channels.
				slog.Warn("spine: error channel closed unexpectedly — restarting dispatch loop")
				go c.dispatchLoop(ctx)
				return
			}
			// Track consecutive errors to distinguish transient blips from a
			// stuck reconnect loop. Escalate log severity after the threshold.
			n := atomic.AddInt32(&c.consecutiveErrs, 1)
			if n >= errWarnThreshold {
				nano := atomic.LoadInt64(&c.lastDeliveryNano)
				var since string
				if nano == 0 {
					since = "never"
				} else {
					since = time.Since(time.Unix(0, nano)).Round(time.Second).String()
				}
				slog.Error("spine: persistent subscriber errors — Spine may be unreachable",
					"consecutive_errors", n,
					"last_delivery_ago", since,
					"error", err)
			} else {
				slog.Warn("spine: subscriber error", "error", err)
			}

		case frame, ok := <-deliveries:
			if !ok {
				if ctx.Err() != nil {
					return // context cancelled — normal shutdown
				}
				// Delivery channel closed unexpectedly (Spine disconnected or SDK
				// reconnected and replaced its internal channel). Restart the loop
				// so the new invocation calls c.subscriber.Deliveries() and picks
				// up the fresh channel.  Without this the agent goes permanently
				// deaf: SSH health checks pass but Spine messages never arrive.
				slog.Warn("spine: delivery channel closed unexpectedly — restarting dispatch loop")
				go c.dispatchLoop(ctx)
				return
			}
			// Successful delivery: reset error counter and record delivery time.
			atomic.StoreInt32(&c.consecutiveErrs, 0)
			atomic.StoreInt64(&c.lastDeliveryNano, time.Now().UnixNano())
			for _, env := range frame.Envelopes {
				c.safeDispatch(ctx, env)
			}
		}
	}
}

// safeDispatch wraps dispatch with a recover to prevent a single bad envelope
// or handler panic from crashing the entire agent process.
func (c *Client) safeDispatch(ctx context.Context, env *umsv1.Envelope) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("spine: handler panic recovered",
				"panic", r,
				"envelope_type", env.GetType(),
				"envelope_id", env.GetEnvelopeId(),
				"stack", string(debug.Stack()))
		}
	}()
	c.dispatch(ctx, env)
}

// dispatch routes a single envelope to its registered handlers and acks it.
func (c *Client) dispatch(ctx context.Context, env *umsv1.Envelope) {
	if env == nil {
		slog.Warn("spine: skipping nil envelope")
		return
	}

	msg := fromEnvelope(env)

	// Inject trace ID into context for handlers to access
	if msg.TraceID != "" {
		ctx = sdk.WithTraceID(ctx, msg.TraceID)
	}

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

// livenessStaleThreshold is how long without a delivery before we consider
// the dispatch loop stale and force a resubscribe.
const livenessStaleThreshold = 90 * time.Second

// livenessCheckInterval is how often we check for staleness.
const livenessCheckInterval = 30 * time.Second

// livenessWatchdog runs alongside dispatchLoop and detects when the Spine
// subscription has gone stale (no messages for livenessStaleThreshold).
// When staleness is detected, it forces a resubscribe to recover from
// silent connection loss after Spine reconnects.
func (c *Client) livenessWatchdog(ctx context.Context) {
	ticker := time.NewTicker(livenessCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			nano := atomic.LoadInt64(&c.lastDeliveryNano)
			if nano == 0 {
				// Never received a delivery yet — too early to judge.
				// But if it's been a very long time since Start(), warn.
				continue
			}

			sinceLastDelivery := time.Since(time.Unix(0, nano))
			if sinceLastDelivery > livenessStaleThreshold {
				slog.Warn("spine: no deliveries received — forcing resubscribe",
					"last_delivery_ago", sinceLastDelivery.Round(time.Second),
					"threshold", livenessStaleThreshold,
				)
				if err := c.Resubscribe(); err != nil {
					slog.Error("spine: resubscribe failed during liveness recovery",
						"error", err,
					)
				}
			}
		}
	}
}

// Resubscribe re-sends the subscription for this server to Spine.
// This is used to recover from stale dispatch loops after a reconnect
// where the subscription may have been silently lost.
func (c *Client) Resubscribe() error {
	target := &umsv1.Target{
		TargetType: umsv1.TargetType_TARGET_TYPE_SERVER,
		TargetId:   c.serverID,
		OrgId:      c.orgID,
	}
	if err := c.subscriber.Subscribe([]*umsv1.Target{target}); err != nil {
		return fmt.Errorf("spine: resubscribe: %w", err)
	}
	slog.Info("spine: resubscribed successfully",
		"server_id", c.serverID,
	)
	return nil
}
