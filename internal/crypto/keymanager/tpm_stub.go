//go:build !linux
// +build !linux

package keymanager

import "fmt"

func newTPMKeyManager(cfg *Config) (KeyManager, error) {
	return nil, fmt.Errorf("TPM 2.0 backend is only available on Linux")
}
