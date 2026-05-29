package internal

import (
	"context"
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	"github.com/otpki/otpki-pkg/spfy/anchor"
	"github.com/samber/oops"
)

type StoredEntry struct {
	Key       string          `json:"key"`
	Value     []byte          `json:"value"`
	ETag      anchor.ETag     `json:"etag"`
	Metadata  anchor.Metadata `json:"metadata,omitempty"`
	ExpiresAt *time.Time      `json:"expires_at,omitempty"`
}

type StoredOperation struct {
	Type     anchor.OperationType `json:"type"`
	Key      string               `json:"key"`
	Value    []byte               `json:"value,omitempty"`
	Expected anchor.ETag          `json:"expected,omitempty"`
	Options  anchor.WriteOptions  `json:"options,omitempty"`
	Metadata anchor.Metadata      `json:"metadata,omitempty"`
}

type StoredEvent struct {
	Type  anchor.EventType `json:"type"`
	Key   string           `json:"key"`
	Entry *StoredEntry     `json:"entry,omitempty"`
	At    time.Time        `json:"at"`
}

type StoredQueryResult struct {
	Entries    []StoredEntry
	NextCursor string
}

type Backend interface {
	Get(ctx context.Context, key string) (StoredEntry, error)
	Put(ctx context.Context, key string, value []byte, opts anchor.WriteOptions) (StoredEntry, error)
	Create(ctx context.Context, key string, value []byte, opts anchor.WriteOptions) (StoredEntry, error)
	CompareAndSet(ctx context.Context, key string, expected anchor.ETag, value []byte, opts anchor.WriteOptions) (StoredEntry, error)
	AppendMetadata(ctx context.Context, key string, metadata anchor.Metadata) (StoredEntry, error)
	Delete(ctx context.Context, key string) error
	Consume(ctx context.Context, key string) (StoredEntry, error)
	Query(ctx context.Context, query anchor.Query) (StoredQueryResult, error)
	Watch(ctx context.Context, opts anchor.WatchOptions) (<-chan StoredEvent, error)
	Batch(ctx context.Context, ops []StoredOperation) ([]StoredEntry, error)
}

type StoreAdapter[T any] struct {
	inner Backend
	codec anchor.Codec[T]
}

func NewStoreAdapter[T any](inner Backend, codec anchor.Codec[T]) *StoreAdapter[T] {
	if codec == nil {
		codec = anchor.JSONCodec[T]{}
	}
	return &StoreAdapter[T]{inner: inner, codec: codec}
}

func (s *StoreAdapter[T]) Get(ctx context.Context, key string) (anchor.Entry[T], error) {
	inner, err := s.inner.Get(ctx, key)
	if err != nil {
		var zero anchor.Entry[T]
		return zero, err
	}
	return s.decode(inner)
}

func (s *StoreAdapter[T]) Put(ctx context.Context, key string, value T, opts anchor.WriteOptions) (anchor.Entry[T], error) {
	data, err := s.codec.Marshal(value)
	if err != nil {
		var zero anchor.Entry[T]
		return zero, err
	}
	inner, err := s.inner.Put(ctx, key, data, opts)
	if err != nil {
		var zero anchor.Entry[T]
		return zero, err
	}
	return s.decode(inner)
}

func (s *StoreAdapter[T]) Create(ctx context.Context, key string, value T, opts anchor.WriteOptions) (anchor.Entry[T], error) {
	data, err := s.codec.Marshal(value)
	if err != nil {
		var zero anchor.Entry[T]
		return zero, err
	}
	inner, err := s.inner.Create(ctx, key, data, opts)
	if err != nil {
		var zero anchor.Entry[T]
		return zero, err
	}
	return s.decode(inner)
}

func (s *StoreAdapter[T]) CompareAndSet(ctx context.Context, key string, expected anchor.ETag, value T, opts anchor.WriteOptions) (anchor.Entry[T], error) {
	data, err := s.codec.Marshal(value)
	if err != nil {
		var zero anchor.Entry[T]
		return zero, err
	}
	inner, err := s.inner.CompareAndSet(ctx, key, expected, data, opts)
	if err != nil {
		var zero anchor.Entry[T]
		return zero, err
	}
	return s.decode(inner)
}

func (s *StoreAdapter[T]) AppendMetadata(ctx context.Context, key string, metadata anchor.Metadata) (anchor.Entry[T], error) {
	inner, err := s.inner.AppendMetadata(ctx, key, metadata)
	if err != nil {
		var zero anchor.Entry[T]
		return zero, err
	}
	return s.decode(inner)
}

func (s *StoreAdapter[T]) Delete(ctx context.Context, key string) error {
	return s.inner.Delete(ctx, key)
}

func (s *StoreAdapter[T]) Consume(ctx context.Context, key string) (anchor.Entry[T], error) {
	inner, err := s.inner.Consume(ctx, key)
	if err != nil {
		var zero anchor.Entry[T]
		return zero, err
	}
	return s.decode(inner)
}

func (s *StoreAdapter[T]) Query(ctx context.Context, query anchor.Query) (anchor.QueryResult[T], error) {
	res, err := s.inner.Query(ctx, query)
	if err != nil {
		return anchor.QueryResult[T]{}, err
	}
	entries := make([]anchor.Entry[T], 0, len(res.Entries))
	for _, inner := range res.Entries {
		entry, err := s.decode(inner)
		if err != nil {
			return anchor.QueryResult[T]{}, err
		}
		entries = append(entries, entry)
	}
	return anchor.QueryResult[T]{Entries: entries, NextCursor: res.NextCursor}, nil
}

