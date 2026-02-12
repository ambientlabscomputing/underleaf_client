package kernel

import (
	"context"
	"fmt"

	pb "github.com/ambientlabscomputing/umc_sdk/proto/ua_kernel/v1"
	"github.com/ambientlabscomputing/underleaf_client/internal/raft"
)

// SecretServer implements the SecretService for UMCs.
type SecretServer struct {
	pb.UnimplementedSecretServiceServer
	secretStore *raft.SecretStore
}

// NewSecretServer creates a new secret server.
func NewSecretServer(secretStore *raft.SecretStore) *SecretServer {
	return &SecretServer{
		secretStore: secretStore,
	}
}

// StoreSecret stores an encrypted secret in the cluster.
func (s *SecretServer) StoreSecret(ctx context.Context, req *pb.StoreSecretRequest) (*pb.StoreSecretResponse, error) {
	data := make(map[string]interface{})
	for k, v := range req.Metadata {
		data[k] = v
	}
	data["value"] = string(req.Value)

	_, err := s.secretStore.Put(ctx, req.Key, data, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to store secret: %w", err)
	}

	return &pb.StoreSecretResponse{}, nil
}

// GetSecret retrieves a decrypted secret from the cluster.
func (s *SecretServer) GetSecret(ctx context.Context, req *pb.GetSecretRequest) (*pb.GetSecretResponse, error) {
	version, err := s.secretStore.Get(ctx, req.Key, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get secret: %w", err)
	}

	valueStr, ok := version.Data["value"].(string)
	if !ok {
		return nil, fmt.Errorf("secret value is not a string")
	}

	return &pb.GetSecretResponse{
		Value: []byte(valueStr),
	}, nil
}

// MountSecret mounts a secret as a file (for UMCs that need file-based secrets).
func (s *SecretServer) MountSecret(ctx context.Context, req *pb.MountSecretRequest) (*pb.MountSecretResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// ListSecrets lists all secrets matching a prefix.
func (s *SecretServer) ListSecrets(ctx context.Context, req *pb.ListSecretsRequest) (*pb.ListSecretsResponse, error) {
	metadataList, err := s.secretStore.List(ctx, req.Prefix, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list secrets: %w", err)
	}

	secrets := make([]*pb.SecretMetadata, 0, len(metadataList))
	for _, meta := range metadataList {
		secrets = append(secrets, &pb.SecretMetadata{
			Key: meta.Path,
		})
	}

	return &pb.ListSecretsResponse{
		Secrets: secrets,
	}, nil
}
