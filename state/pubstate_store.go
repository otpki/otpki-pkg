package state

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"strings"
	"time"

	"github.com/otpki/otpki-pkg/config"
	"github.com/otpki/otpki-pkg/helpers"
	spfystate "github.com/otpki/otpki-pkg/spfy/anchor"
)

type PubStateStore[K any, V any] struct {
	config    config.Config
	store     spfystate.Store[[]byte]
	newKey    func() K
	transform *Transform[V]
	keyPrefix string
}

type pubStateEnvelope[V any] struct {
	Data         V
	CreatedAt    time.Time
	MaxExpiresAt time.Time
	TTL          time.Duration
}

func NewPubStateStore[V any](
	conf config.Config,
	store spfystate.Store[[]byte],
	transform *Transform[V],
) *PubStateStore[string, V] {
	return NewPubStateStoreWithKey[string, V](conf, store, generateKey, transform)
}

func NewPubStateStoreWithKey[K any, V any](
	conf config.Config,
	store spfystate.Store[[]byte],
	newKey func() K,
	transform *Transform[V],
) *PubStateStore[K, V] {
	return &PubStateStore[K, V]{
		config:    conf,
		store:     store,
		newKey:    newKey,
		transform: transform,
		keyPrefix: conf.App.Keyspace.Prefix(),
	}
}

func (sm *PubStateStore[K, V]) Put(ctx context.Context, data V, ttl, maxTTL time.Duration) (K, error) {
	if sm.newKey == nil {
		var zero K
		return zero, ErrInvalidKey
	}

	key := sm.newKey()
	if err := sm.PutWithKey(ctx, key, data, ttl, maxTTL); err != nil {
		var zero K
		return zero, err
	}

	return key, nil
}

func (sm *PubStateStore[K, V]) PutWithKey(ctx context.Context, key K, data V, ttl, maxTTL time.Duration) error {
	keyString, err := keyToString(key)
	if err != nil {
		return err
	}

	encoded, err := sm.encode(ctx, data, ttl, maxTTL)
	if err != nil {
		return err
	}

	return sm.storeValue(ctx, keyString, encoded, ttl, maxTTL)
}

func (sm *PubStateStore[K, V]) Get(ctx context.Context, key K) (V, error) {
	return sm.get(ctx, key, true)
}

func (sm *PubStateStore[K, V]) Peek(ctx context.Context, key K) (V, error) {
	return sm.get(ctx, key, false)
}

func (sm *PubStateStore[K, V]) get(ctx context.Context, key K, renew bool) (V, error) {
	keyString, err := keyToString(key)
	if err != nil {
		var zero V
		return zero, err
	}
	rawKey := keyString
	keyString = sm.prefixedKey(keyString)

	entry, err := sm.store.Get(ctx, keyString)
	if err != nil {
		if isNotFoundError(err) {
			var zero V
			return zero, ErrStateNotFound
		}
		var zero V
		return zero, err
	}
	if len(entry.Value) == 0 {
		var zero V
		return zero, ErrStateNotFound
	}

	stateData, err := sm.decode(entry.Value)
	if err != nil {
		var zero V
		return zero, err
	}

	now := helpers.Now()
	if stateData.expired(now) {
		_ = sm.store.Delete(ctx, keyString)
		var zero V
		return zero, ErrStateNotFound
	}

	if renew && stateData.shouldRenew(now) {
		if stateData.canRenew(now) {
			stateData.CreatedAt = now
			if err := sm.storeEnvelope(ctx, rawKey, stateData); err != nil {
				var zero V
				return zero, err
			}
		}
	}

	return sm.load(ctx, stateData.Data)
}

func (sm *PubStateStore[K, V]) PeekMultiple(ctx context.Context, keys []K) ([]V, error) {
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

func (sm *PubStateStore[K, V]) Delete(ctx context.Context, key K) error {
	keyString, err := keyToString(key)
	if err != nil {
		return err
	}
	keyString = sm.prefixedKey(keyString)

	err = sm.store.Delete(ctx, keyString)
	if err != nil && isNotFoundError(err) {
		return nil
	}

	return err
}

func (sm *PubStateStore[K, V]) encode(ctx context.Context, data V, ttl, maxTTL time.Duration) ([]byte, error) {
	if sm.transform != nil && sm.transform.Store != nil {
		var err error
		data, err = sm.transform.Store(ctx, data)
		if err != nil {
			return nil, err
		}
	}

	now := helpers.Now()
	env := pubStateEnvelope[V]{
		Data:      data,
		CreatedAt: now,
		TTL:       ttl,
	}
	if maxTTL > 0 {
		env.MaxExpiresAt = now.Add(maxTTL)
	}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(env); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (sm *PubStateStore[K, V]) storeEnvelope(ctx context.Context, key string, env pubStateEnvelope[V]) error {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(env); err != nil {
		return err
	}

	return sm.storeValue(ctx, key, buf.Bytes(), env.TTL, time.Until(env.MaxExpiresAt))
}

func (sm *PubStateStore[K, V]) storeValue(
	ctx context.Context,
	key string,
	value []byte,
	ttl, maxTTL time.Duration,
) error {
	key = sm.prefixedKey(key)
	_, err := sm.store.Put(ctx, key, value, spfystate.WriteOptions{
		TTL: backendTTL(ttl, maxTTL),
	})
	return err
}

func (sm *PubStateStore[K, V]) decode(data []byte) (pubStateEnvelope[V], error) {
	var env pubStateEnvelope[V]
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&env); err != nil {
		return pubStateEnvelope[V]{}, err
	}

	return env, nil
}

func (sm *PubStateStore[K, V]) load(ctx context.Context, data V) (V, error) {
	if sm.transform == nil || sm.transform.Load == nil {
		return data, nil
	}
	return sm.transform.Load(ctx, data)
}

func (sm *PubStateStore[K, V]) prefixedKey(key string) string {
	if sm.keyPrefix == "" {
		return key
	}
	return sm.keyPrefix + "/" + key
}

func backendTTL(ttl, maxTTL time.Duration) time.Duration {
	backendTTL := max(ttl, maxTTL)
	if backendTTL <= 0 {
		return 0
	}

	return backendTTL
}

func (e pubStateEnvelope[V]) expired(now time.Time) bool {
	if e.TTL > 0 && now.After(e.CreatedAt.Add(e.TTL)) {
		return true
	}
	if !e.MaxExpiresAt.IsZero() && now.After(e.MaxExpiresAt) {
		return true
	}
	return false
}

func (e pubStateEnvelope[V]) shouldRenew(now time.Time) bool {
	return e.TTL > 0 && now.Sub(e.CreatedAt) > e.TTL/2
}

func (e pubStateEnvelope[V]) canRenew(now time.Time) bool {
	if e.TTL <= 0 {
		return false
	}
	if e.MaxExpiresAt.IsZero() {
		return true
	}
	return !now.Add(e.TTL).After(e.MaxExpiresAt)
}

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, spfystate.ErrNotFound) {
		return true
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") || strings.Contains(msg, "redis: nil")
}

var _ Store[string, any] = (*PubStateStore[string, any])(nil)
