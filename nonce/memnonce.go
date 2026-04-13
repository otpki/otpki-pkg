package nonce

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"sync"
)

// TODO: this is a simple in-memory nonce with no expiration
// Nonces should expire, and not be locked to a single process memory
// Ideal: redis to store nonces, SET with expiration, GET w/ delete flag for atomic usage

type LocalMemoryNonceManager struct {
	nonces map[string]string
	mutex  sync.RWMutex
}

func NewLocalMemoryNonceManager() *LocalMemoryNonceManager {
	return &LocalMemoryNonceManager{
		nonces: make(map[string]string),
	}
}

func (nm *LocalMemoryNonceManager) CreateNonce(_ context.Context) (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	nonce := base64.RawURLEncoding.EncodeToString(bytes)

	// Store in the map
	nm.mutex.Lock()
	nm.nonces[nonce] = nonce
	nm.mutex.Unlock()

	return nonce, nil
}

func (nm *LocalMemoryNonceManager) AddNonce(_ context.Context, nonce string) (string, error) {
	// Store in the map
	nm.mutex.Lock()
	nm.nonces[nonce] = nonce
	nm.mutex.Unlock()

	return nonce, nil
}

// Returns true if source contains nonce and then remove it, else return false
func (nm *LocalMemoryNonceManager) CheckNonce(_ context.Context, nonce string) (bool, error) {
	nm.mutex.Lock()
	defer nm.mutex.Unlock()

	_, exists := nm.nonces[nonce]
	if exists {
		delete(nm.nonces, nonce)
		return true, nil
	}
	return false, nil
}

// Returns True if Nonce did not exist and was added, else returns false
func (nm *LocalMemoryNonceManager) AddUniqueNonce(_ context.Context, nonce string) (bool, error) {
	nm.mutex.Lock()
	defer nm.mutex.Unlock()
	_, exists := nm.nonces[nonce]
	if exists {
		return false, nil
	}
	nm.nonces[nonce] = nonce
	return true, nil
}