func (s *StoreAdapter[T]) Watch(ctx context.Context, opts anchor.WatchOptions) (<-chan anchor.Event[T], error) {
	storedCh, err := s.inner.Watch(ctx, opts)
	if err != nil {
		return nil, err
	}
	out := make(chan anchor.Event[T], 64)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-storedCh:
				if !ok {
					return
				}
				decoded, ok := s.decodeEvent(ev)
				if !ok {
					continue
				}
				select {
				case out <- decoded:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func (s *StoreAdapter[T]) Batch(ctx context.Context, ops []anchor.Operation[T]) ([]anchor.Entry[T], error) {
	storedOps := make([]StoredOperation, 0, len(ops))
	for _, op := range ops {
		var value []byte
		if op.Type == anchor.OpPut || op.Type == anchor.OpCreate || op.Type == anchor.OpCompareAndSet {
			encoded, err := s.codec.Marshal(op.Value)
			if err != nil {
				return nil, err
			}
			value = encoded
		}
		storedOps = append(storedOps, StoredOperation{Type: op.Type, Key: op.Key, Value: value, Expected: op.Expected, Options: op.Options, Metadata: op.Metadata})
	}
	storedEntries, err := s.inner.Batch(ctx, storedOps)
	if err != nil {
		return nil, err
	}
	entries := make([]anchor.Entry[T], 0, len(storedEntries))
	for _, inner := range storedEntries {
		entry, err := s.decode(inner)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (s *StoreAdapter[T]) decode(inner StoredEntry) (anchor.Entry[T], error) {
	value, err := s.codec.Unmarshal(inner.Value)
	if err != nil {
		var zero anchor.Entry[T]
		return zero, err
	}
	return anchor.Entry[T]{Key: inner.Key, Value: value, ETag: inner.ETag, Metadata: CloneMetadata(inner.Metadata), ExpiresAt: CloneTime(inner.ExpiresAt)}, nil
}

func (s *StoreAdapter[T]) decodeEvent(ev StoredEvent) (anchor.Event[T], bool) {
	var entry *anchor.Entry[T]
	if ev.Entry != nil {
		decoded, err := s.decode(*ev.Entry)
		if err != nil {
			return anchor.Event[T]{}, false
		}
		entry = &decoded
	}
	return anchor.Event[T]{Type: ev.Type, Key: ev.Key, Entry: entry, At: ev.At}, true
}

func ExpiresAt(now time.Time, ttl time.Duration) *time.Time {
	if ttl <= 0 {
		return nil
	}
	t := now.Add(ttl).UTC()
	return &t
}

func StoredExpired(entry StoredEntry, now time.Time) bool {
	return entry.ExpiresAt != nil && !entry.ExpiresAt.After(now)
}

func CloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := *t
	return &v
}

func CloneStoredEntry(entry StoredEntry) StoredEntry {
	entry.Value = append([]byte(nil), entry.Value...)
	entry.Metadata = CloneMetadata(entry.Metadata)
	entry.ExpiresAt = CloneTime(entry.ExpiresAt)
	return entry
}

func StoredEventFor(kind anchor.EventType, key string, entry *StoredEntry, at time.Time) StoredEvent {
	var copy *StoredEntry
	if entry != nil {
		v := CloneStoredEntry(*entry)
		copy = &v
	}
	return StoredEvent{Type: kind, Key: key, Entry: copy, At: at}
}

func WatchMatches(opts anchor.WatchOptions, key string) bool {
	if opts.Key != "" && key != opts.Key {
		return false
	}
	if opts.Prefix != "" && !strings.HasPrefix(key, opts.Prefix) {
		return false
	}
	return true
}

func ValidateStoredOps(ops []StoredOperation) error {
	seen := map[string]struct{}{}
	for _, op := range ops {
		if op.Key == "" {
			return oops.New("key is required")
		}
		if _, ok := seen[op.Key]; ok {
			return anchor.ErrInvalidBatch
		}
		seen[op.Key] = struct{}{}
		switch op.Type {
		case anchor.OpPut, anchor.OpCreate, anchor.OpCompareAndSet, anchor.OpDelete, anchor.OpMetadataAppend, anchor.OpConsume:
		default:
			return oops.New("unknown operation type")
		}
	}
	return nil
}

func EncodeOffsetCursor(n int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(n)))
}

func DecodeOffsetCursor(cursor string) int {
	if cursor == "" {
		return 0
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(string(decoded))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func TtlFromExpiresAt(expiresAt *time.Time) time.Duration {
	if expiresAt == nil {
		return 0
	}
	d := time.Until(*expiresAt)
	if d <= 0 {
		return time.Millisecond
	}
	return d
}

func FilterStoredWatch(ctx context.Context, opts anchor.WatchOptions, in <-chan StoredEvent, buffer int) <-chan StoredEvent {
	out := make(chan StoredEvent, NormalizeWatchBuffer(buffer))
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-in:
				if !ok {
					return
				}
				if !WatchMatches(opts, ev.Key) {
					continue
				}
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out
}
