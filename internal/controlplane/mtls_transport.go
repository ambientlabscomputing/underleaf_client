package controlplane

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/crypto"
)

// MTLSTransport wraps http.RoundTripper to add mTLS headers
type MTLSTransport struct {
	base           http.RoundTripper
	privateKey     *ecdsa.PrivateKey
	certificatePEM []byte
	enabled        bool
}

// NewMTLSTransport creates a transport that adds mTLS headers to requests
func NewMTLSTransport(base http.RoundTripper, privateKeyPath, certPath string) (*MTLSTransport, error) {
	if privateKeyPath == "" || certPath == "" {
		// mTLS not configured, return disabled transport
		return &MTLSTransport{
			base:    base,
			enabled: false,
		}, nil
	}

	// Load private key
	privateKey, err := crypto.LoadPrivateKeyFromFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load private key: %w", err)
	}

	// Load certificate
	certPEM, err := crypto.LoadCertificateFromFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load certificate: %w", err)
	}

	return &MTLSTransport{
		base:           base,
		privateKey:     privateKey,
		certificatePEM: certPEM,
		enabled:        true,
	}, nil
}

// RoundTrip implements http.RoundTripper
func (t *MTLSTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// If mTLS not enabled or this is not an agent endpoint, pass through
	if !t.enabled || !t.shouldUseMTLS(req) {
		return t.base.RoundTrip(req)
	}

	// Clone the request to avoid modifying the original
	reqClone := req.Clone(req.Context())

	// Add certificate header (base64 encoded)
	certBase64 := base64.StdEncoding.EncodeToString(t.certificatePEM)
	reqClone.Header.Set("X-Client-Certificate", certBase64)

	// Add timestamp for replay protection
	timestamp := time.Now().UTC().Format(time.RFC3339)
	reqClone.Header.Set("X-Request-Timestamp", timestamp)

	// Read request body for signing
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}
		// Restore body for the actual request
		reqClone.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	// Build signature payload: METHOD\nPATH\nTIMESTAMP\nBODY
	signaturePayload := buildSignaturePayload(req.Method, req.URL.Path, timestamp, bodyBytes)

	// Sign the payload
	signature, err := crypto.SignData(t.privateKey, signaturePayload)
	if err != nil {
		return nil, fmt.Errorf("failed to sign request: %w", err)
	}

	// Add signature header (base64 encoded)
	signatureBase64 := base64.StdEncoding.EncodeToString(signature)
	reqClone.Header.Set("X-Client-Signature", signatureBase64)

	// Make the request
	return t.base.RoundTrip(reqClone)
}

// shouldUseMTLS determines if this request should use mTLS headers
// Use mTLS for all API endpoints when available
func (t *MTLSTransport) shouldUseMTLS(req *http.Request) bool {
	// Use mTLS for all /api/* endpoints if client has certificate
	return strings.HasPrefix(req.URL.Path, "/api/")
}

// buildSignaturePayload creates the data that should be signed
// Format: METHOD\nPATH\nTIMESTAMP\nBODY
func buildSignaturePayload(method, path, timestamp string, body []byte) []byte {
	var buf bytes.Buffer
	buf.WriteString(strings.ToUpper(method))
	buf.WriteString("\n")
	buf.WriteString(path)
	buf.WriteString("\n")
	buf.WriteString(timestamp)
	buf.WriteString("\n")
	buf.Write(body)
	return buf.Bytes()
}
