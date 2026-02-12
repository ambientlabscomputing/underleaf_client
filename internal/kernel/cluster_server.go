package kernel

import (
	"context"
	"fmt"

	pb "github.com/ambientlabscomputing/umc_sdk/proto/ua_kernel/v1"
	"github.com/ambientlabscomputing/underleaf_client/internal/raft"
)

// ClusterServer implements the ClusterService for UMCs.
type ClusterServer struct {
	pb.UnimplementedClusterServiceServer
	node *raft.Node
}

// NewClusterServer creates a new cluster server.
func NewClusterServer(node *raft.Node) *ClusterServer {
	return &ClusterServer{
		node: node,
	}
}

// GetClusterState returns the current state of the cluster.
func (s *ClusterServer) GetClusterState(ctx context.Context, req *pb.GetClusterStateRequest) (*pb.GetClusterStateResponse, error) {
	stats, err := s.node.GetStats()
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster stats: %w", err)
	}

	return &pb.GetClusterStateResponse{
		LeaderId:  stats.LeaderID,
		RaftIndex: stats.CommitIndex,
		RaftTerm:  stats.Term,
		Nodes:     []*pb.NodeInfo{},
	}, nil
}

// ProposeClusterConfig proposes a change to the cluster configuration.
func (s *ClusterServer) ProposeClusterConfig(ctx context.Context, req *pb.ProposeClusterConfigRequest) (*pb.ProposeClusterConfigResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// JoinCluster adds a node to the cluster.
func (s *ClusterServer) JoinCluster(ctx context.Context, req *pb.JoinClusterRequest) (*pb.JoinClusterResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// GetKV retrieves a value from the cluster KV store.
func (s *ClusterServer) GetKV(ctx context.Context, req *pb.GetKVRequest) (*pb.GetKVResponse, error) {
	kv := raft.NewKV(s.node)
	entry, err := kv.Get(req.Key, raft.ReadModeLinearizable)
	if err != nil {
		return nil, fmt.Errorf("failed to get KV: %w", err)
	}

	return &pb.GetKVResponse{
		Value:   entry.Value,
		Version: uint64(entry.Version),
	}, nil
}

// PutKV stores a value in the cluster KV store.
func (s *ClusterServer) PutKV(ctx context.Context, req *pb.PutKVRequest) (*pb.PutKVResponse, error) {
	kv := raft.NewKV(s.node)
	err := kv.Put(req.Key, req.Value, "")
	if err != nil {
		return nil, fmt.Errorf("failed to put KV: %w", err)
	}

	return &pb.PutKVResponse{}, nil
}
