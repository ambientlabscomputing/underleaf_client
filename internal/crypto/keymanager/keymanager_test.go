package keymanager

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestKeyManagerBasics(t *testing.T) {
	km, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("Failed to create key manager: %v", err)
	}
	defer km.Close()

	blob, err := km.Initialize()
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	if len(blob) == 0 {
		t.Error("Expected non-empty initialization blob")
	}

	plainKey := make([]byte, 32)
	rand.Read(plainKey)

	wrapped, err := km.WrapKey(plainKey)
	if err != nil {
		t.Fatalf("Failed to wrap: %v", err)
	}

	unwrapped, err := km.UnwrapKey(wrapped)
	if err != nil {
		t.Fatalf("Failed to unwrap: %v", err)
	}
	if !bytes.Equal(plainKey, unwrapped) {
		t.Error("Unwrapped key mismatch")
	}
}

func TestKeyManagerSealUnseal(t *testing.T) {
	km, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("Failed to create key manager: %v", err)
	}
	defer km.Close()

	if !km.IsSealed() {
		t.Error("Expected key manager to start sealed")
	}

	blob, err := km.Initialize()
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	if km.IsSealed() {
		t.Error("Expected key manager to be unsealed after init")
	}

	if err := km.Seal(); err != nil {
		t.Fatalf("Failed to seal: %v", err)
	}

	if !km.IsSealed() {
		t.Error("Expected key manager to be sealed")
	}

	// Should fail when sealed
	_, err = km.DeriveKey([]byte("test"), 32)
	if err != ErrSealedKey {
		t.Errorf("Expected ErrSealedKey, got: %v", err)
	}

	if err := km.Unseal(blob); err != nil {
		t.Fatalf("Failed to unseal: %v", err)
	}

	if km.IsSealed() {
		t.Error("Expected key manager to be unsealed")
	}

	// Should work after unseal
	_, err = km.DeriveKey([]byte("test"), 32)
	if err != nil {
		t.Errorf("Derive should work after unseal: %v", err)
	}
}

func TestKeyDerivation(t *testing.T) {
	km, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("Failed to create key manager: %v", err)
	}
	defer km.Close()

	_, err = km.Initialize()
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	// Same context should produce same key
	key1, err := km.DeriveKey([]byte("context1"), 32)
	if err != nil {
		t.Fatalf("Failed to derive key: %v", err)
	}

	key2, err := km.DeriveKey([]byte("context1"), 32)
	if err != nil {
		t.Fatalf("Failed to derive key: %v", err)
	}

	if !bytes.Equal(key1, key2) {
		t.Error("Same context should produce same key")
	}

	// Different context should produce different key
	key3, err := km.DeriveKey([]byte("context2"), 32)
	if err != nil {
		t.Fatalf("Failed to derive key: %v", err)
	}

	if bytes.Equal(key1, key3) {
		t.Error("Different context should produce different key")
	}
}

func TestEncryptionDecryption(t *testing.T) {
	km, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("Failed to create key manager: %v", err)
	}
	defer km.Close()

	_, err = km.Initialize()
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	testData := []byte("This is secret data that needs encryption")

	ciphertext, err := km.Encrypt(testData)
	if err != nil {
		t.Fatalf("Failed to encrypt: %v", err)
	}

	if bytes.Equal(testData, ciphertext) {
		t.Error("Ciphertext should be different from plaintext")
	}

	plaintext, err := km.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Failed to decrypt: %v", err)
	}

	if !bytes.Equal(testData, plaintext) {
		t.Error("Decrypted data doesn't match original")
	}

	// Multiple encryptions should produce different ciphertexts (nonce should be different)
	ciphertext2, err := km.Encrypt(testData)
	if err != nil {
		t.Fatalf("Failed to encrypt second time: %v", err)
	}

	if bytes.Equal(ciphertext, ciphertext2) {
		t.Error("Multiple encryptions should produce different ciphertexts")
	}

	// But both should decrypt to same plaintext
	plaintext2, err := km.Decrypt(ciphertext2)
	if err != nil {
		t.Fatalf("Failed to decrypt second ciphertext: %v", err)
	}

	if !bytes.Equal(testData, plaintext2) {
		t.Error("Second decryption doesn't match original")
	}
}

func TestBackendInfo(t *testing.T) {
	km, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("Failed to create key manager: %v", err)
	}
	defer km.Close()

	info := km.GetBackendInfo()
	if info.Type == "" {
		t.Error("Expected non-empty backend type")
	}
	if !info.Available {
		t.Error("Expected backend to be available")
	}
	if len(info.Features) == 0 {
		t.Error("Expected non-empty features list")
	}

	t.Logf("Backend: %s, Features: %v", info.Type, info.Features)
}

func TestErrorConditions(t *testing.T) {
	km, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("Failed to create key manager: %v", err)
	}
	defer km.Close()

	// Test operations before initialization (sealed state)
	_, err = km.DeriveKey([]byte("test"), 32)
	if err == nil {
		t.Error("Expected error when operating on sealed/uninitialized key manager")
	}

	_, err = km.WrapKey([]byte("test"))
	if err == nil {
		t.Error("Expected error when operating on sealed/uninitialized key manager")
	}

	_, err = km.Encrypt([]byte("test"))
	if err == nil {
		t.Error("Expected error when operating on sealed/uninitialized key manager")
	}

	// Initialize
	_, err = km.Initialize()
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	// Test double initialization
	_, err = km.Initialize()
	if err != ErrAlreadyInitialized {
		t.Errorf("Expected ErrAlreadyInitialized, got: %v", err)
	}

	// Test invalid wrapped key
	_, err = km.UnwrapKey([]byte("invalid"))
	if err == nil {
		t.Error("Expected error for invalid wrapped key")
	}

	// Test invalid ciphertext
	_, err = km.Decrypt([]byte("invalid"))
	if err == nil {
		t.Error("Expected error for invalid ciphertext")
	}
}
