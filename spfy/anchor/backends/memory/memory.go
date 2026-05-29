package memory

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/otpki/otpki-pkg/spfy/anchor"
	"github.com/otpki/otpki-pkg/spfy/anchor/internal"
)

type Backend struct {
	mu          sync.RWMutex
	entries     map[string]internal.StoredEntry
	watchers    map[uint64]chan internal.StoredEvent
	nextID      uint64
	closed      bool
	watchBuffer int
}

func NewBackend(cfg Config) *Backend {
	if cfg == (Config{}) {
		cfg = DefaultConfig()
	}
	return &Backend{
		entries:     map[string]internal.StoredEntry{},
		watchers:    map[uint64]chan internal.StoredEvent{},
		watchBuffer: internal.NormalizeWatchBuffer(cfg.WatchBuffer),
	}
}

func New[T any](cfg Config, codec anchor.Codec[T]) anchor.Store[T] {
	return internal.NewStoreAdapter[T](NewBackend(cfg), codec)
}

func (s *Backend) Get(ctx context.Context, key string) (internal.StoredEntry, error) {
	if err := ctx.Err(); err != nil {
		return internal.StoredEntry{}, err
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[key]
	if !ok {
		return internal.StoredEntry{}, anchor.ErrNotFound
	}
	if internal.StoredExpired(entry, now) {
		delete(s.entries, key)
		s.broadcastLocked([]internal.StoredEvent{internal.StoredEventFor(anchor.EventExpire, key, nil, now)})
		return internal.StoredEntry{}, anchor.ErrNotFound
	}
	return internal.CloneStoredEntry(entry), nil
}

func (s *Backend) Put(
	ctx context.Context,
	key string,
	value []byte,
	opts anchor.WriteOptions,
) (internal.StoredEntry, error) {
	res, err := s.Batch(ctx, []internal.StoredOperation{{Type: anchor.OpPut, Key: key, Value: value, Options: opts}})
	if err != nil {
		return internal.StoredEntry{}, err
	}
	return res[0], nil
}

func (s *Backend) Create(
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

func (s *Backend) CompareAndSet(
	ctx context.Context,
	key string,
	expected anchor.ETag,
	value []byte,
	opts anchor.WriteOptions,
) (internal.StoredEntry, error) {
	res, err := s.Batch(
		ctx,
		[]internal.StoredOperation{
			{Type: anchor.OpCompareAndSet, Key: key, Expected: expected, Value: value, Options: opts},
		},
	)
	if err != nil {
		return internal.StoredEntry{}, err
	}
	return res[0], nil
}

func (s *Backend) AppendMetadata(
	ctx context.Context,
	key string,
	metadata anchor.Metadata,
) (internal.StoredEntry, error) {
	res, err := s.Batch(ctx, []internal.StoredOperation{{Type: anchor.OpMetadataAppend, Key: key, Metadata: metadata}})
	if err != nil {
		return internal.StoredEntry{}, err
	}
	return res[0], nil
}

func (s *Backend) Delete(ctx context.Context, key string) error {
	_, err := s.Batch(ctx, []internal.StoredOperation{{Type: anchor.OpDelete, Key: key}})
	return err
}

func (s *Backend) Consume(ctx context.Context, key string) (internal.StoredEntry, error) {
	res, err := s.Batch(ctx, []internal.StoredOperation{{Type: anchor.OpConsume, Key: key}})
	if err != nil {
		return internal.StoredEntry{}, err
	}
	return res[0], nil
}

func (s *Backend) Batch(ctx context.Context, ops []internal.StoredOperation) ([]internal.StoredEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := internal.ValidateStoredOps(ops); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, anchor.ErrClosed
	}
	// Validate against a snapshot while treating expired entries as absent.
	for _, op := range ops {
		entry, exists := s.entries[op.Key]
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
		if entry, ok := s.entries[op.Key]; ok && internal.StoredExpired(entry, now) {
			delete(s.entries, op.Key)
			events = append(events, internal.StoredEventFor(anchor.EventExpire, op.Key, nil, now))
		}
		current, exists := s.entries[op.Key]
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
			s.entries[op.Key] = entry
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
			s.entries[op.Key] = entry
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
			s.entries[op.Key] = entry
			copy := internal.CloneStoredEntry(entry)
			results = append(results, copy)
			events = append(events, internal.StoredEventFor(anchor.EventCompareAndSet, op.Key, &copy, now))
		case anchor.OpMetadataAppend:
			current.Metadata = internal.MergeMetadata(current.Metadata, op.Metadata)
			current.ETag = anchor.NewETag()
			s.entries[op.Key] = current
			copy := internal.CloneStoredEntry(current)
			results = append(results, copy)
			events = append(events, internal.StoredEventFor(anchor.EventMetadataAppend, op.Key, &copy, now))
		case anchor.OpDelete:
			delete(s.entries, op.Key)
			events = append(events, internal.StoredEventFor(anchor.EventDelete, op.Key, nil, now))
		case anchor.OpConsume:
			delete(s.entries, op.Key)
			copy := internal.CloneStoredEntry(current)
			results = append(results, copy)
			events = append(events, internal.StoredEventFor(anchor.EventConsume, op.Key, &copy, now))
		}
	}
	s.broadcastLocked(events)
	return results, nil
}

