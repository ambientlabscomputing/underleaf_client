package raft

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/raft"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/ambientlabscomputing/underleaf_client/internal/raft/proto"
)

// GRPCTransport implements raft.Transport using gRPC.
type GRPCTransport struct {
	// bindAddr is the address to bind the gRPC server to
	bindAddr string

	// advertiseAddr is the address advertised to other nodes
	advertiseAddr raft.ServerAddress

	// timeout is the timeout for RPC calls
	timeout time.Duration

	// server is the gRPC server
	server *grpc.Server

	// listener is the network listener
	listener net.Listener

	// consumeCh is the channel for incoming RPC requests
	consumeCh chan raft.RPC

	// localAddr is this node's address
	localAddr raft.ServerAddress

	// peers tracks connections to other nodes
	peers   map[raft.ServerAddress]*grpc.ClientConn
	peersMu sync.RWMutex

	// logger for structured logging
	logger *slog.Logger

	// shutdown channel
	shutdownCh chan struct{}
	shutdown   bool
	shutdownMu sync.Mutex
}

// NewGRPCTransport creates a new gRPC-based Raft transport.
func NewGRPCTransport(bindAddr, advertiseAddr string, timeout time.Duration, logger *slog.Logger) (*GRPCTransport, error) {
	if advertiseAddr == "" {
		advertiseAddr = bindAddr
	}

	t := &GRPCTransport{
		bindAddr:      bindAddr,
		advertiseAddr: raft.ServerAddress(advertiseAddr),
		timeout:       timeout,
		consumeCh:     make(chan raft.RPC),
		localAddr:     raft.ServerAddress(advertiseAddr),
		peers:         make(map[raft.ServerAddress]*grpc.ClientConn),
		logger:        logger,
		shutdownCh:    make(chan struct{}),
	}

	return t, nil
}

// Start starts the gRPC server.
func (t *GRPCTransport) Start() error {
	listener, err := net.Listen("tcp", t.bindAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", t.bindAddr, err)
	}

	t.listener = listener
	t.server = grpc.NewServer()

	// Register the gRPC service handler
	handler := &grpcRaftHandler{transport: t}
	pb.RegisterRaftTransportServer(t.server, handler)

	go func() {
		if err := t.server.Serve(listener); err != nil {
			t.logger.Error("gRPC server error", "error", err)
		}
	}()

	t.logger.Info("gRPC transport started", "bind_addr", t.bindAddr, "advertise_addr", t.advertiseAddr)
	return nil
}

// Consumer returns a channel for consuming incoming RPC requests.
func (t *GRPCTransport) Consumer() <-chan raft.RPC {
	return t.consumeCh
}

// LocalAddr returns the local address of this transport.
func (t *GRPCTransport) LocalAddr() raft.ServerAddress {
	return t.localAddr
}

// AppendEntriesPipeline returns a new pipeline for AppendEntries RPCs.
// This is an optimization for batching AppendEntries calls.
func (t *GRPCTransport) AppendEntriesPipeline(id raft.ServerID, target raft.ServerAddress) (raft.AppendPipeline, error) {
	// For simplicity, we'll use the default implementation which just wraps AppendEntries
	return nil, raft.ErrPipelineReplicationNotSupported
}

// AppendEntries sends an AppendEntries RPC to the target node.
func (t *GRPCTransport) AppendEntries(id raft.ServerID, target raft.ServerAddress, args *raft.AppendEntriesRequest, resp *raft.AppendEntriesResponse) error {
	conn, err := t.getConn(target)
	if err != nil {
		return err
	}

	client := pb.NewRaftTransportClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	// Convert raft.AppendEntriesRequest to protobuf
	pbReq := &pb.AppendEntriesRequest{
		Term:              args.Term,
		Leader:            []byte(args.Addr),
		PrevLogEntry:      args.PrevLogEntry,
		PrevLogTerm:       args.PrevLogTerm,
		Entries:           convertLogEntries(args.Entries),
		LeaderCommitIndex: args.LeaderCommitIndex,
	}

	pbResp, err := client.AppendEntries(ctx, pbReq)
	if err != nil {
		return err
	}

	// Convert protobuf response to raft.AppendEntriesResponse
	resp.Term = pbResp.Term
	resp.LastLog = pbResp.LastLog
	resp.Success = pbResp.Success
	resp.NoRetryBackoff = pbResp.NoRetryBackoff

	return nil
}

