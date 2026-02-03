//go:build linux
// +build linux

package keymanager

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/google/go-tpm/tpm2"
	"github.com/google/go-tpm/tpm2/transport"
	"golang.org/x/crypto/hkdf"
)

// tpmKeyManager implements KeyManager using TPM 2.0
type tpmKeyManager struct {
	mu            sync.RWMutex
	config        *Config
	tpm           transport.TPMCloser
	srkHandle     tpm2.TPMHandle
	masterKey     []byte
	wrappingKey   []byte
	encryptionKey []byte
	initialized   bool
	sealed        bool
	tpmInfo       *BackendInfo
}

const srkHandleValue = 0x81000001

func newTPMKeyManager(cfg *Config) (KeyManager, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	tpmPath := cfg.TPMDevicePath
	if tpmPath == "" {
		tpmPath = detectTPMPath()
	}

	tpmDevice, err := transport.OpenTPM(tpmPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open TPM device at %s: %w", tpmPath, err)
	}

	km := &tpmKeyManager{
		config:    cfg,
		tpm:       tpmDevice,
		srkHandle: tpm2.TPMHandle(srkHandleValue),
		sealed:    true,
	}

	if err := km.initTPMInfo(); err != nil {
		tpmDevice.Close()
		return nil, fmt.Errorf("failed to get TPM info: %w", err)
	}

	if err := km.ensureSRK(); err != nil {
		tpmDevice.Close()
		return nil, fmt.Errorf("failed to ensure SRK: %w", err)
	}

	return km, nil
}

func (km *tpmKeyManager) Initialize() ([]byte, error) {
	km.mu.Lock()
	defer km.mu.Unlock()

	if km.initialized {
		return nil, ErrAlreadyInitialized
	}

	masterKey := make([]byte, masterKeySize)
	if _, err := io.ReadFull(km.config.RandomReader, masterKey); err != nil {
		return nil, fmt.Errorf("failed to generate master key: %w", err)
	}

	sealedBlob, err := km.sealToTPM(masterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to seal master key to TPM: %w", err)
	}

	km.masterKey = masterKey
	km.initialized = true
	km.sealed = false

	if err := km.deriveSubKeys(); err != nil {
		return nil, err
	}

	return sealedBlob, nil
}

func (km *tpmKeyManager) Seal() error {
	km.mu.Lock()
	defer km.mu.Unlock()

	if !km.initialized {
		return ErrNotInitialized
	}

	if km.sealed {
		return nil
	}

	if km.masterKey != nil {
		for i := range km.masterKey {
			km.masterKey[i] = 0
		}
		km.masterKey = nil
	}
	if km.wrappingKey != nil {
		for i := range km.wrappingKey {
			km.wrappingKey[i] = 0
		}
		km.wrappingKey = nil
	}
	if km.encryptionKey != nil {
		for i := range km.encryptionKey {
			km.encryptionKey[i] = 0
		}
		km.encryptionKey = nil
	}

	km.sealed = true
	return nil
}

func (km *tpmKeyManager) Unseal(masterKeyBlob []byte) error {
	km.mu.Lock()
	defer km.mu.Unlock()

	if !km.initialized && len(masterKeyBlob) > 0 {
		km.initialized = true
	}

	if !km.initialized {
		return ErrNotInitialized
	}

	if !km.sealed {
		return nil
	}

	masterKey, err := km.unsealFromTPM(masterKeyBlob)
	if err != nil {
		return fmt.Errorf("failed to unseal master key from TPM: %w", err)
	}

	km.masterKey = masterKey
	km.sealed = false

	if err := km.deriveSubKeys(); err != nil {
		return err
	}

	return nil
}

func (km *tpmKeyManager) IsSealed() bool {
	km.mu.RLock()
	defer km.mu.RUnlock()
	return km.sealed
}

func (km *tpmKeyManager) IsInitialized() bool {
	km.mu.RLock()
	defer km.mu.RUnlock()
	return km.initialized
}

func (km *tpmKeyManager) DeriveKey(context []byte, keyLength int) ([]byte, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	if km.sealed {
		return nil, ErrSealedKey
	}

	if !km.initialized {
		return nil, ErrNotInitialized
	}

	hkdfReader := hkdf.New(sha256.New, km.masterKey, nil, context)
	derivedKey := make([]byte, keyLength)
	if _, err := io.ReadFull(hkdfReader, derivedKey); err != nil {
		return nil, fmt.Errorf("key derivation failed: %w", err)
	}

	return derivedKey, nil
}

