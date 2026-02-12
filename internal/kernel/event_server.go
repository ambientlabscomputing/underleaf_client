package kernel

import (
	"context"
	"fmt"
	"sync"

	pb "github.com/ambientlabscomputing/umc_sdk/proto/ua_kernel/v1"
	"github.com/ambientlabscomputing/underleaf_client/internal/bus"
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

	return &pb.EmitEventResponse{}, nil
}

// SubscribeLocal subscribes to events matching a filter.
func (s *EventServer) SubscribeLocal(req *pb.SubscribeLocalRequest, stream pb.EventService_SubscribeLocalServer) error {
	bufferSize := int(req.BufferSizeHint)
	if bufferSize <= 0 {
		bufferSize = 100
	}
	ch := make(chan *pb.LocalEvent, bufferSize)

	// Register subscriber for each event type
	s.mu.Lock()
	for _, eventType := range req.EventTypeFilter {
		s.subscribers[eventType] = append(s.subscribers[eventType], ch)
	}
	s.mu.Unlock()

	// Unregister on exit
	defer func() {
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
		case <-stream.Context().Done():
			return nil
		case event := <-ch:
			if err := stream.Send(event); err != nil {
				return err
			}
		}
	}
}