// RequestVote sends a RequestVote RPC to the target node.
func (t *GRPCTransport) RequestVote(id raft.ServerID, target raft.ServerAddress, args *raft.RequestVoteRequest, resp *raft.RequestVoteResponse) error {
	conn, err := t.getConn(target)
	if err != nil {
		return err
	}

	client := pb.NewRaftTransportClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	// Convert to protobuf
	pbReq := &pb.RequestVoteRequest{
		Term:               args.Term,
		Candidate:          []byte(args.Addr),
		LastLogIndex:       args.LastLogIndex,
		LastLogTerm:        args.LastLogTerm,
		LeadershipTransfer: args.LeadershipTransfer,
	}

	pbResp, err := client.RequestVote(ctx, pbReq)
	if err != nil {
		return err
	}

	resp.Term = pbResp.Term
	resp.Granted = pbResp.Granted

	return nil
}

// InstallSnapshot sends a snapshot to the target node.
func (t *GRPCTransport) InstallSnapshot(id raft.ServerID, target raft.ServerAddress, args *raft.InstallSnapshotRequest, resp *raft.InstallSnapshotResponse, data io.Reader) error {
	conn, err := t.getConn(target)
	if err != nil {
		return err
	}

	client := pb.NewRaftTransportClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout*10) // Longer timeout for snapshots
	defer cancel()

	stream, err := client.InstallSnapshot(ctx)
	if err != nil {
		return err
	}

	// Send initial metadata
	initialReq := &pb.InstallSnapshotRequest{
		Term:               args.Term,
		Leader:             []byte(args.Leader),
		LastLogIndex:       args.LastLogIndex,
		LastLogTerm:        args.LastLogTerm,
		Configuration:      args.Configuration,
		ConfigurationIndex: args.ConfigurationIndex,
		Size:               args.Size,
	}

	if err := stream.Send(initialReq); err != nil {
		return err
	}

	// Stream snapshot data in chunks
	buf := make([]byte, 64*1024) // 64KB chunks
	for {
		n, err := data.Read(buf)
		if err != nil && err != io.EOF {
			return err
		}
		if n == 0 {
			break
		}

		chunkReq := &pb.InstallSnapshotRequest{
			Data: buf[:n],
		}

		if err := stream.Send(chunkReq); err != nil {
			return err
		}

		if err == io.EOF {
			break
		}
	}

	pbResp, err := stream.CloseAndRecv()
	if err != nil {
		return err
	}

	resp.Term = pbResp.Term
	resp.Success = pbResp.Success

	return nil
}

// EncodePeer encodes a peer address for transmission.
func (t *GRPCTransport) EncodePeer(id raft.ServerID, addr raft.ServerAddress) []byte {
	return []byte(addr)
}

// DecodePeer decodes a peer address from transmission.
func (t *GRPCTransport) DecodePeer(buf []byte) raft.ServerAddress {
	return raft.ServerAddress(buf)
}

// SetHeartbeatHandler sets the handler for heartbeat fast-path.
// This is an optional performance optimization that allows handling heartbeats
// without going through the normal RPC pipeline. For our edge-optimized use case
// with small clusters (1-3 nodes) and low latency networks, the standard RPC
// path provides sufficient performance. If heartbeat performance becomes a
// bottleneck in production, this can be implemented to bypass the consumeCh.
func (t *GRPCTransport) SetHeartbeatHandler(cb func(rpc raft.RPC)) {
	// Intentionally not implemented - standard RPC path is sufficient for our use case
	// See: https://github.com/hashicorp/raft/blob/main/transport.go for reference implementation
}

// TimeoutNow sends a TimeoutNow RPC to the target node.
func (t *GRPCTransport) TimeoutNow(id raft.ServerID, target raft.ServerAddress, args *raft.TimeoutNowRequest, resp *raft.TimeoutNowResponse) error {
	conn, err := t.getConn(target)
	if err != nil {
		return err
	}

	client := pb.NewRaftTransportClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	_, err = client.TimeoutNow(ctx, &pb.TimeoutNowRequest{})
	return err
}