func (s *Backend) Query(ctx context.Context, query anchor.Query) (internal.StoredQueryResult, error) {
	if err := ctx.Err(); err != nil {
		return internal.StoredQueryResult{}, err
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.entries))
	var expired []internal.StoredEvent
	for key, entry := range s.entries {
		if internal.StoredExpired(entry, now) {
			delete(s.entries, key)
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
		entries = append(entries, internal.CloneStoredEntry(s.entries[key]))
	}
	var next string
	if start+limit < len(keys) {
		next = internal.EncodeOffsetCursor(start + limit)
	}
	return internal.StoredQueryResult{Entries: entries, NextCursor: next}, nil
}

func (s *Backend) Watch(ctx context.Context, opts anchor.WatchOptions) (<-chan internal.StoredEvent, error) {
	out := make(chan internal.StoredEvent, s.watchBuffer)
	now := time.Now().UTC()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		close(out)
		return nil, anchor.ErrClosed
	}
	if opts.IncludeCurrent {
		keys := make([]string, 0, len(s.entries))
		for key, entry := range s.entries {
			if internal.StoredExpired(entry, now) || !internal.WatchMatches(opts, key) {
				continue
			}
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			out <- internal.StoredEventFor(anchor.EventPut, key, new(internal.CloneStoredEntry(s.entries[key])), now)
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

func (s *Backend) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	for id, ch := range s.watchers {
		delete(s.watchers, id)
		close(ch)
	}
	return nil
}

func (s *Backend) broadcastLocked(events []internal.StoredEvent) {
	if len(events) == 0 {
		return
	}
	for _, ev := range events {
		for _, ch := range s.watchers {
			select {
			case ch <- ev:
			default:
			}
		}
	}
}

func (s *Backend) ReplaceAll(ctx context.Context, entries []internal.StoredEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = map[string]internal.StoredEntry{}
	now := time.Now().UTC()
	var events []internal.StoredEvent
	for _, entry := range entries {
		if internal.StoredExpired(entry, now) {
			continue
		}
		copy := internal.CloneStoredEntry(entry)
		s.entries[copy.Key] = copy
		events = append(events, internal.StoredEventFor(anchor.EventPut, copy.Key, &copy, now))
	}
	s.broadcastLocked(events)
	return nil
}

func (s *Backend) AllEntries(ctx context.Context) ([]internal.StoredEntry, error) {
	res, err := s.Query(ctx, anchor.Query{Limit: 0})
	if err != nil {
		return nil, err
	}
	return res.Entries, nil
}

func (s *Backend) ApplyReplicatedEvent(ctx context.Context, ev internal.StoredEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch ev.Type {
	case anchor.EventPut, anchor.EventCreate, anchor.EventCompareAndSet, anchor.EventMetadataAppend:
		if ev.Entry != nil {
			s.entries[ev.Key] = internal.CloneStoredEntry(*ev.Entry)
		}
	case anchor.EventDelete, anchor.EventConsume, anchor.EventExpire:
		delete(s.entries, ev.Key)
	}
	s.broadcastLocked([]internal.StoredEvent{ev})
	return nil
}
