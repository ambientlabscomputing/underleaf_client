package bus

import (
	"context"
	"encoding/json"
	"fmt"

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
		TargetType: &s.TargetType,
		TargetID:   &s.TargetID,
		OrgID:      &s.OrgID,
		TraceID:    &s.TraceID,
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
	eventBus event_bus_client.EventClient
	channels map[string][]chan event_bus_client.Message
}

func (c *Client) Start(ctx context.Context, serverID string) error {
	logger := logging.GetLogger(ctx)
	logger.Info("starting event bus client")
	c.channels = make(map[string][]chan event_bus_client.Message)
	InitSubscriptions(serverID)
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
	subscriptionReq := event_bus_client.SubscriptionRequest{
		Topic:      selector.Topic,
		TargetType: &selector.TargetType,
		TargetID:   &selector.TargetID,
		OrgID:      &selector.OrgID,
		TraceID:    &selector.TraceID,
	}
	handlerChan := make(chan event_bus_client.Message, 100)
	err := c.eventBus.Subscribe(ctx, subscriptionReq)
	if err != nil {
		return ClientSubscription{}, err
	}
	c.channels[selector.ToIndex()] = append(c.channels[selector.ToIndex()], handlerChan)
	return ClientSubscription{
		Selector:    selector,
		HandlerChan: handlerChan,
	}, nil
}

func (c *Client) handleIncomingMessages(ctx context.Context) {
	logger := logging.GetLogger(ctx)
	// get incoming msg channel
	incomingChan := c.eventBus.IncomingMsgChannel()

	// for select loop to handle incoming messages
	for {
		select {
		case msg := <-incomingChan:
			selector := SelectorFields{
				Topic:      msg.Topic,
				TargetType: defref(msg.TargetType),
				TargetID:   defref(msg.TargetID),
				OrgID:      defref(msg.OrgID),
				TraceID:    defref(msg.TraceID),
			}
			index := selector.ToIndex()
			if chans, ok := c.channels[index]; ok {
				for _, ch := range chans {
					logger.Debug("message received", "destination", index)
					ch <- msg
				}
			} else {
				logger.Warn("no subscribers for message", "selector", index)
			}
		case <-ctx.Done():
			return
		}
	}
}

func defref(s *string) string {
	if s != nil {
		return *s
	}
	return ""
}