// Close shuts down the transport.
func (t *GRPCTransport) Close() error {
	t.shutdownMu.Lock()
	defer t.shutdownMu.Unlock()

	if t.shutdown {
		return nil
	}

	t.shutdown = true
	close(t.shutdownCh)

	// Stop accepting new requests
	if t.server != nil {
		t.server.GracefulStop()
	}

	// Close peer connections
	t.peersMu.Lock()
	for addr, conn := range t.peers {
		conn.Close()
		delete(t.peers, addr)
	}
	t.peersMu.Unlock()

	t.logger.Info("gRPC transport closed")
	return nil
}

// getConn gets or creates a connection to a peer.
func (t *GRPCTransport) getConn(target raft.ServerAddress) (*grpc.ClientConn, error) {
	t.peersMu.RLock()
	conn, exists := t.peers[target]
	t.peersMu.RUnlock()

	if exists {
		return conn, nil
	}

	// Create new connection
	t.peersMu.Lock()
	defer t.peersMu.Unlock()

	// Double-check after acquiring write lock
	if conn, exists := t.peers[target]; exists {
		return conn, nil
	}

	// Dial the peer
	conn, err := grpc.NewClient(
		string(target),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to dial %s: %w", target, err)
	}

	t.peers[target] = conn
	t.logger.Debug("established connection to peer", "target", target)

	return conn, nil
}

// grpcRaftHandler implements the pb.RaftTransportServer interface
type grpcRaftHandler struct {
	pb.UnimplementedRaftTransportServer
	transport *GRPCTransport
}

