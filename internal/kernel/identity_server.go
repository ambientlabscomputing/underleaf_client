package kernel

import (
	"context"
	"fmt"

	pb "github.com/ambientlabscomputing/umc_sdk/proto/ua_kernel/v1"
	"github.com/ambientlabscomputing/underleaf_client/internal/crypto"
	"github.com/ambientlabscomputing/underleaf_client/internal/crypto/keymanager"
)

// IdentityServer implements the IdentityService for UMCs.
type IdentityServer struct {
	pb.UnimplementedIdentityServiceServer
	keyManager keymanager.KeyManager
	nodeID     string
	orgID      string
	clusterID  string
}

// NewIdentityServer creates a new identity server.
func NewIdentityServer(km keymanager.KeyManager, nodeID, orgID, clusterID string) *IdentityServer {
	return &IdentityServer{
		keyManager: km,
		nodeID:     nodeID,
		orgID:      orgID,
		clusterID:  clusterID,
	}
}

// GetNodeIdentity returns the node's identity information.
func (s *IdentityServer) GetNodeIdentity(ctx context.Context, req *pb.GetNodeIdentityRequest) (*pb.GetNodeIdentityResponse, error) {
	return &pb.GetNodeIdentityResponse{
		NodeId:    s.nodeID,
		OrgId:     s.orgID,
		ClusterId: s.clusterID,
	}, nil
}

// SignPayload signs a payload using the key manager's encryption.
func (s *IdentityServer) SignPayload(ctx context.Context, req *pb.SignPayloadRequest) (*pb.SignPayloadResponse, error) {
	if s.keyManager == nil {
		return nil, fmt.Errorf("key manager not available")
	}

	// Use key manager's Encrypt as a signing mechanism
	signature, err := s.keyManager.Encrypt(req.Payload)
	if err != nil {
		return nil, fmt.Errorf("failed to sign payload: %w", err)
	}

	return &pb.SignPayloadResponse{
		Signature: signature,
		Algorithm: "aes-gcm",
	}, nil
}

// VerifyTrust verifies if a certificate is trusted by the cluster.
func (s *IdentityServer) VerifyTrust(ctx context.Context, req *pb.VerifyTrustRequest) (*pb.VerifyTrustResponse, error) {
	certPEM := []byte(req.SignerCertPem)
	cert, err := crypto.ParseCertificate(certPEM)
	if err != nil {
		return &pb.VerifyTrustResponse{
			Trusted: false,
			Error:   fmt.Sprintf("failed to parse certificate: %v", err),
		}, nil
	}

	valid, err := crypto.VerifySignature(cert.PublicKey, req.Payload, req.Signature)
	if err != nil || !valid {
		errMsg := "signature verification failed"
		if err != nil {
			errMsg = fmt.Sprintf("signature verification failed: %v", err)
		}
		return &pb.VerifyTrustResponse{
			Trusted: false,
			Error:   errMsg,
		}, nil
	}

	return &pb.VerifyTrustResponse{
		Trusted: true,
	}, nil
}

// IssueLocalCertificate issues a certificate for UMC-to-UMC communication.
func (s *IdentityServer) IssueLocalCertificate(ctx context.Context, req *pb.IssueLocalCertificateRequest) (*pb.IssueLocalCertificateResponse, error) {
	privateKey, err := crypto.GenerateECDSAKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}

	csrData := crypto.CSRData{
		CommonName:  req.ComponentName,
		DNSNames:    req.DnsNames,
		IPAddresses: req.IpAddresses,
	}

	csrPEM, err := crypto.GenerateCSRWithData(privateKey, csrData)
	if err != nil {
		return nil, fmt.Errorf("failed to generate CSR: %w", err)
	}

	privateKeyPEM, err := crypto.PrivateKeyToPEM(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encode private key: %w", err)
	}

	// TODO: Sign CSR with cluster CA; for now return CSR as cert placeholder
	return &pb.IssueLocalCertificateResponse{
		CertificatePem: string(csrPEM),
		PrivateKeyPem:  string(privateKeyPEM),
	}, nil
}
