package cluster

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	// JoinCodeLength is the length of the human-readable join code (8 characters)
	JoinCodeLength = 8
	// JoinCodeAlphabet is the set of characters used in join codes (excludes ambiguous: 0, O, 1, I, l)
	JoinCodeAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	// JoinTokenTTL is how long a join token remains valid
	JoinTokenTTL = 1 * time.Hour
)

// JoinToken represents credentials for joining a cluster
type JoinToken struct {
	// Code is the human-readable short code for verification (e.g., "A7K2-N9P4")
	Code string `json:"code"`
	// FullToken is the complete token including metadata (stored on agent temporarily)
	FullToken string `json:"full_token"`
	// TokenHash is the SHA-256 hash of FullToken (stored in server_api for validation)
	TokenHash string `json:"token_hash"`
	// ClusterID is the cluster this token grants access to
	ClusterID string `json:"cluster_id"`
	// CreatedBy is the node ID that generated this token
	CreatedBy string `json:"created_by"`
	// CreatedAt is when this token was generated
	CreatedAt time.Time `json:"created_at"`
	// ExpiresAt is when this token becomes invalid
	ExpiresAt time.Time `json:"expires_at"`
	// Fingerprint is the CA fingerprint for this cluster (for additional verification)
	Fingerprint string `json:"fingerprint"`
}

// GenerateJoinToken creates a new join token with a human-readable code
func GenerateJoinToken(clusterID, createdBy, caFingerprint string) (*JoinToken, error) {
	// Generate 8-character alphanumeric code
	code, err := generateJoinCode()
	if err != nil {
		return nil, fmt.Errorf("failed to generate join code: %w", err)
	}

	// Generate full token (code + random entropy)
	entropy := make([]byte, 32)
	if _, err := rand.Read(entropy); err != nil {
		return nil, fmt.Errorf("failed to generate token entropy: %w", err)
	}
	fullToken := fmt.Sprintf("%s-%s", code, hex.EncodeToString(entropy))

	// Hash the full token for storage
	hash := sha256.Sum256([]byte(fullToken))
	tokenHash := hex.EncodeToString(hash[:])

	now := time.Now()
	token := &JoinToken{
		Code:        formatJoinCode(code),
		FullToken:   fullToken,
		TokenHash:   tokenHash,
		ClusterID:   clusterID,
		CreatedBy:   createdBy,
		CreatedAt:   now,
		ExpiresAt:   now.Add(JoinTokenTTL),
		Fingerprint: caFingerprint,
	}

	return token, nil
}

// generateJoinCode creates a random 8-character code using the safe alphabet
func generateJoinCode() (string, error) {
	code := make([]byte, JoinCodeLength)
	for i := 0; i < JoinCodeLength; i++ {
		// Generate random index into alphabet
		idx := make([]byte, 1)
		if _, err := rand.Read(idx); err != nil {
			return "", err
		}
		code[i] = JoinCodeAlphabet[int(idx[0])%len(JoinCodeAlphabet)]
	}
	return string(code), nil
}

// formatJoinCode formats a join code with a hyphen for readability (e.g., "A7K2-N9P4")
func formatJoinCode(code string) string {
	if len(code) != JoinCodeLength {
		return code
	}
	return fmt.Sprintf("%s-%s", code[:4], code[4:])
}

// ValidateJoinCode checks if a provided code matches the expected format
func ValidateJoinCode(code string) bool {
	// Remove hyphens and spaces
	cleaned := strings.ReplaceAll(strings.ReplaceAll(code, "-", ""), " ", "")
	cleaned = strings.ToUpper(cleaned)

	if len(cleaned) != JoinCodeLength {
		return false
	}

	// Check all characters are in alphabet
	for _, ch := range cleaned {
		if !strings.ContainsRune(JoinCodeAlphabet, ch) {
			return false
		}
	}

	return true
}

// HashToken generates a SHA-256 hash of a token string
func HashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// IsTokenExpired checks if a join token has expired
func IsTokenExpired(token *JoinToken) bool {
	return time.Now().After(token.ExpiresAt)
}

// CompareFingerprintsMatch verifies two CA fingerprints match (case-insensitive, ignores colons)
func CompareFingerprintsMatch(fp1, fp2 string) bool {
	clean1 := strings.ToLower(strings.ReplaceAll(fp1, ":", ""))
	clean2 := strings.ToLower(strings.ReplaceAll(fp2, ":", ""))
	return clean1 == clean2
}

// FormatFingerprint formats a hex fingerprint with colons for display (e.g., "A7:B3:2F:...")
func FormatFingerprint(fingerprint string) string {
	cleaned := strings.ReplaceAll(strings.ToUpper(fingerprint), ":", "")
	if len(cleaned) < 2 {
		return fingerprint
	}

	var formatted strings.Builder
	for i := 0; i < len(cleaned); i += 2 {
		if i > 0 {
			formatted.WriteString(":")
		}
		end := i + 2
		if end > len(cleaned) {
			end = len(cleaned)
		}
		formatted.WriteString(cleaned[i:end])
	}
	return formatted.String()
}

// TrustVerification represents the data exchanged during trust ceremony
type TrustVerification struct {
	// NodeID is the ID of the node requesting to join
	NodeID string `json:"node_id"`
	// Code is the short code provided by the user
	Code string `json:"code"`
	// Fingerprint is the CA fingerprint for verification
	Fingerprint string `json:"fingerprint"`
	// RaftAddr is the Raft address of the requesting node
	RaftAddr string `json:"raft_addr"`
}

// ValidateTrustVerification checks if a trust verification request is valid
func ValidateTrustVerification(tv *TrustVerification, expectedToken *JoinToken) error {
	if tv == nil {
		return fmt.Errorf("trust verification is nil")
	}

	if expectedToken == nil {
		return fmt.Errorf("expected token is nil")
	}

	// Check token expiration
	if IsTokenExpired(expectedToken) {
		return fmt.Errorf("join token has expired")
	}

	// Validate code format
	if !ValidateJoinCode(tv.Code) {
		return fmt.Errorf("invalid join code format")
	}

	// Clean and compare codes
	cleanedProvided := strings.ReplaceAll(strings.ReplaceAll(strings.ToUpper(tv.Code), "-", ""), " ", "")
	cleanedExpected := strings.ReplaceAll(strings.ReplaceAll(strings.ToUpper(expectedToken.Code), "-", ""), " ", "")

	if cleanedProvided != cleanedExpected {
		return fmt.Errorf("join code does not match")
	}

	// Verify fingerprint if provided
	if tv.Fingerprint != "" && expectedToken.Fingerprint != "" {
		if !CompareFingerprintsMatch(tv.Fingerprint, expectedToken.Fingerprint) {
			return fmt.Errorf("CA fingerprint mismatch")
		}
	}

	return nil
}
