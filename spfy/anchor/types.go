package anchor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/samber/oops"
)

var (
	ErrNotFound      = oops.New("key not found")
	ErrAlreadyExists = oops.New("key already exists")
	ErrETagMismatch  = oops.New("etag mismatch")
	ErrConflict      = oops.New("transaction conflict")
	ErrClosed        = oops.New("store is closed")
	ErrInvalidBatch  = oops.New("invalid batch")
)

type ETag string

func NewETag() ETag {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return ETag(hex.EncodeToString(b[:]))
	}
	return ETag(fmt.Sprintf("%d", time.Now().UnixNano()))
}

type Metadata map[string][]string

type Entry[T any] struct {
	Key       string
	Value     T
	ETag      ETag
	Metadata  Metadata
	ExpiresAt *time.Time
}

type WriteOptions struct {
	// TTL is the desired lifetime for this write. Zero means no expiration for
	// Put/Create/CompareAndSet. AppendMetadata preserves the existing TTL.
	TTL      time.Duration
	Metadata Metadata
}

type EventType string

const (
	EventPut            EventType = "put"
	EventCreate         EventType = "create"
	EventCompareAndSet  EventType = "compare_and_set"
	EventDelete         EventType = "delete"
	EventMetadataAppend EventType = "metadata_append"
	EventExpire         EventType = "expire"
	EventConsume        EventType = "consume"
)

type Event[T any] struct {
	Type  EventType
	Key   string
	Entry *Entry[T]
	At    time.Time
}

type WatchOptions struct {
	Key            string
	Prefix         string
	IncludeCurrent bool
}

type Query struct {
	Prefix   string
	Metadata Metadata
	Limit    int
	Cursor   string
}

type QueryResult[T any] struct {
	Entries    []Entry[T]
	NextCursor string
}

type OperationType string

const (
	OpPut            OperationType = "put"
	OpCreate         OperationType = "create"
	OpCompareAndSet  OperationType = "compare_and_set"
	OpDelete         OperationType = "delete"
	OpMetadataAppend OperationType = "metadata_append"
	// OpConsume atomically gets and deletes an entry. Its result is the consumed
	// entry as it existed before deletion.
	OpConsume OperationType = "consume"
)

type Operation[T any] struct {
	Type     OperationType
	Key      string
	Value    T
	Expected ETag
	Options  WriteOptions
	Metadata Metadata
}

type Store[T any] interface {
	Get(ctx context.Context, key string) (Entry[T], error)
	Put(ctx context.Context, key string, value T, opts WriteOptions) (Entry[T], error)
	// Create atomically creates a key if it does not exist.
	Create(ctx context.Context, key string, value T, opts WriteOptions) (Entry[T], error)
	// CompareAndSet atomically sets a key if its ETag matches the expected ETag.
	CompareAndSet(ctx context.Context, key string, expected ETag, value T, opts WriteOptions) (Entry[T], error)
	AppendMetadata(ctx context.Context, key string, metadata Metadata) (Entry[T], error)
	Delete(ctx context.Context, key string) error
	// Consume atomically gets and deletes a key, like Redis GETDEL. The returned
	// entry is the value before deletion.
	Consume(ctx context.Context, key string) (Entry[T], error)
	Query(ctx context.Context, query Query) (QueryResult[T], error)
	Watch(ctx context.Context, opts WatchOptions) (<-chan Event[T], error)

	// Batch applies all operations to one consistent backend transaction where
	// the backend supports it. Stores may reject duplicate mutations for the same
	// key within a single batch to keep ETag/result semantics unambiguous.
	Batch(ctx context.Context, ops []Operation[T]) ([]Entry[T], error)
}
