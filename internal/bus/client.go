package bus

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/ambientlabscomputing/event_bus_client"
	"github.com/ambientlabscomputing/underleaf_client/internal/logging"
)

type SelectorFields struct {
	Topic      string `json:"topic"`
	TargetType string `json:"target_type,omitempty"`
	TargetID   string `json:"target_id,omitempty"`
	OrgID      string `json:"org_id,omitempty"`
	TraceID    string `json:"trace_id,omitempty"`
}

func (s SelectorFields) ToSubscriptionRequest() event_bus_client.SubscriptionRequest {
	return event_bus_client.SubscriptionRequest{
		Topic:      s.Topic,
		TargetType: nilIfEmpty(s.TargetType),
		TargetID:   nilIfEmpty(s.TargetID),
		OrgID:      nilIfEmpty(s.OrgID),
		TraceID:    nilIfEmpty(s.TraceID),
	}
}

func (s SelectorFields) ToIndex() string {
	return fmt.Sprintf("%s|%s|%s|%s|%s", s.Topic, s.TargetType, s.TargetID, s.OrgID, s.TraceID)
}

type ClientSubscription struct {
	Selector    SelectorFields
	HandlerChan chan event_bus_client.Message
}

type EventClient interface {
	Publish(ctx context.Context, selector SelectorFields, payload interface{}) error
	Subscribe(ctx context.Context, selector SelectorFields) (ClientSubscription, error)
}

type Client struct {
	eventBus   event_bus_client.EventClient
	channels   map[string][]chan event_bus_client.Message
	channelsMu sync.RWMutex // Protects channels map from concurrent access
}

func (c *Client) Start(ctx context.Context, serverID string) error {
	logger := logging.GetLogger(ctx)
	logger.Info("starting event bus client")
	c.channels = make(map[string][]chan event_bus_client.Message)
	InitSubscriptions(serverID)

	// Pre-register channels for starting subscriptions so they're ready when messages arrive
	for _, sub := range StartingSubscriptions {
		selector := SelectorFields{
			Topic:      sub.Topic,
			TargetType: defref(sub.TargetType),
			TargetID:   defref(sub.TargetID),
			OrgID:      defref(sub.OrgID),
			TraceID:    defref(sub.TraceID),
		}
		index := selector.ToIndex()
		handlerChan := make(chan event_bus_client.Message, 100)
		c.channelsMu.Lock()
		c.channels[index] = append(c.channels[index], handlerChan)
		c.channelsMu.Unlock()
		logger.Debug("pre-registered channel for starting subscription", "index", index)
	}

	if err := c.eventBus.Connect(ctx, &StartingSubscriptions); err != nil {
		return err
	}
	go c.handleIncomingMessages(ctx)
	logger.Info("event bus client started")
	return nil
}

func NewClient(e event_bus_client.EventClient) (*Client, error) {
	return &Client{
		eventBus: e,
	}, nil
}

func (c *Client) Publish(ctx context.Context, selector SelectorFields, payload interface{}) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	// publish vars:
	// ctx context.Context,
	// topic, content string,
	// targetType, targetID, traceID, orgID *string,
	_, err = c.eventBus.Publish(
		ctx,
		selector.Topic, string(payloadBytes),
		&selector.TargetType, &selector.TargetID,
		&selector.TraceID, &selector.OrgID,
	)
	return err
}