func (km *tpmKeyManager) WrapKey(plainKey []byte) ([]byte, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	if km.sealed {
		return nil, ErrSealedKey
	}

	if !km.initialized {
		return nil, ErrNotInitialized
	}

	block, err := aes.NewCipher(km.wrappingKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(km.config.RandomReader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, plainKey, nil)
	wrapped := make([]byte, 1+len(nonce)+len(ciphertext))
	wrapped[0] = versionByte
	copy(wrapped[1:], nonce)
	copy(wrapped[1+len(nonce):], ciphertext)

	return wrapped, nil
}

func (km *tpmKeyManager) UnwrapKey(wrappedKey []byte) ([]byte, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	if km.sealed {
		return nil, ErrSealedKey
	}

	if !km.initialized {
		return nil, ErrNotInitialized
	}

	if len(wrappedKey) < 1+nonceSize {
		return nil, fmt.Errorf("invalid wrapped key size")
	}

	version := wrappedKey[0]
	if version != versionByte {
		return nil, fmt.Errorf("unsupported wrapped key version: %d", version)
	}

	nonce := wrappedKey[1 : 1+nonceSize]
	ciphertext := wrappedKey[1+nonceSize:]

	block, err := aes.NewCipher(km.wrappingKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt key: %w", err)
	}

	return plaintext, nil
}

func (km *tpmKeyManager) Encrypt(plaintext []byte) ([]byte, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	if km.sealed {
		return nil, ErrSealedKey
	}

	if !km.initialized {
		return nil, ErrNotInitialized
	}

	block, err := aes.NewCipher(km.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(km.config.RandomReader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	encrypted := make([]byte, 1+4+len(nonce)+len(ciphertext))
	encrypted[0] = versionByte
	binary.BigEndian.PutUint32(encrypted[1:5], uint32(len(plaintext)))
	copy(encrypted[5:], nonce)
	copy(encrypted[5+len(nonce):], ciphertext)

	return encrypted, nil
}

func (km *tpmKeyManager) Decrypt(ciphertext []byte) ([]byte, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	if km.sealed {
		return nil, ErrSealedKey
	}

	if !km.initialized {
		return nil, ErrNotInitialized
	}

	if len(ciphertext) < 1+4+nonceSize {
		return nil, fmt.Errorf("invalid ciphertext size")
	}

	version := ciphertext[0]
	if version != versionByte {
		return nil, fmt.Errorf("unsupported ciphertext version: %d", version)
	}

	nonce := ciphertext[5 : 5+nonceSize]
	ct := ciphertext[5+nonceSize:]

	block, err := aes.NewCipher(km.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	return plaintext, nil
}

func (km *tpmKeyManager) GetBackendInfo() BackendInfo {
	km.mu.RLock()
	defer km.mu.RUnlock()

	if km.tpmInfo != nil {
		return *km.tpmInfo
	}

	return BackendInfo{
		Type:      BackendTPM,
		Available: true,
		Features:  []string{"tpm2.0", "aes-256-gcm", "hkdf-sha256"},
	}
}

func (km *tpmKeyManager) Close() error {
	if err := km.Seal(); err != nil {
		return err
	}
	if km.tpm != nil {
		return km.tpm.Close()
	}
	return nil
}

func (km *tpmKeyManager) deriveSubKeys() error {
	wrappingKey, err := km.deriveKeyInternal([]byte("key-wrapping"), 32)
	if err != nil {
		return fmt.Errorf("failed to derive wrapping key: %w", err)
	}
	km.wrappingKey = wrappingKey

	encryptionKey, err := km.deriveKeyInternal([]byte("direct-encryption"), 32)
	if err != nil {
		return fmt.Errorf("failed to derive encryption key: %w", err)
	}
	km.encryptionKey = encryptionKey

	return nil
}

func (km *tpmKeyManager) deriveKeyInternal(context []byte, keyLength int) ([]byte, error) {
	hkdfReader := hkdf.New(sha256.New, km.masterKey, nil, context)
	derivedKey := make([]byte, keyLength)
	if _, err := io.ReadFull(hkdfReader, derivedKey); err != nil {
		return nil, fmt.Errorf("key derivation failed: %w", err)
	}
	return derivedKey, nil
}

func (km *tpmKeyManager) sealToTPM(data []byte) ([]byte, error) {
	createSeal := tpm2.Create{
		ParentHandle: tpm2.AuthHandle{
			Handle: km.srkHandle,
			Auth:   tpm2.PasswordAuth(nil),
		},
		InPublic: tpm2.New2B(tpm2.TPMTPublic{
			Type:    tpm2.TPMAlgKeyedHash,
			NameAlg: tpm2.TPMAlgSHA256,
			ObjectAttributes: tpm2.TPMAObject{
				FixedTPM:            true,
				FixedParent:         true,
				UserWithAuth:        true,
				NoDA:                true,
				SensitiveDataOrigin: false,
			},
			Parameters: tpm2.NewTPMUPublicParms(
				tpm2.TPMAlgKeyedHash,
				&tpm2.TPMSKeyedHashParms{
					Scheme: tpm2.TPMTKeyedHashScheme{
						Scheme: tpm2.TPMAlgNull,
					},
				},
			),
		}),
		InSensitive: tpm2.TPM2BSensitiveCreate{
			Sensitive: &tpm2.TPMSSensitiveCreate{
				Data: tpm2.NewTPMUSensitiveCreate(&tpm2.TPM2BSensitiveData{
					Buffer: data,
				}),
			},
		},
	}

	rsp, err := createSeal.Execute(km.tpm)
	if err != nil {
		return nil, fmt.Errorf("TPM Create failed: %w", err)
	}

	privBytes, err := rsp.OutPrivate.Bytes()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal private: %w", err)
	}
	pubBytes, err := rsp.OutPublic.Bytes()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal public: %w", err)
	}

	blob := make([]byte, 1+4+len(privBytes)+len(pubBytes))
	blob[0] = versionByte
	binary.BigEndian.PutUint32(blob[1:5], uint32(len(privBytes)))
	copy(blob[5:], privBytes)
	copy(blob[5+len(privBytes):], pubBytes)

	return blob, nil
}

func (km *tpmKeyManager) unsealFromTPM(blob []byte) ([]byte, error) {
	if len(blob) < 5 {
		return nil, fmt.Errorf("invalid sealed blob size")
	}

	version := blob[0]
	if version != versionByte {
		return nil, fmt.Errorf("unsupported sealed blob version: %d", version)
	}

	privLen := binary.BigEndian.Uint32(blob[1:5])
	if len(blob) < int(5+privLen) {
		return nil, fmt.Errorf("invalid sealed blob: insufficient data")
	}

	privBytes := blob[5 : 5+privLen]
	pubBytes := blob[5+privLen:]

	var inPrivate tpm2.TPM2BPrivate
	if err := inPrivate.Unmarshal(privBytes); err != nil {
		return nil, fmt.Errorf("failed to unmarshal private: %w", err)
	}

	var inPublic tpm2.TPM2BPublic
	if err := inPublic.Unmarshal(pubBytes); err != nil {
		return nil, fmt.Errorf("failed to unmarshal public: %w", err)
	}

	load := tpm2.Load{
		ParentHandle: tpm2.AuthHandle{
			Handle: km.srkHandle,
			Auth:   tpm2.PasswordAuth(nil),
		},
		InPrivate: inPrivate,
		InPublic:  inPublic,
	}

	loadRsp, err := load.Execute(km.tpm)
	if err != nil {
		return nil, fmt.Errorf("TPM Load failed: %w", err)
	}
	defer func() {
		flush := tpm2.FlushContext{FlushHandle: loadRsp.ObjectHandle}
		flush.Execute(km.tpm)
	}()

	unseal := tpm2.Unseal{
		ItemHandle: tpm2.AuthHandle{
			Handle: loadRsp.ObjectHandle,
			Auth:   tpm2.PasswordAuth(nil),
		},
	}

	unsealRsp, err := unseal.Execute(km.tpm)
	if err != nil {
		return nil, fmt.Errorf("TPM Unseal failed: %w", err)
	}

	return unsealRsp.OutData.Buffer, nil
}

func (km *tpmKeyManager) ensureSRK() error {
	readPub := tpm2.ReadPublic{
		ObjectHandle: km.srkHandle,
	}

	_, err := readPub.Execute(km.tpm)
	if err == nil {
		return nil
	}

	createPrimary := tpm2.CreatePrimary{
		PrimaryHandle: tpm2.TPMRHOwner,
		InPublic:      tpm2.New2B(tpm2.RSASRKTemplate),
	}

	rsp, err := createPrimary.Execute(km.tpm)
	if err != nil {
		return fmt.Errorf("failed to create SRK: %w", err)
	}

	evictControl := tpm2.EvictControl{
		Auth: tpm2.TPMRHOwner,
		ObjectHandle: &tpm2.NamedHandle{
			Handle: rsp.ObjectHandle,
			Name:   rsp.Name,
		},
		PersistentHandle: km.srkHandle,
	}

	_, err = evictControl.Execute(km.tpm)
	if err != nil {
		return fmt.Errorf("failed to persist SRK: %w", err)
	}

	return nil
}

func (km *tpmKeyManager) initTPMInfo() error {
	getCap := tpm2.GetCapability{
		Capability:    tpm2.TPMCapTPMProperties,
		Property:      uint32(tpm2.TPMPTManufacturer),
		PropertyCount: 1,
	}

	rsp, err := getCap.Execute(km.tpm)
	if err != nil {
		return fmt.Errorf("failed to get TPM capabilities: %w", err)
	}

	info := &BackendInfo{
		Type:      BackendTPM,
		Available: true,
		Features:  []string{"tpm2.0", "aes-256-gcm", "hkdf-sha256", "hardware-sealed"},
		Metadata:  make(map[string]interface{}),
	}

	if rsp.CapabilityData.Data.TPMProperties != nil && len(rsp.CapabilityData.Data.TPMProperties.TPMProperty) > 0 {
		mfr := rsp.CapabilityData.Data.TPMProperties.TPMProperty[0].Value
		info.Metadata["manufacturer"] = fmt.Sprintf("0x%08x", mfr)
	}

	km.tpmInfo = info
	return nil
}

func detectTPMPath() string {
	paths := []string{"/dev/tpmrm0", "/dev/tpm0"}
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return "/dev/tpmrm0"
}
