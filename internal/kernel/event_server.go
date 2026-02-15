package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	pb "github.com/ambientlabscomputing/umc_sdk/proto/ua_kernel/v1"
	"github.com/ambientlabscomputing/underleaf_client/internal/bus"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// EventServer implements the EventService for UMCs.
type EventServer struct {
	pb.UnimplementedEventServiceServer
	busClient   *bus.Client
	subscribers map[string][]chan *pb.LocalEvent
	mu          sync.RWMutex
}

// NewEventServer creates a new event server.
func NewEventServer(busClient *bus.Client) *EventServer {
	return &EventServer{
		busClient:   busClient,
		subscribers: make(map[string][]chan *pb.LocalEvent),
	}
}

// EmitEvent publishes an event to the event bus.
func (s *EventServer) EmitEvent(ctx context.Context, req *pb.EmitEventRequest) (*pb.EmitEventResponse, error) {
	if s.busClient == nil {
		return nil, fmt.Errorf("event bus client not available")
	}

	selector := bus.SelectorFields{
		Topic:      req.EventType,
		TargetType: req.EntityKind,
		TargetID:   req.EntityId,
		TraceID:    req.TraceId,
	}

	err := s.busClient.Publish(ctx, selector, req.Payload)
	if err != nil {
		return nil, fmt.Errorf("failed to publish event: %w", err)
	}

	return &pb.EmitEventResponse{
		EventId:   uuid.New().String(),
		EmittedAt: timestamppb.Now(),
	}, nil
}

// SubscribeLocal subscribes to events matching a filter.
func (s *EventServer) SubscribeLocal(req *pb.SubscribeLocalRequest, stream pb.EventService_SubscribeLocalServer) error {
	bufferSize := int(req.BufferSizeHint)
	if bufferSize <= 0 {
		bufferSize = 100
	}
	ch := make(chan *pb.LocalEvent, bufferSize)
	ctx := stream.Context()

	// Register subscriber for each event type (for internal events)
	s.mu.Lock()
	for _, eventType := range req.EventTypeFilter {
		s.subscribers[eventType] = append(s.subscribers[eventType], ch)
	}
	s.mu.Unlock()

	// Subscribe to external bus for each event type
	var busSubscriptions []bus.ClientSubscription
	if s.busClient != nil {
		for _, eventType := range req.EventTypeFilter {
			selector := bus.SelectorFields{
				Topic: eventType,
			}
			busSub, err := s.busClient.Subscribe(ctx, selector)
			if err != nil {
				// Log but don't fail - internal events still work
				fmt.Printf("Warning: failed to subscribe to event bus for %s: %v\n", eventType, err)
			} else {
				busSubscriptions = append(busSubscriptions, busSub)
			}
		}
	}

	// Launch goroutines to bridge external bus events to internal channel
	doneChan := make(chan struct{})
	var bridgeWg sync.WaitGroup
	for _, busSub := range busSubscriptions {
		bridgeWg.Add(1)
		go func(sub bus.ClientSubscription) {
			defer bridgeWg.Done()
			for {
				select {
				case <-doneChan:
					return
				case <-ctx.Done():
					return
				case busMsg := <-sub.HandlerChan:
					// Convert event_bus_client.Message to pb.LocalEvent
					localEvent := &pb.LocalEvent{
						EventId:   busMsg.ID,
						EventType: busMsg.Topic,
					}

					// Parse content as JSON and convert to google.protobuf.Struct
					if busMsg.Content != "" {
						var contentMap map[string]interface{}
						if err := json.Unmarshal([]byte(busMsg.Content), &contentMap); err == nil {
							if payload, err := structpb.NewStruct(contentMap); err == nil {
								localEvent.Payload = payload
							}
						}
					}

					// Map optional pointer fields
					if busMsg.TargetType != nil {
						localEvent.EntityKind = *busMsg.TargetType
					}
					if busMsg.TargetID != nil {
						localEvent.EntityId = *busMsg.TargetID
					}

					// Send to internal channel (non-blocking)
					select {
					case ch <- localEvent:
					case <-ctx.Done():
						return
					default:
						// Channel full, skip event to avoid blocking
					}
				}
			}
		}(busSub)
	}

	// Unregister on exit
	defer func() {
		close(doneChan)
		bridgeWg.Wait()

		s.mu.Lock()
		for _, eventType := range req.EventTypeFilter {
			subs := s.subscribers[eventType]
			for i, subCh := range subs {
				if subCh == ch {
					s.subscribers[eventType] = append(subs[:i], subs[i+1:]...)
					break
				}
			}
		}
		s.mu.Unlock()
		close(ch)
	}()

	// Stream events to the client
	for {
		select {
		case <-ctx.Done():
			return nil
		case event := <-ch:
			if err := stream.Send(event); err != nil {
				return err
			}
		}
	}
}
