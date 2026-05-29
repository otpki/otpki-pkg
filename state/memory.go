package state

import (
	"context"
	"sync"
	"time"

	"github.com/otpki/otpki-pkg/helpers"
)

// Even though pubstate.Store can provide an in-memory implementation,
// some consumers may still want the raw go-like state store. this is especially
// useful for creating a local state store when the application wide one is remote

type MemoryStateStore[K any, V any] struct {
	sessions  sync.Map // map[string]*memorySession[V]
	ticker    *time.Ticker
	newKey    func() K
	transform *Transform[V]
}

type memorySession[V any] struct {
	mu           sync.Mutex
	Data         V
	CreatedAt    time.Time
	ExpiresAt    time.Time
	MaxExpiresAt time.Time // When session must be destroyed (max TTL)
	TTL          time.Duration
}

func NewMemoryStateStore[V any](reaperInterval time.Duration, transform *Transform[V]) *MemoryStateStore[string, V] {
	return NewMemoryStateStoreWithKey[string, V](reaperInterval, generateKey, transform)
}

func NewMemoryStateStoreWithKey[K any, V any](
	reaperInterval time.Duration,
	newKey func() K,
	transform *Transform[V],
) *MemoryStateStore[K, V] {
	sm := &MemoryStateStore[K, V]{
		sessions:  sync.Map{},
		ticker:    time.NewTicker(reaperInterval),
		newKey:    newKey,
		transform: transform,
	}

	// start cleanup goroutine
	go sm.cleanupExpiredSessions()

	return sm
}

func (sm *MemoryStateStore[K, V]) Put(ctx context.Context, data V, ttl, maxTTL time.Duration) (K, error) {
	if sm.newKey == nil {
		var zero K
		return zero, ErrInvalidKey
	}

	key := sm.newKey()
	err := sm.PutWithKey(ctx, key, data, ttl, maxTTL)
	if err != nil {
		var zero K
		return zero, err
	}
	return key, nil
}

func (sm *MemoryStateStore[K, V]) PutWithKey(ctx context.Context, key K, data V, ttl, maxTTL time.Duration) error {
	keyString, err := keyToString(key)
	if err != nil {
		return err
	}
	if sm.transform != nil && sm.transform.Store != nil {
		data, err = sm.transform.Store(ctx, data)
		if err != nil {
			return err
		}
	}

	now := helpers.Now()

	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = now.Add(ttl)
	}

	var maxExpiresAt time.Time
	if maxTTL > 0 {
		maxExpiresAt = now.Add(maxTTL)
	}

	sm.sessions.Store(keyString, &memorySession[V]{
		Data:         data,
		CreatedAt:    now,
		ExpiresAt:    expiresAt,    // zero means "no TTL expiration"
		MaxExpiresAt: maxExpiresAt, // zero means "no max TTL"
		TTL:          ttl,
	})

	return nil
}

func (sm *MemoryStateStore[K, V]) Get(ctx context.Context, key K) (V, error) {
	return sm.get(ctx, key, true)
}

func (sm *MemoryStateStore[K, V]) Peek(ctx context.Context, key K) (V, error) {
	return sm.get(ctx, key, false)
}

func (sm *MemoryStateStore[K, V]) get(ctx context.Context, key K, renew bool) (V, error) {
	keyString, err := keyToString(key)
	if err != nil {
		var zero V
		return zero, err
	}

	value, exists := sm.sessions.Load(keyString)
	if !exists {
		var zero V
		return zero, ErrStateNotFound
	}

	session := value.(*memorySession[V])
	now := helpers.Now()

	session.mu.Lock()
	defer session.mu.Unlock()

	// check TTL expiration (only if ExpiresAt is set)
	if !session.ExpiresAt.IsZero() && now.After(session.ExpiresAt) {
		sm.sessions.CompareAndDelete(keyString, value)
		var zero V
		return zero, ErrStateNotFound
	}

	// check max TTL
	if !session.MaxExpiresAt.IsZero() && now.After(session.MaxExpiresAt) {
		sm.sessions.CompareAndDelete(keyString, value)
		var zero V
		return zero, ErrStateNotFound
	}

	// sliding expiration only when TTL > 0 and renew enabled
	if renew && session.TTL > 0 {
		elapsed := now.Sub(session.CreatedAt)
		if elapsed > session.TTL/2 {
			// don't renew past max TTL
			if !session.MaxExpiresAt.IsZero() && now.Add(session.TTL).After(session.MaxExpiresAt) {
				return sm.load(ctx, session.Data)
			}

			session.CreatedAt = now
			session.ExpiresAt = now.Add(session.TTL)
		}
	}

	return sm.load(ctx, session.Data)
}

func (sm *MemoryStateStore[K, V]) PeekMultiple(ctx context.Context, keys []K) ([]V, error) {
	results := make([]V, 0, len(keys))
	for _, key := range keys {
		data, err := sm.Peek(ctx, key)
		if err != nil {
			return nil, err
		}
		results = append(results, data)
	}

	return results, nil
}

func (sm *MemoryStateStore[K, V]) Delete(ctx context.Context, key K) error {
	keyString, err := keyToString(key)
	if err != nil {
		return err
	}

	sm.sessions.Delete(keyString)
	return nil
}

func (sm *MemoryStateStore[K, V]) Shutdown() {
	sm.ticker.Stop()
}

func (sm *MemoryStateStore[K, V]) load(ctx context.Context, data V) (V, error) {
	if sm.transform == nil || sm.transform.Load == nil {
		return data, nil
	}
	return sm.transform.Load(ctx, data)
}

func (sm *MemoryStateStore[K, V]) cleanupExpiredSessions() {
	for range sm.ticker.C {
		now := helpers.Now()
		sm.sessions.Range(func(key, value any) bool {
			session, ok := value.(*memorySession[V])
			if !ok {
				return true
			}

			session.mu.Lock()
			expiredByTTL := !session.ExpiresAt.IsZero() && now.After(session.ExpiresAt)
			expiredByMax := !session.MaxExpiresAt.IsZero() && now.After(session.MaxExpiresAt)
			session.mu.Unlock()

			if expiredByTTL || expiredByMax {
				sm.sessions.CompareAndDelete(key, value)
			}
			return true
		})
	}
}
