package state

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/samber/oops"
)

var ErrStateNotFound = oops.New("state not found")
var ErrInvalidKey = oops.New("invalid key")

// state.Store is a generic interface for storing and retrieving state data.
// K is the key type, V is the stored value type.
type Store[K any, V any] interface {
	// Put stores data and returns a generated key. ttl is the active state length, maxTTL is the maximum state length.
	// maxTTL of 0 means no maximum state length.
	Put(ctx context.Context, data V, ttl, maxTTL time.Duration) (key K, err error)
	// PutWithKey stores data using a caller-provided key.
	PutWithKey(ctx context.Context, key K, data V, ttl, maxTTL time.Duration) error
	// Get returns data if the key is valid. It will also renew data if it is still valid.
	Get(ctx context.Context, key K) (V, error)
	// Peek returns data if the key is valid without renewing the session.
	Peek(ctx context.Context, key K) (V, error)
	// PeekMultiple returns data for all provided keys without renewing sessions.
	PeekMultiple(ctx context.Context, keys []K) ([]V, error)
	// Delete will destroy state and remove it from the store.
	Delete(ctx context.Context, key K) error
	Shutdown()
}

type Transform[V any] struct {
	Store func(ctx context.Context, data V) (V, error)
	Load  func(ctx context.Context, data V) (V, error)
}

func generateKey() string {
	bytes := make([]byte, 32)
	rand.Read(bytes)
	return base64.URLEncoding.EncodeToString(bytes)
}

func keyToString[K any](key K) (string, error) {
	keyString := strings.TrimSpace(fmt.Sprint(key))
	if keyString == "" {
		return "", ErrInvalidKey
	}

	return keyString, nil
}