// AppendEntries handles incoming AppendEntries RPC.
func (h *grpcRaftHandler) AppendEntries(ctx context.Context, req *pb.AppendEntriesRequest) (*pb.AppendEntriesResponse, error) {
	// Convert protobuf to raft types
	args := &raft.AppendEntriesRequest{
		RPCHeader:         raft.RPCHeader{ProtocolVersion: raft.ProtocolVersionMax},
		Term:              req.Term,
		Leader:            req.Leader,
		PrevLogEntry:      req.PrevLogEntry,
		PrevLogTerm:       req.PrevLogTerm,
		Entries:           convertPBLogEntries(req.Entries),
		LeaderCommitIndex: req.LeaderCommitIndex,
	}

	respCh := make(chan raft.RPCResponse, 1)
	rpc := raft.RPC{
		Command:  args,
		RespChan: respCh,
	}

	// Send to consumer
	select {
	case h.transport.consumeCh <- rpc:
	case <-h.transport.shutdownCh:
		return nil, errors.New("transport is shutdown")
	}

	// Wait for response
	select {
	case resp := <-respCh:
		if resp.Error != nil {
			return nil, resp.Error
		}

		// Convert response
		aeResp := resp.Response.(*raft.AppendEntriesResponse)
		return &pb.AppendEntriesResponse{
			Term:           aeResp.Term,
			LastLog:        aeResp.LastLog,
			Success:        aeResp.Success,
			NoRetryBackoff: aeResp.NoRetryBackoff,
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// RequestVote handles incoming RequestVote RPC.
func (h *grpcRaftHandler) RequestVote(ctx context.Context, req *pb.RequestVoteRequest) (*pb.RequestVoteResponse, error) {
	args := &raft.RequestVoteRequest{
		RPCHeader:          raft.RPCHeader{ProtocolVersion: raft.ProtocolVersionMax},
		Term:               req.Term,
		Candidate:          req.Candidate,
		LastLogIndex:       req.LastLogIndex,
		LastLogTerm:        req.LastLogTerm,
		LeadershipTransfer: req.LeadershipTransfer,
	}

	respCh := make(chan raft.RPCResponse, 1)
	rpc := raft.RPC{
		Command:  args,
		RespChan: respCh,
	}

	select {
	case h.transport.consumeCh <- rpc:
	case <-h.transport.shutdownCh:
		return nil, errors.New("transport is shutdown")
	}

	select {
	case resp := <-respCh:
		if resp.Error != nil {
			return nil, resp.Error
		}

		rvResp := resp.Response.(*raft.RequestVoteResponse)
		return &pb.RequestVoteResponse{
			Term:    rvResp.Term,
			Granted: rvResp.Granted,
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// InstallSnapshot handles incoming InstallSnapshot RPC.
func (h *grpcRaftHandler) InstallSnapshot(stream pb.RaftTransport_InstallSnapshotServer) error {
	// Receive first message with metadata
	first, err := stream.Recv()
	if err != nil {
		return err
	}

	args := &raft.InstallSnapshotRequest{
		RPCHeader:          raft.RPCHeader{ProtocolVersion: raft.ProtocolVersionMax},
		SnapshotVersion:    raft.SnapshotVersionMax,
		Term:               first.Term,
		Leader:             first.Leader,
		LastLogIndex:       first.LastLogIndex,
		LastLogTerm:        first.LastLogTerm,
		Configuration:      first.Configuration,
		ConfigurationIndex: first.ConfigurationIndex,
		Size:               first.Size,
	}

	// Create pipe for snapshot data
	pipeReader, pipeWriter := io.Pipe()

	// Read snapshot data in goroutine
	go func() {
		defer pipeWriter.Close()

		for {
			chunk, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				pipeWriter.CloseWithError(err)
				return
			}

			if _, err := pipeWriter.Write(chunk.Data); err != nil {
				pipeWriter.CloseWithError(err)
				return
			}
		}
	}()

	respCh := make(chan raft.RPCResponse, 1)
	rpc := raft.RPC{
		Command:  args,
		Reader:   pipeReader,
		RespChan: respCh,
	}

	select {
	case h.transport.consumeCh <- rpc:
	case <-h.transport.shutdownCh:
		return errors.New("transport is shutdown")
	}

	select {
	case resp := <-respCh:
		if resp.Error != nil {
			return resp.Error
		}

		isResp := resp.Response.(*raft.InstallSnapshotResponse)
		return stream.SendAndClose(&pb.InstallSnapshotResponse{
			Term:    isResp.Term,
			Success: isResp.Success,
		})
	case <-stream.Context().Done():
		return stream.Context().Err()
	}
}

// TimeoutNow handles incoming TimeoutNow RPC.
func (h *grpcRaftHandler) TimeoutNow(ctx context.Context, req *pb.TimeoutNowRequest) (*pb.TimeoutNowResponse, error) {
	args := &raft.TimeoutNowRequest{
		RPCHeader: raft.RPCHeader{ProtocolVersion: raft.ProtocolVersionMax},
	}

	respCh := make(chan raft.RPCResponse, 1)
	rpc := raft.RPC{
		Command:  args,
		RespChan: respCh,
	}

	select {
	case h.transport.consumeCh <- rpc:
	case <-h.transport.shutdownCh:
		return nil, errors.New("transport is shutdown")
	}

	select {
	case resp := <-respCh:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return &pb.TimeoutNowResponse{}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Helper functions

func convertLogEntries(entries []*raft.Log) []*pb.LogEntry {
	if entries == nil {
		return nil
	}

	pbEntries := make([]*pb.LogEntry, len(entries))
	for i, entry := range entries {
		pbEntries[i] = &pb.LogEntry{
			Index:      entry.Index,
			Term:       entry.Term,
			Type:       pb.LogType(entry.Type),
			Data:       entry.Data,
			Extensions: entry.Extensions,
		}
	}
	return pbEntries
}

func convertPBLogEntries(pbEntries []*pb.LogEntry) []*raft.Log {
	if pbEntries == nil {
		return nil
	}

	entries := make([]*raft.Log, len(pbEntries))
	for i, pbEntry := range pbEntries {
		entries[i] = &raft.Log{
			Index:      pbEntry.Index,
			Term:       pbEntry.Term,
			Type:       raft.LogType(pbEntry.Type),
			Data:       pbEntry.Data,
			Extensions: pbEntry.Extensions,
		}
	}
	return entries
}
