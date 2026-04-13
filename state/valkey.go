package state

import (
	"bytes"
	"context"
	"encoding/gob"
	"strings"
	"time"

	"github.com/otpki/otpki-pkg/helpers"
	valkey "github.com/redis/go-redis/v9"
)

type ValkeyStateStore[K any, V any] struct {
	client    valkey.UniversalClient
	prefix    string
	newKey    func() K
	transform *Transform[V]
}

type valkeySessionData[V any] struct {
	Data         V
	CreatedAt    time.Time
	MaxExpiresAt time.Time // When session must be destroyed (max TTL)
	TTL          time.Duration
}

func NewValkeyStateStore[V any](client valkey.UniversalClient, prefix string, transform *Transform[V]) *ValkeyStateStore[string, V] {
	return NewValkeyStateStoreWithKey[string, V](client, prefix, generateKey, transform)
}

func NewValkeyStateStoreWithKey[K any, V any](client valkey.UniversalClient, prefix string, newKey func() K, transform *Transform[V]) *ValkeyStateStore[K, V] {
	if !strings.HasSuffix(prefix, ":") {
		prefix += ":"
	}
	return &ValkeyStateStore[K, V]{
		client:    client,
		prefix:    prefix,
		newKey:    newKey,
		transform: transform,
	}
}

func (sm *ValkeyStateStore[K, V]) Put(ctx context.Context, data V, ttl, maxTTL time.Duration) (K, error) {
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

func (sm *ValkeyStateStore[K, V]) PutWithKey(ctx context.Context, key K, data V, ttl, maxTTL time.Duration) error {
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

	// wrap the data with metadata
	now := helpers.Now()
	var maxExpiresAt time.Time
	if maxTTL > 0 {
		maxExpiresAt = now.Add(maxTTL)
	}

	sessionData := valkeySessionData[V]{
		Data:         data,
		CreatedAt:    now,
		MaxExpiresAt: maxExpiresAt,
		TTL:          ttl,
	}

	// serialize the wrapped data using gob
	buf := &bytes.Buffer{}
	encoder := gob.NewEncoder(buf)
	err = encoder.Encode(sessionData)
	if err != nil {
		return err
	}

	// store in Valkey with TTL
	err = sm.client.Set(ctx, sm.prefix+keyString, buf.Bytes(), ttl).Err()
	if err != nil {
		return err
	}

	return nil
}

func (sm *ValkeyStateStore[K, V]) Get(ctx context.Context, key K) (V, error) {
	fetched, err := sm.peekMultipleWithMetadata(ctx, []K{key})
	if err != nil {
		var zero V
		return zero, err
	}

	sessionData := fetched[0].sessionData
	data := sessionData.Data

	// sliding expiration, renew if more than 50% through TTL
	// and session hasn't exceeded max TTL
	now := helpers.Now()
	elapsed := now.Sub(sessionData.CreatedAt)
	if elapsed > sessionData.TTL/2 {
		// check if renewing would exceed max TTL
		if !sessionData.MaxExpiresAt.IsZero() && now.Add(sessionData.TTL).After(sessionData.MaxExpiresAt) {
			// cannot renew - would exceed max TTL
			// return data but don't renew
			return data, nil
		}

		// update the created time and store back
		sessionData.CreatedAt = now
		buf := &bytes.Buffer{}
		encoder := gob.NewEncoder(buf)
		err = encoder.Encode(sessionData)
		if err != nil {
			var zero V
			return zero, err
		}

		// update the session with new data and reset TTL
		err = sm.client.Set(ctx, sm.prefix+fetched[0].keyString, buf.Bytes(), sessionData.TTL).Err()
		if err != nil {
			var zero V
			return zero, err
		}
	}

	return sm.load(ctx, data)
}

func (sm *ValkeyStateStore[K, V]) Peek(ctx context.Context, key K) (V, error) {
	results, err := sm.PeekMultiple(ctx, []K{key})
	if err != nil {
		var zero V
		return zero, err
	}
	if len(results) == 0 {
		var zero V
		return zero, ErrStateNotFound
	}

	return results[0], nil
}

func (sm *ValkeyStateStore[K, V]) PeekMultiple(ctx context.Context, keys []K) ([]V, error) {
	fetched, err := sm.peekMultipleWithMetadata(ctx, keys)
	if err != nil {
		return nil, err
	}

	results := make([]V, 0, len(fetched))
	for _, item := range fetched {
		data, err := sm.load(ctx, item.sessionData.Data)
		if err != nil {
			return nil, err
		}
		results = append(results, data)
	}

	return results, nil
}

type fetchedSession[V any] struct {
	keyString   string
	sessionData valkeySessionData[V]
}

func (sm *ValkeyStateStore[K, V]) peekMultipleWithMetadata(ctx context.Context, keys []K) ([]fetchedSession[V], error) {
	if len(keys) == 0 {
		return []fetchedSession[V]{}, nil
	}

	keyStrings := make([]string, 0, len(keys))
	redisKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		keyString, err := keyToString(key)
		if err != nil {
			return nil, err
		}
		keyStrings = append(keyStrings, keyString)
		redisKeys = append(redisKeys, sm.prefix+keyString)
	}

	values, err := sm.client.MGet(ctx, redisKeys...).Result()
	if err != nil {
		return nil, err
	}

	now := helpers.Now()
	fetched := make([]fetchedSession[V], 0, len(values))

	for i, value := range values {
		if value == nil {
			return nil, ErrStateNotFound
		}

		var dataBytes []byte
		switch v := value.(type) {
		case string:
			dataBytes = []byte(v)
		case []byte:
			dataBytes = v
		default:
			return nil, ErrStateNotFound
		}

		var sessionData valkeySessionData[V]
		buf := bytes.NewBuffer(dataBytes)
		decoder := gob.NewDecoder(buf)
		if err := decoder.Decode(&sessionData); err != nil {
			return nil, err
		}

		if !sessionData.MaxExpiresAt.IsZero() && now.After(sessionData.MaxExpiresAt) {
			_, _ = sm.client.Del(ctx, sm.prefix+keyStrings[i]).Result()
			return nil, ErrStateNotFound
		}

		fetched = append(fetched, fetchedSession[V]{
			keyString:   keyStrings[i],
			sessionData: sessionData,
		})
	}

	return fetched, nil
}

func (sm *ValkeyStateStore[K, V]) Delete(ctx context.Context, key K) error {
	keyString, err := keyToString(key)
	if err != nil {
		return err
	}

	return sm.client.Del(ctx, sm.prefix+keyString).Err()
}

func (sm *ValkeyStateStore[K, V]) Shutdown() {
	return
}

func (sm *ValkeyStateStore[K, V]) load(ctx context.Context, data V) (V, error) {
	if sm.transform == nil || sm.transform.Load == nil {
		return data, nil
	}
	return sm.transform.Load(ctx, data)
}
