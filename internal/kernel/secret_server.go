package kernel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	pb "github.com/ambientlabscomputing/umc_sdk/proto/ua_kernel/v1"
	"github.com/ambientlabscomputing/underleaf_client/internal/raft"
	"google.golang.org/protobuf/types/known/timestamppb"
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

// SetSecretStore updates the secret store (used when raft is initialized after server creation).
func (s *SecretServer) SetSecretStore(secretStore *raft.SecretStore) {
	s.secretStore = secretStore
}

// StoreSecret stores an encrypted secret in the cluster.
func (s *SecretServer) StoreSecret(ctx context.Context, req *pb.StoreSecretRequest) (*pb.StoreSecretResponse, error) {
	if s.secretStore == nil {
		return nil, fmt.Errorf("secret store not available (raft not initialized)")
	}

	data := make(map[string]interface{})
	for k, v := range req.Metadata {
		data[k] = v
	}
	data["value"] = string(req.Value)

	meta, err := s.secretStore.Put(ctx, req.Key, data, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to store secret: %w", err)
	}

	return &pb.StoreSecretResponse{
		Version:   fmt.Sprintf("%d", meta.Version),
		CreatedAt: timestamppb.New(meta.CreatedTime),
	}, nil
}

// GetSecret retrieves a decrypted secret from the cluster.
func (s *SecretServer) GetSecret(ctx context.Context, req *pb.GetSecretRequest) (*pb.GetSecretResponse, error) {
	if s.secretStore == nil {
		return nil, fmt.Errorf("secret store not available (raft not initialized)")
	}

	version, err := s.secretStore.Get(ctx, req.Key, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get secret: %w", err)
	}

	valueStr, ok := version.Data["value"].(string)
	if !ok {
		return nil, fmt.Errorf("secret value is not a string")
	}

	// Map custom metadata, excluding the "value" field
	metadata := make(map[string]string)
	for k, v := range version.Metadata.CustomMeta {
		metadata[k] = v
	}

	return &pb.GetSecretResponse{
		Value:     []byte(valueStr),
		Version:   fmt.Sprintf("%d", version.Metadata.Version),
		Metadata:  metadata,
		CreatedAt: timestamppb.New(version.Metadata.CreatedTime),
	}, nil
}

// MountSecret mounts a secret as a file (for UMCs that need file-based secrets).
func (s *SecretServer) MountSecret(ctx context.Context, req *pb.MountSecretRequest) (*pb.MountSecretResponse, error) {
	if s.secretStore == nil {
		return &pb.MountSecretResponse{
			Success: false,
			Error:   "secret store not available",
		}, nil
	}

	// Retrieve the secret
	version, err := s.secretStore.Get(ctx, req.Key, nil)
	if err != nil {
		return &pb.MountSecretResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to get secret: %v", err),
		}, nil
	}

	// Extract the secret value
	valueStr, ok := version.Data["value"].(string)
	if !ok {
		return &pb.MountSecretResponse{
			Success: false,
			Error:   "secret value is not a string",
		}, nil
	}

	// Ensure target directory exists
	targetDir := filepath.Dir(req.TargetPath)
	if err := os.MkdirAll(targetDir, 0700); err != nil {
		return &pb.MountSecretResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to create target directory: %v", err),
		}, nil
	}

	// Write secret to file with restricted permissions
	if err := os.WriteFile(req.TargetPath, []byte(valueStr), 0400); err != nil {
		return &pb.MountSecretResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to write secret file: %v", err),
		}, nil
	}

	return &pb.MountSecretResponse{
		Success: true,
	}, nil
}

// ListSecrets lists all secrets matching a prefix.
func (s *SecretServer) ListSecrets(ctx context.Context, req *pb.ListSecretsRequest) (*pb.ListSecretsResponse, error) {
	if s.secretStore == nil {
		return nil, fmt.Errorf("secret store not available (raft not initialized)")
	}

	metadataList, err := s.secretStore.List(ctx, req.Prefix, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list secrets: %w", err)
	}

	secrets := make([]*pb.SecretMetadata, 0, len(metadataList))
	for _, meta := range metadataList {
		secrets = append(secrets, &pb.SecretMetadata{
			Key:       meta.Path,
			Version:   fmt.Sprintf("%d", meta.Version),
			CreatedAt: timestamppb.New(meta.CreatedTime),
			Metadata:  meta.CustomMeta,
		})
	}

	return &pb.ListSecretsResponse{
		Secrets: secrets,
	}, nil
}
