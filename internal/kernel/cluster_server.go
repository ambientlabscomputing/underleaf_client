package kernel

import (
	"context"
	"fmt"
	"time"

	pb "github.com/ambientlabscomputing/umc_sdk/proto/ua_kernel/v1"
	"github.com/ambientlabscomputing/underleaf_client/internal/raft"
	hashicorpraft "github.com/hashicorp/raft"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	if s.node == nil {
		return nil, status.Error(codes.Unavailable, "raft node not available")
	}

	stats, err := s.node.GetStats()
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster stats: %w", err)
	}

	// Get cluster configuration to enumerate nodes
	config, err := s.node.GetConfiguration()
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster configuration: %w", err)
	}

	// Map raft.NodeInfo to pb.NodeInfo
	nodes := make([]*pb.NodeInfo, 0, len(config.Nodes))
	for _, node := range config.Nodes {
		// Map raft.NodeRole to pb.NodeRole
		var role pb.NodeRole
		switch node.Role {
		case raft.RoleLeader:
			role = pb.NodeRole_NODE_ROLE_VOTER
		case raft.RoleFollower:
			role = pb.NodeRole_NODE_ROLE_VOTER
		case raft.RoleLearner:
			role = pb.NodeRole_NODE_ROLE_NONVOTER
		default:
			role = pb.NodeRole_NODE_ROLE_UNSPECIFIED
		}

		nodes = append(nodes, &pb.NodeInfo{
			NodeId:  node.ID,
			Address: node.Address,
			Role:    role,
			State:   pb.NodeState_NODE_STATE_ALIVE, // Raft doesn't track node state directly
		})
	}

	// Compute cluster health
	quorum := (len(config.Nodes) / 2) + 1
	var health pb.ClusterHealth
	if stats.NumPeers+1 >= len(config.Nodes) {
		health = pb.ClusterHealth_CLUSTER_HEALTH_HEALTHY
	} else if stats.NumPeers+1 >= quorum {
		health = pb.ClusterHealth_CLUSTER_HEALTH_DEGRADED
	} else {
		health = pb.ClusterHealth_CLUSTER_HEALTH_CRITICAL
	}

	return &pb.GetClusterStateResponse{
		ClusterId: stats.NodeID, // Use node ID as cluster ID for now
		LeaderId:  stats.LeaderID,
		Nodes:     nodes,
		Health:    health,
		RaftIndex: stats.CommitIndex,
		RaftTerm:  stats.Term,
	}, nil
}

// ProposeClusterConfig proposes a change to the cluster configuration.
func (s *ClusterServer) ProposeClusterConfig(ctx context.Context, req *pb.ProposeClusterConfigRequest) (*pb.ProposeClusterConfigResponse, error) {
	if s.node == nil {
		return nil, status.Error(codes.Unavailable, "raft node not available")
	}

	// Get the underlying Raft instance
	r := s.node.GetRaft()
	if r == nil {
		return nil, status.Error(codes.Unavailable, "raft instance not available")
	}

	// TODO: Parse req.Nodes, compare with current config, apply changes
	return nil, status.Error(codes.Unimplemented, "cluster configuration changes not yet fully implemented")
}

// JoinCluster adds a node to the cluster.
func (s *ClusterServer) JoinCluster(ctx context.Context, req *pb.JoinClusterRequest) (*pb.JoinClusterResponse, error) {
	if s.node == nil {
		return &pb.JoinClusterResponse{
			Success: false,
			Error:   "raft node not available",
		}, nil
	}

	// Get the underlying Raft instance
	r := s.node.GetRaft()
	if r == nil {
		return &pb.JoinClusterResponse{
			Success: false,
			Error:   "raft instance not available",
		}, nil
	}

	// Verify we're the leader (only leader can add servers)
	if r.State() != hashicorpraft.Leader {
		return &pb.JoinClusterResponse{
			Success: false,
			Error:   "only the cluster leader can accept join requests",
		}, nil
	}

	// TODO: Validate join_token for security and authorization
	// For now, we'll allow any join request

	// Use node_id from request; fall back to join_token for backward compatibility
	var nodeID hashicorpraft.ServerID
	if req.NodeId != "" {
		nodeID = hashicorpraft.ServerID(req.NodeId)
	} else {
		// Backward compatibility: extract node ID from join_token if NodeId not provided
		nodeID = hashicorpraft.ServerID(req.JoinToken)
	}

	// Add the server as a voter
	serverAddr := hashicorpraft.ServerAddress(req.NodeAddress)
	future := r.AddVoter(nodeID, serverAddr, 0, 10*time.Second)
	if err := future.Error(); err != nil {
		return &pb.JoinClusterResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to add voter: %v", err),
		}, nil
	}

	// TODO: Get actual cluster ID from configuration or metadata
	// For now, return a placeholder
	return &pb.JoinClusterResponse{
		Success:   true,
		ClusterId: "default-cluster",
	}, nil
}

// GetKV retrieves a value from the cluster KV store.
func (s *ClusterServer) GetKV(ctx context.Context, req *pb.GetKVRequest) (*pb.GetKVResponse, error) {
	if s.node == nil {
		return nil, status.Error(codes.Unavailable, "raft node not available")
	}

	kv := raft.NewKV(s.node)
	entry, err := kv.Get(req.Key, raft.ReadModeLinearizable)
	if err != nil {
		return nil, fmt.Errorf("failed to get KV: %w", err)
	}

	return &pb.GetKVResponse{
		Value:     entry.Value,
		Version:   uint64(entry.Version),
		UpdatedAt: timestamppb.New(entry.ModTime),
	}, nil
}

// PutKV stores a value in the cluster KV store.
func (s *ClusterServer) PutKV(ctx context.Context, req *pb.PutKVRequest) (*pb.PutKVResponse, error) {
	if s.node == nil {
		return nil, status.Error(codes.Unavailable, "raft node not available")
	}

	kv := raft.NewKV(s.node)
	err := kv.Put(req.Key, req.Value, "")
	if err != nil {
		return nil, fmt.Errorf("failed to put KV: %w", err)
	}

	// Read back the entry to get version and timestamp
	entry, err := kv.Get(req.Key, raft.ReadModeLinearizable)
	if err != nil {
		return nil, fmt.Errorf("failed to read back KV: %w", err)
	}

	return &pb.PutKVResponse{
		Version:   uint64(entry.Version),
		UpdatedAt: timestamppb.New(entry.ModTime),
	}, nil
}
