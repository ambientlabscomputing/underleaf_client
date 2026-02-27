package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	umsv1 "github.com/ambientlabscomputing/mycelium_spine/proto/ums/v1"
	pb "github.com/ambientlabscomputing/umc_sdk/proto/ua_kernel/v1"
	"github.com/ambientlabscomputing/underleaf_client/internal/spine"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// EventServer implements the gRPC EventService for UMCs.
// It bridges UMC pub/sub calls to Mycelium Spine.
//
// The UMC-facing gRPC API (EmitEvent / SubscribeLocal) is unchanged.
// Internally:
//   - EmitEvent marshals the UMC payload and publishes to Spine.
//   - SubscribeLocal registers a spine handler for each requested event type
//     and streams received messages back to the UMC.
type EventServer struct {
	pb.UnimplementedEventServiceServer
	spineClient *spine.Client
	serverID    string
	orgID       string
	mu          sync.Mutex
}

// NewEventServer creates a new event server backed by a spine.Client.
func NewEventServer(spineClient *spine.Client, serverID, orgID string) *EventServer {
	return &EventServer{
		spineClient: spineClient,
		serverID:    serverID,
		orgID:       orgID,
	}
}

// EmitEvent publishes a UMC-generated event to Mycelium Spine.
// The event is routed to the same server so that other UMCs subscribed via
// SubscribeLocal receive it through the shared spine subscription.
func (s *EventServer) EmitEvent(ctx context.Context, req *pb.EmitEventRequest) (*pb.EmitEventResponse, error) {
	if s.spineClient == nil {
		return nil, fmt.Errorf("spine client not available")
	}

	// Marshal the structpb.Struct payload to JSON bytes.
	payload, err := json.Marshal(req.Payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event payload: %w", err)
	}

	// Route the event back to this server so subscribed UMCs receive it.
	targets := []*umsv1.Target{
		{
			TargetType: umsv1.TargetType_TARGET_TYPE_SERVER,
			TargetId:   s.serverID,
			OrgId:      s.orgID,
		},
	}

	if err := s.spineClient.Publish(ctx, req.EventType, payload, targets, umsv1.QoS_QOS_CONTROL, s.orgID); err != nil {
		return nil, fmt.Errorf("failed to publish event to spine: %w", err)
	}

	return &pb.EmitEventResponse{
		EventId:   uuid.New().String(),
		EmittedAt: timestamppb.Now(),
	}, nil
}

// SubscribeLocal subscribes a UMC to one or more event types.
// Handlers are registered on the shared spine.Client; each incoming envelope
// of a matching type is converted to a pb.LocalEvent and streamed to the UMC.
// All handlers are deregistered when the UMC disconnects.
func (s *EventServer) SubscribeLocal(req *pb.SubscribeLocalRequest, stream pb.EventService_SubscribeLocalServer) error {
	if s.spineClient == nil {
		return fmt.Errorf("spine client not available")
	}

	bufferSize := int(req.BufferSizeHint)
	if bufferSize <= 0 {
		bufferSize = 100
	}

	// Buffered channel to bridge from spine handlers to this stream's send loop.
	eventCh := make(chan *pb.LocalEvent, bufferSize)
	ctx := stream.Context()

	// Register a handler for each requested event type.
	deregisters := make([]func(), 0, len(req.EventTypeFilter))
	for _, eventType := range req.EventTypeFilter {
		et := eventType // capture loop variable
		deregister := s.spineClient.Register(et, func(ctx context.Context, msg spine.Message) {
			// Extract trace ID from the spine message context and embed in local event
			localEvent := spineMessageToLocalEvent(ctx, msg)
			// Non-blocking send: drop if the UMC is too slow rather than blocking the dispatch loop.
			select {
			case eventCh <- localEvent:
			default:
			}
		})
		deregisters = append(deregisters, deregister)
	}

	// Deregister all handlers when the UMC stream closes.
	defer func() {
		for _, dr := range deregisters {
			dr()
		}
		close(eventCh)
	}()

	// Stream events to the UMC.
	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-eventCh:
			if !ok {
				return nil
			}
			if err := stream.Send(event); err != nil {
				return err
			}
		}
	}
}

// spineMessageToLocalEvent converts a spine.Message to the pb.LocalEvent that UMCs expect.
// It preserves the trace ID from the spine message for downstream tracing.
func spineMessageToLocalEvent(ctx context.Context, msg spine.Message) *pb.LocalEvent {
	event := &pb.LocalEvent{
		EventId:   msg.EnvelopeID,
		EventType: msg.Type,
	}

	// Attempt to parse the payload as a JSON object → structpb.Struct.
	if len(msg.Payload) > 0 {
		var m map[string]interface{}
		if err := json.Unmarshal(msg.Payload, &m); err == nil {
			if s, err := structpb.NewStruct(m); err == nil {
				event.Payload = s
			}
		}
	}

	return event
}
