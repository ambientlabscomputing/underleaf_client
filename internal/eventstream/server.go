package eventstream

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/ambientlabscomputing/underleaf_client/proto/ua_mma/v1"
)

const (
	DefaultBufferSize = 1000
	MaxBufferSize     = 10000
	RingBufferSize    = 50000
	MinBufferSize     = 100
)

type Server struct {
	pb.UnimplementedUAEventStreamServiceServer
	mu          sync.RWMutex
	subscribers map[string]*subscriber
	ringBuffer  *ringBuffer
	logger      *slog.Logger
	clusterID   string
	nodeID      string
	seqCounter  uint64
}

type subscriber struct {
	id       string
	ch       chan *pb.UAEvent
	ctx      context.Context
	cancel   context.CancelFunc
	filter   map[string]bool
	lastSent map[string]uint64
}

type ringBuffer struct {
	mu     sync.RWMutex
	events []*pb.UAEvent
	head   int
	size   int
	cap    int
}

func NewServer(clusterID, nodeID string, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		subscribers: make(map[string]*subscriber),
		ringBuffer:  newRingBuffer(RingBufferSize),
		logger:      logger,
		clusterID:   clusterID,
		nodeID:      nodeID,
	}
}

func (s *Server) StreamEvents(req *pb.StreamEventsRequest, stream pb.UAEventStreamService_StreamEventsServer) error {
	ctx := stream.Context()
	peerInfo := "unknown"
	if p, ok := peer.FromContext(ctx); ok {
		peerInfo = p.Addr.String()
	}
	s.logger.Info("new event stream subscription", "peer", peerInfo)

	bufferSize := int(req.BufferSizeHint)
	if bufferSize == 0 {
		bufferSize = DefaultBufferSize
	}
	if bufferSize < MinBufferSize {
		bufferSize = MinBufferSize
	}
	if bufferSize > MaxBufferSize {
		bufferSize = MaxBufferSize
	}

	filter := make(map[string]bool)
	for _, et := range req.EventTypeFilter {
		filter[et] = true
	}

	subCtx, cancel := context.WithCancel(ctx)
	sub := &subscriber{
		id:       fmt.Sprintf("sub-%d", time.Now().UnixNano()),
		ch:       make(chan *pb.UAEvent, bufferSize),
		ctx:      subCtx,
		cancel:   cancel,
		filter:   filter,
		lastSent: make(map[string]uint64),
	}

	s.mu.Lock()
	s.subscribers[sub.id] = sub
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.subscribers, sub.id)
		s.mu.Unlock()
		cancel()
		s.logger.Info("event stream subscription closed", "peer", peerInfo, "subscriber_id", sub.id)
	}()

	if len(req.LastSeenSeq) > 0 {
		events := s.ringBuffer.GetEventsSince(req.LastSeenSeq)
		s.logger.Info("replaying catch-up events", "count", len(events), "subscriber_id", sub.id)
		for _, evt := range events {
			if s.shouldSend(sub, evt) {
				if err := stream.Send(evt); err != nil {
					s.logger.Error("failed to send catch-up event", "error", err, "subscriber_id", sub.id)
					return status.Errorf(codes.Unavailable, "failed to send event: %v", err)
				}
				sub.lastSent[evt.NodeId] = evt.Seq
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case evt := <-sub.ch:
			if err := stream.Send(evt); err != nil {
				s.logger.Error("failed to send event", "error", err, "subscriber_id", sub.id)
				return status.Errorf(codes.Unavailable, "failed to send event: %v", err)
			}
			sub.lastSent[evt.NodeId] = evt.Seq
			s.logger.Info("sent event to subscriber", "event_type", evt.EventType, "event_id", evt.EventId, "seq", evt.Seq, "subscriber_id", sub.id)
		}
	}
}

func (s *Server) shouldSend(sub *subscriber, evt *pb.UAEvent) bool {
	if len(sub.filter) == 0 {
		return true
	}
	return sub.filter[evt.EventType]
}

func (s *Server) PublishEvent(eventType string, payload map[string]interface{}, entityKind, entityID string) error {
	s.mu.Lock()
	s.seqCounter++
	seq := s.seqCounter
	s.mu.Unlock()

	payloadStruct, err := structpb.NewStruct(payload)
	if err != nil {
		return fmt.Errorf("failed to convert payload to protobuf struct: %w", err)
	}

	evt := &pb.UAEvent{
		EventId:   fmt.Sprintf("evt-%s-%d", s.nodeID, seq),
		EventType: eventType,
		EmittedAt: timestamppb.Now(),
		ClusterId: s.clusterID,
		NodeId:    s.nodeID,
		Seq:       seq,
		Payload:   payloadStruct,
	}

	if entityKind != "" && entityID != "" {
		evt.EntityRef = &pb.EntityRef{
			Kind: entityKind,
			Id:   entityID,
		}
	}

	s.ringBuffer.Add(evt)
	s.mu.RLock()
	defer s.mu.RUnlock()

	s.logger.Info("publishing event to subscribers", "event_type", evt.EventType, "event_id", evt.EventId, "subscriber_count", len(s.subscribers))

	for _, sub := range s.subscribers {
		if !s.shouldSend(sub, evt) {
			s.logger.Info("skipping subscriber due to filter", "subscriber_id", sub.id, "event_type", evt.EventType)
			continue
		}
		s.logger.Info("attempting to send to subscriber", "subscriber_id", sub.id, "event_type", evt.EventType)
		select {
		case sub.ch <- evt:
			s.logger.Info("event queued for subscriber", "subscriber_id", sub.id, "event_type", evt.EventType)
		default:
			select {
			case <-sub.ch:
				s.logger.Warn("subscriber buffer full, dropped oldest event", "subscriber_id", sub.id)
			default:
			}
			select {
			case sub.ch <- evt:
			default:
				s.logger.Error("subscriber buffer still full after drop, skipping event", "subscriber_id", sub.id)
			}
		}
	}
	return nil
}

func newRingBuffer(capacity int) *ringBuffer {
	return &ringBuffer{
		events: make([]*pb.UAEvent, capacity),
		cap:    capacity,
	}
}

func (rb *ringBuffer) Add(evt *pb.UAEvent) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.events[rb.head] = evt
	rb.head = (rb.head + 1) % rb.cap
	if rb.size < rb.cap {
		rb.size++
	}
}

func (rb *ringBuffer) GetEventsSince(lastSeenSeq map[string]uint64) []*pb.UAEvent {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	var result []*pb.UAEvent
	start := (rb.head - rb.size + rb.cap) % rb.cap
	for i := 0; i < rb.size; i++ {
		idx := (start + i) % rb.cap
		evt := rb.events[idx]
		if evt == nil {
			continue
		}
		lastSeen, exists := lastSeenSeq[evt.NodeId]
		if !exists || evt.Seq > lastSeen {
			result = append(result, evt)
		}
	}
	return result
}

func (s *Server) SubscriberCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.subscribers)
}

func (s *Server) BufferStats() (size, capacity int) {
	s.ringBuffer.mu.RLock()
	defer s.ringBuffer.mu.RUnlock()
	return s.ringBuffer.size, s.ringBuffer.cap
}