func (c *Client) Subscribe(ctx context.Context, selector SelectorFields) (ClientSubscription, error) {
	logger := logging.GetLogger(ctx)
	index := selector.ToIndex()

	logger.Debug("subscribing to event bus",
		"topic", selector.Topic,
		"target_type", selector.TargetType,
		"target_id", selector.TargetID,
		"index", index,
	)

	// Check if already subscribed (from StartingSubscriptions)
	c.channelsMu.RLock()
	chans, ok := c.channels[index]
	c.channelsMu.RUnlock()
	
	if ok && len(chans) > 0 {
		logger.Debug("reusing existing subscription channel", "index", index)
		return ClientSubscription{
			Selector:    selector,
			HandlerChan: chans[0], // Return the first (pre-registered) channel
		}, nil
	}

	// New subscription - register with event bus
	// Use nil for empty filter fields (event_bus_client expects nil for "no filter")
	subscriptionReq := event_bus_client.SubscriptionRequest{
		Topic:      selector.Topic,
		TargetType: nilIfEmpty(selector.TargetType),
		TargetID:   nilIfEmpty(selector.TargetID),
		OrgID:      nilIfEmpty(selector.OrgID),
		TraceID:    nilIfEmpty(selector.TraceID),
	}

	err := c.eventBus.Subscribe(ctx, subscriptionReq)
	if err != nil {
		logger.Error("failed to subscribe to event bus", "error", err)
		return ClientSubscription{}, err
	}

	handlerChan := make(chan event_bus_client.Message, 100)
	c.channelsMu.Lock()
	c.channels[index] = append(c.channels[index], handlerChan)
	totalChannels := len(c.channels)
	c.channelsMu.Unlock()

	logger.Debug("subscription registered",
		"index", index,
		"total_channels", totalChannels,
	)
	return ClientSubscription{
		Selector:    selector,
		HandlerChan: handlerChan,
	}, nil
}

func (c *Client) handleIncomingMessages(ctx context.Context) {
	logger := logging.GetLogger(ctx)
	// get incoming msg channel
	incomingChan := c.eventBus.IncomingMsgChannel()

	logger.Info("message handler started, waiting for messages...")

	// for select loop to handle incoming messages
	for {
		select {
		case msg, ok := <-incomingChan:
			if !ok {
				logger.Warn("incoming message channel closed")
				return
			}
			logger.Info("RAW MESSAGE RECEIVED FROM EVENT BUS",
				"topic", msg.Topic,
				"content_length", len(msg.Content),
				"target_id", defref(msg.TargetID),
			)
			selector := SelectorFields{
				Topic:      msg.Topic,
				TargetType: defref(msg.TargetType),
				TargetID:   defref(msg.TargetID),
				OrgID:      defref(msg.OrgID),
				TraceID:    defref(msg.TraceID),
			}
			index := selector.ToIndex()
			
			c.channelsMu.RLock()
			numChannels := len(c.channels)
			logger.Debug("incoming message",
				"topic", msg.Topic,
				"target_id", defref(msg.TargetID),
				"index", index,
				"registered_channels", numChannels,
			)

			// Try exact match first
			chans, ok := c.channels[index]
			c.channelsMu.RUnlock()
			
			if ok {
				for _, ch := range chans {
					logger.Debug("message delivered (exact match)", "destination", index)
					ch <- msg
				}
				continue
			}

			// Fallback: try topic-only match (for subscriptions that filter in handler)
			topicOnlySelector := SelectorFields{Topic: msg.Topic}
			topicOnlyIndex := topicOnlySelector.ToIndex()
			
			c.channelsMu.RLock()
			chans, ok = c.channels[topicOnlyIndex]
			c.channelsMu.RUnlock()
			
			if ok {
				for _, ch := range chans {
					logger.Debug("message delivered (topic match)", "destination", topicOnlyIndex)
					ch <- msg
				}
				continue
			}

			logger.Warn("no subscribers for message",
				"selector", index,
				"available_indices", c.getChannelIndices(),
			)
		case <-ctx.Done():
			return
		}
	}
}

func (c *Client) getChannelIndices() []string {
	c.channelsMu.RLock()
	defer c.channelsMu.RUnlock()
	
	indices := make([]string, 0, len(c.channels))
	for k := range c.channels {
		indices = append(indices, k)
	}
	return indices
}

// Stop gracefully shuts down the bus client
func (c *Client) Stop() error {
	if closer, ok := c.eventBus.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

// defref dereferences a string pointer, returning empty string if nil
func defref(s *string) string {
	if s != nil {
		return *s
	}
	return ""
}

// nilIfEmpty returns nil if the string is empty, otherwise a pointer to the string.
// This is important because event_bus_client expects nil for "no filter", not a pointer to an empty string.
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
