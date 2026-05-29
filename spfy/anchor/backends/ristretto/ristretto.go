package ristretto

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dgraph-io/ristretto/v2"
	"github.com/otpki/otpki-pkg/spfy/anchor"
	"github.com/otpki/otpki-pkg/spfy/anchor/internal"
)

type backend struct {
	mu          sync.Mutex
	cache       *ristretto.Cache[string, []byte]
	index       map[string]internal.StoredEntry
	watchers    map[uint64]chan internal.StoredEvent
	nextID      uint64
	watchBuffer int
}

func newBackend(cache *ristretto.Cache[string, []byte], cfg Config) *backend {
	if cfg == (Config{}) {
		cfg = DefaultConfig()
	}
	return &backend{
		cache:       cache,
		index:       map[string]internal.StoredEntry{},
		watchers:    map[uint64]chan internal.StoredEvent{},
		watchBuffer: internal.NormalizeWatchBuffer(cfg.WatchBuffer),
	}
}

func New[T any](cfg Config, codec anchor.Codec[T]) (anchor.Store[T], error) {
	if cfg == (Config{}) {
		cfg = DefaultConfig()
	}
	cache, err := ristretto.NewCache(&ristretto.Config[string, []byte]{
		NumCounters: cfg.NumCounters,
		MaxCost:     cfg.MaxCost,
		BufferItems: cfg.BufferItems,
	})
	if err != nil {
		return nil, err
	}
	return internal.NewStoreAdapter[T](newBackend(cache, cfg), codec), nil
}

