package asynqwrap

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/redis/go-redis/v9"
	"github.com/samber/oops"
)

const recurringStateRedisKeyPrefix = "asynqwrap:recurring-state:"

type recurringStateStore interface {
	Upsert(ctx context.Context, key string, version string) (bool, error)
	Get(ctx context.Context, key string) (string, bool, error)
	Delete(ctx context.Context, key string) error
}

type recurringStateAccessor interface {
	recurringStateStore() recurringStateStore
}

type memoryRecurringStateStore struct {
	versions sync.Map
}

func newMemoryRecurringStateStore() *memoryRecurringStateStore {
	return &memoryRecurringStateStore{}
}

func (s *memoryRecurringStateStore) Upsert(_ context.Context, key, version string) (bool, error) {
	_, existed := s.versions.Swap(key, version)
	return existed, nil
}

func (s *memoryRecurringStateStore) Get(_ context.Context, key string) (string, bool, error) {
	value, ok := s.versions.Load(key)
	if !ok {
		return "", false, nil
	}
	version, _ := value.(string)
	return version, true, nil
}

func (s *memoryRecurringStateStore) Delete(_ context.Context, key string) error {
	s.versions.Delete(key)
	return nil
}

type redisRecurringStateStore struct {
	client redis.UniversalClient
	prefix string
}

func newRedisRecurringStateStore(client redis.UniversalClient, prefix string) *redisRecurringStateStore {
	return &redisRecurringStateStore{
		client: client,
		prefix: normalizeRedisKeyPrefix(prefix),
	}
}

func (s *redisRecurringStateStore) Upsert(ctx context.Context, key, version string) (bool, error) {
	prev, err := s.client.SetArgs(ctx, s.redisKey(key), version, redis.SetArgs{Get: true}).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, oops.Errorf("upsert recurring state: %w", err)
	}
	return prev != "", nil
}

func (s *redisRecurringStateStore) Get(ctx context.Context, key string) (string, bool, error) {
	version, err := s.client.Get(ctx, s.redisKey(key)).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, oops.Errorf("get recurring state: %w", err)
	}
	return version, true, nil
}

func (s *redisRecurringStateStore) Delete(ctx context.Context, key string) error {
	if err := s.client.Del(ctx, s.redisKey(key)).Err(); err != nil {
		return oops.Errorf("delete recurring state: %w", err)
	}
	return nil
}

func (s *redisRecurringStateStore) redisKey(key string) string {
	if strings.Contains(s.prefix, "/") {
		return strings.TrimSuffix(s.prefix, "/") + "/" + key
	}
	return s.prefix + recurringStateRedisKeyPrefix + key
}

func normalizeRedisKeyPrefix(prefix string) string {
	if prefix == "" {
		return ""
	}
	if strings.Contains(prefix, "/") {
		return strings.TrimSuffix(prefix, "/")
	}
	return strings.TrimSuffix(prefix, ":") + ":"
}

func recurringStateStoreFromInspector(inspector Inspector) (recurringStateStore, error) {
	accessor, ok := inspector.(recurringStateAccessor)
	if !ok {
		return nil, oops.Errorf("inspector does not expose recurring state store")
	}
	store := accessor.recurringStateStore()
	if store == nil {
		return nil, oops.Errorf("recurring state store is nil")
	}
	return store, nil
}