func (s *backend) Get(ctx context.Context, key string) (internal.StoredEntry, error) {
	if err := ctx.Err(); err != nil {
		return internal.StoredEntry{}, err
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.loadLocked(key)
	if !ok {
		delete(s.index, key)
		return internal.StoredEntry{}, anchor.ErrNotFound
	}
	if internal.StoredExpired(entry, now) {
		s.deleteLocked(key)
		s.broadcastLocked([]internal.StoredEvent{internal.StoredEventFor(anchor.EventExpire, key, nil, now)})
		return internal.StoredEntry{}, anchor.ErrNotFound
	}
	return internal.CloneStoredEntry(entry), nil
}

func (s *backend) Put(ctx context.Context, key string, value []byte, opts anchor.WriteOptions) (internal.StoredEntry, error) {
	res, err := s.Batch(ctx, []internal.StoredOperation{{Type: anchor.OpPut, Key: key, Value: value, Options: opts}})
	if err != nil {
		return internal.StoredEntry{}, err
	}
	return res[0], nil
}

func (s *backend) Create(
	ctx context.Context,
	key string,
	value []byte,
	opts anchor.WriteOptions,
) (internal.StoredEntry, error) {
	res, err := s.Batch(ctx, []internal.StoredOperation{{Type: anchor.OpCreate, Key: key, Value: value, Options: opts}})
	if err != nil {
		return internal.StoredEntry{}, err
	}
	return res[0], nil
}

func (s *backend) CompareAndSet(
	ctx context.Context,
	key string,
	expected anchor.ETag,
	value []byte,
	opts anchor.WriteOptions,
) (internal.StoredEntry, error) {
	res, err := s.Batch(
		ctx,
		[]internal.StoredOperation{{Type: anchor.OpCompareAndSet, Key: key, Expected: expected, Value: value, Options: opts}},
	)
	if err != nil {
		return internal.StoredEntry{}, err
	}
	return res[0], nil
}
func (s *backend) AppendMetadata(ctx context.Context, key string, metadata anchor.Metadata) (internal.StoredEntry, error) {
	res, err := s.Batch(ctx, []internal.StoredOperation{{Type: anchor.OpMetadataAppend, Key: key, Metadata: metadata}})
	if err != nil {
		return internal.StoredEntry{}, err
	}
	return res[0], nil
}
func (s *backend) Delete(ctx context.Context, key string) error {
	_, err := s.Batch(ctx, []internal.StoredOperation{{Type: anchor.OpDelete, Key: key}})
	return err
}
func (s *backend) Consume(ctx context.Context, key string) (internal.StoredEntry, error) {
	res, err := s.Batch(ctx, []internal.StoredOperation{{Type: anchor.OpConsume, Key: key}})
	if err != nil {
		return internal.StoredEntry{}, err
	}
	return res[0], nil
}

func (s *backend) Batch(ctx context.Context, ops []internal.StoredOperation) ([]internal.StoredEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := internal.ValidateStoredOps(ops); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, op := range ops {
		entry, exists := s.loadLocked(op.Key)
		if exists && internal.StoredExpired(entry, now) {
			exists = false
		}
		switch op.Type {
		case anchor.OpCreate:
			if exists {
				return nil, anchor.ErrAlreadyExists
			}
		case anchor.OpCompareAndSet:
			if !exists {
				return nil, anchor.ErrNotFound
			}
			if entry.ETag != op.Expected {
				return nil, anchor.ErrETagMismatch
			}
		case anchor.OpMetadataAppend, anchor.OpDelete, anchor.OpConsume:
			if !exists {
				return nil, anchor.ErrNotFound
			}
		}
	}
	var results []internal.StoredEntry
	var events []internal.StoredEvent
	for _, op := range ops {
		if entry, ok := s.loadLocked(op.Key); ok && internal.StoredExpired(entry, now) {
			s.deleteLocked(op.Key)
			events = append(events, internal.StoredEventFor(anchor.EventExpire, op.Key, nil, now))
		}
		current, exists := s.loadLocked(op.Key)
		switch op.Type {
		case anchor.OpPut:
			entry := internal.StoredEntry{
				Key:       op.Key,
				Value:     append([]byte(nil), op.Value...),
				ETag:      anchor.NewETag(),
				Metadata:  internal.CloneMetadata(op.Options.Metadata),
				ExpiresAt: internal.ExpiresAt(now, op.Options.TTL),
			}
			if exists {
				entry.Metadata = internal.MergeMetadata(current.Metadata, op.Options.Metadata)
			}
			s.setLocked(entry, op.Options.TTL)
			copy := internal.CloneStoredEntry(entry)
			results = append(results, copy)
			events = append(events, internal.StoredEventFor(anchor.EventPut, op.Key, &copy, now))
		case anchor.OpCreate:
			entry := internal.StoredEntry{
				Key:       op.Key,
				Value:     append([]byte(nil), op.Value...),
				ETag:      anchor.NewETag(),
				Metadata:  internal.CloneMetadata(op.Options.Metadata),
				ExpiresAt: internal.ExpiresAt(now, op.Options.TTL),
			}
			s.setLocked(entry, op.Options.TTL)
			copy := internal.CloneStoredEntry(entry)
			results = append(results, copy)
			events = append(events, internal.StoredEventFor(anchor.EventCreate, op.Key, &copy, now))
		case anchor.OpCompareAndSet:
			entry := internal.StoredEntry{
				Key:       op.Key,
				Value:     append([]byte(nil), op.Value...),
				ETag:      anchor.NewETag(),
				Metadata:  internal.MergeMetadata(current.Metadata, op.Options.Metadata),
				ExpiresAt: internal.ExpiresAt(now, op.Options.TTL),
			}
			s.setLocked(entry, op.Options.TTL)
			copy := internal.CloneStoredEntry(entry)
			results = append(results, copy)
			events = append(events, internal.StoredEventFor(anchor.EventCompareAndSet, op.Key, &copy, now))
		case anchor.OpMetadataAppend:
			current.Metadata = internal.MergeMetadata(current.Metadata, op.Metadata)
			current.ETag = anchor.NewETag()
			s.setLocked(current, internal.TtlFromExpiresAt(current.ExpiresAt))
			copy := internal.CloneStoredEntry(current)
			results = append(results, copy)
			events = append(events, internal.StoredEventFor(anchor.EventMetadataAppend, op.Key, &copy, now))
		case anchor.OpDelete:
			s.deleteLocked(op.Key)
			events = append(events, internal.StoredEventFor(anchor.EventDelete, op.Key, nil, now))
		case anchor.OpConsume:
			s.deleteLocked(op.Key)
			copy := internal.CloneStoredEntry(current)
			results = append(results, copy)
			events = append(events, internal.StoredEventFor(anchor.EventConsume, op.Key, &copy, now))
		}
	}
	s.cache.Wait()
	s.broadcastLocked(events)
	return results, nil
}

func (s *backend) Query(ctx context.Context, query anchor.Query) (internal.StoredQueryResult, error) {
	if err := ctx.Err(); err != nil {
		return internal.StoredQueryResult{}, err
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.index))
	var expired []internal.StoredEvent
	for key := range s.index {
		entry, ok := s.loadLocked(key)
		if !ok {
			delete(s.index, key)
			continue
		}
		if internal.StoredExpired(entry, now) {
			s.deleteLocked(key)
			expired = append(expired, internal.StoredEventFor(anchor.EventExpire, key, nil, now))
			continue
		}
		if query.Prefix != "" && !strings.HasPrefix(key, query.Prefix) {
			continue
		}
		if !internal.MetadataContains(entry.Metadata, query.Metadata) {
			continue
		}
		keys = append(keys, key)
	}
	s.broadcastLocked(expired)
	sort.Strings(keys)
	start := internal.DecodeOffsetCursor(query.Cursor)
	if start > len(keys) {
		start = len(keys)
	}
	limit := query.Limit
	if limit <= 0 || limit > len(keys)-start {
		limit = len(keys) - start
	}
	entries := make([]internal.StoredEntry, 0, limit)
	for _, key := range keys[start : start+limit] {
		if e, ok := s.loadLocked(key); ok {
			entries = append(entries, internal.CloneStoredEntry(e))
		}
	}
	var next string
	if start+limit < len(keys) {
		next = internal.EncodeOffsetCursor(start + limit)
	}
	return internal.StoredQueryResult{Entries: entries, NextCursor: next}, nil
}

func (s *backend) Watch(ctx context.Context, opts anchor.WatchOptions) (<-chan internal.StoredEvent, error) {
	out := make(chan internal.StoredEvent, s.watchBuffer)
	now := time.Now().UTC()
	s.mu.Lock()
	if opts.IncludeCurrent {
		keys := make([]string, 0, len(s.index))
		for key := range s.index {
			if e, ok := s.loadLocked(key); ok && !internal.StoredExpired(e, now) && internal.WatchMatches(opts, key) {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			e, _ := s.loadLocked(key)
			out <- internal.StoredEventFor(anchor.EventPut, key, new(internal.CloneStoredEntry(e)), now)
		}
	}
	id := s.nextID
	s.nextID++
	s.watchers[id] = out
	s.mu.Unlock()
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		if ch, ok := s.watchers[id]; ok {
			delete(s.watchers, id)
			close(ch)
		}
		s.mu.Unlock()
	}()
	return internal.FilterStoredWatch(ctx, opts, out, s.watchBuffer), nil
}

func (s *backend) setLocked(entry internal.StoredEntry, ttl time.Duration) {
	entry = internal.CloneStoredEntry(entry)
	raw, _ := json.Marshal(entry)
	s.index[entry.Key] = entry
	cost := int64(len(raw) + 1)
	if ttl > 0 {
		s.cache.SetWithTTL(entry.Key, raw, cost, ttl)
	} else {
		s.cache.Set(entry.Key, raw, cost)
	}
}

func (s *backend) loadLocked(key string) (internal.StoredEntry, bool) {
	raw, ok := s.cache.Get(key)
	if !ok {
		return internal.StoredEntry{}, false
	}
	var entry internal.StoredEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return internal.StoredEntry{}, false
	}
	return entry, true
}

func (s *backend) deleteLocked(key string) {
	delete(s.index, key)
	s.cache.Del(key)
}

func (s *backend) broadcastLocked(events []internal.StoredEvent) {
	for _, ev := range events {
		for _, ch := range s.watchers {
			select {
			case ch <- ev:
			default:
			}
		}
	}
}
