package redis

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/otpki/otpki-pkg/spfy/anchor"
	"github.com/otpki/otpki-pkg/spfy/anchor/internal"
	"github.com/redis/go-redis/v9"
)

type backend struct {
	client        redis.UniversalClient
	prefix        string
	index         string
	channel       string
	maxTxnRetries int
	watchBuffer   int
}

func newBackend(client redis.UniversalClient, cfg Config) *backend {
	if cfg == (Config{}) {
		cfg = DefaultConfig()
	}
	if cfg.Prefix == "" {
		cfg.Prefix = DefaultConfig().Prefix
	}
	if cfg.MaxTxnRetries <= 0 {
		cfg.MaxTxnRetries = DefaultConfig().MaxTxnRetries
	}
	return &backend{
		client:        client,
		prefix:        cfg.Prefix,
		index:         cfg.Prefix + ":idx",
		channel:       cfg.Prefix + ":events",
		maxTxnRetries: cfg.MaxTxnRetries,
		watchBuffer:   internal.NormalizeWatchBuffer(cfg.WatchBuffer),
	}
}

func New[T any](client redis.UniversalClient, cfg Config, codec anchor.Codec[T]) anchor.Store[T] {
	return internal.NewStoreAdapter[T](newBackend(client, cfg), codec)
}

func (s *backend) Get(ctx context.Context, key string) (internal.StoredEntry, error) {
	entry, err := s.load(ctx, key)
	if err != nil {
		return internal.StoredEntry{}, err
	}
	if internal.StoredExpired(entry, time.Now().UTC()) {
		_ = s.Delete(ctx, key)
		return internal.StoredEntry{}, anchor.ErrNotFound
	}
	return entry, nil
}
func (s *backend) Put(ctx context.Context, key string, value []byte, opts anchor.WriteOptions) (internal.StoredEntry, error) {
	res, err := s.Batch(ctx, []internal.StoredOperation{{Type: anchor.OpPut, Key: key, Value: value, Options: opts}})
	if err != nil {
		return internal.StoredEntry{}, err
	}
	return res[0], nil
}
func (s *backend) Create(ctx context.Context, key string, value []byte, opts anchor.WriteOptions) (internal.StoredEntry, error) {
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
	if err := internal.ValidateStoredOps(ops); err != nil {
		return nil, err
	}
	if len(ops) == 0 {
		return nil, nil
	}
	watchKeys := make([]string, 0, len(ops))
	for _, op := range ops {
		watchKeys = append(watchKeys, s.entryKey(op.Key))
	}
	var results []internal.StoredEntry
	for attempt := 0; attempt < s.maxTxnRetries; attempt++ {
		err := s.client.Watch(ctx, func(tx *redis.Tx) error {
			now := time.Now().UTC()
			current := map[string]internal.StoredEntry{}
			exists := map[string]bool{}
			for _, op := range ops {
				entry, err := s.loadTx(ctx, tx, op.Key)
				if err == nil && internal.StoredExpired(entry, now) {
					exists[op.Key] = false
				} else if err == nil {
					current[op.Key], exists[op.Key] = entry, true
				} else if errors.Is(err, anchor.ErrNotFound) {
					exists[op.Key] = false
				} else {
					return err
				}
			}
			for _, op := range ops {
				entry := current[op.Key]
				ok := exists[op.Key]
				switch op.Type {
				case anchor.OpCreate:
					if ok {
						return anchor.ErrAlreadyExists
					}
				case anchor.OpCompareAndSet:
					if !ok {
						return anchor.ErrNotFound
					}
					if entry.ETag != op.Expected {
						return anchor.ErrETagMismatch
					}
				case anchor.OpMetadataAppend, anchor.OpDelete, anchor.OpConsume:
					if !ok {
						return anchor.ErrNotFound
					}
				}
			}
			pipe := tx.TxPipeline()
			var out []internal.StoredEntry
			for _, op := range ops {
				cur := current[op.Key]
				switch op.Type {
				case anchor.OpPut, anchor.OpCreate, anchor.OpCompareAndSet:
					metadata := internal.CloneMetadata(op.Options.Metadata)
					if op.Type != anchor.OpCreate {
						metadata = internal.MergeMetadata(cur.Metadata, op.Options.Metadata)
					}
					entry := internal.StoredEntry{
						Key:       op.Key,
						Value:     append([]byte(nil), op.Value...),
						ETag:      anchor.NewETag(),
						Metadata:  metadata,
						ExpiresAt: internal.ExpiresAt(now, op.Options.TTL),
					}
					s.storePipe(ctx, pipe, entry, op.Options.TTL)
					out = append(out, internal.CloneStoredEntry(entry))
					typeMap := map[anchor.OperationType]anchor.EventType{
						anchor.OpPut:           anchor.EventPut,
						anchor.OpCreate:        anchor.EventCreate,
						anchor.OpCompareAndSet: anchor.EventCompareAndSet,
					}
					s.publishPipe(ctx, pipe, internal.StoredEventFor(typeMap[op.Type], op.Key, &entry, now))
				case anchor.OpMetadataAppend:
					cur.Metadata = internal.MergeMetadata(cur.Metadata, op.Metadata)
					cur.ETag = anchor.NewETag()
					ttl := internal.TtlFromExpiresAt(cur.ExpiresAt)
					s.storePipe(ctx, pipe, cur, ttl)
					copy := internal.CloneStoredEntry(cur)
					out = append(out, copy)
					s.publishPipe(ctx, pipe, internal.StoredEventFor(anchor.EventMetadataAppend, op.Key, &copy, now))
				case anchor.OpDelete:
					s.deletePipe(ctx, pipe, op.Key)
					s.publishPipe(ctx, pipe, internal.StoredEventFor(anchor.EventDelete, op.Key, nil, now))
				case anchor.OpConsume:
					s.deletePipe(ctx, pipe, op.Key)
					copy := internal.CloneStoredEntry(cur)
					out = append(out, copy)
					s.publishPipe(ctx, pipe, internal.StoredEventFor(anchor.EventConsume, op.Key, &copy, now))
				}
			}
			_, err := pipe.Exec(ctx)
			if err != nil {
				return err
			}
			results = out
			return nil
		}, watchKeys...)
		if err == redis.TxFailedErr {
			continue
		}
		if err != nil {
			return nil, err
		}
		return results, nil
	}
	return nil, anchor.ErrConflict
}

func (s *backend) Query(ctx context.Context, query anchor.Query) (internal.StoredQueryResult, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 1000
	}
	cursor := ""
	if query.Cursor != "" {
		if decoded, err := base64.RawURLEncoding.DecodeString(query.Cursor); err == nil {
			cursor = string(decoded)
		}
	}
	min, max := redisLexRange(query.Prefix, cursor)
	var entries []internal.StoredEntry
	var next string
	last := cursor
	for len(entries) < limit {
		keys, err := s.client.ZRangeByLex(ctx, s.index, &redis.ZRangeBy{Min: min, Max: max, Offset: 0, Count: int64(limit - len(entries) + 32)}).
			Result()
		if err != nil {
			return internal.StoredQueryResult{}, err
		}
		if len(keys) == 0 {
			break
		}
		for _, key := range keys {
			last = key
			entry, err := s.Get(ctx, key)
			if errors.Is(err, anchor.ErrNotFound) {
				_ = s.client.ZRem(ctx, s.index, key).Err()
				continue
			}
			if err != nil {
				return internal.StoredQueryResult{}, err
			}
			if !internal.MetadataContains(entry.Metadata, query.Metadata) {
				continue
			}
			if len(entries) >= limit {
				next = base64.RawURLEncoding.EncodeToString([]byte(entries[len(entries)-1].Key))
				break
			}
			entries = append(entries, entry)
		}
		if next != "" || len(keys) < limit-len(entries)+32 {
			break
		}
		min, max = redisLexRange(query.Prefix, last)
	}
	return internal.StoredQueryResult{Entries: entries, NextCursor: next}, nil
}

func (s *backend) Watch(ctx context.Context, opts anchor.WatchOptions) (<-chan internal.StoredEvent, error) {
	out := make(chan internal.StoredEvent, s.watchBuffer)
	if opts.IncludeCurrent {
		res, err := s.Query(ctx, anchor.Query{Prefix: opts.Prefix, Limit: 0})
		if err != nil {
			close(out)
			return nil, err
		}
		for _, e := range res.Entries {
			if internal.WatchMatches(opts, e.Key) {
				copy := internal.CloneStoredEntry(e)
				out <- internal.StoredEventFor(anchor.EventPut, e.Key, &copy, time.Now().UTC())
			}
		}
	}
	pubsub := s.client.Subscribe(ctx, s.channel)
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		close(out)
		return nil, err
	}
	go func() {
		defer close(out)
		defer pubsub.Close()
		ch := pubsub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var ev internal.StoredEvent
				if json.Unmarshal([]byte(msg.Payload), &ev) == nil && internal.WatchMatches(opts, ev.Key) {
					select {
					case out <- ev:
					default:
					}
				}
			}
		}
	}()
	return out, nil
}

func (s *backend) load(ctx context.Context, key string) (internal.StoredEntry, error) {
	raw, err := s.client.Get(ctx, s.entryKey(key)).Bytes()
	if err == redis.Nil {
		return internal.StoredEntry{}, anchor.ErrNotFound
	}
	if err != nil {
		return internal.StoredEntry{}, err
	}
	var entry internal.StoredEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return internal.StoredEntry{}, err
	}
	return entry, nil
}
func (s *backend) loadTx(ctx context.Context, tx *redis.Tx, key string) (internal.StoredEntry, error) {
	raw, err := tx.Get(ctx, s.entryKey(key)).Bytes()
	if err == redis.Nil {
		return internal.StoredEntry{}, anchor.ErrNotFound
	}
	if err != nil {
		return internal.StoredEntry{}, err
	}
	var entry internal.StoredEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return internal.StoredEntry{}, err
	}
	return entry, nil
}

func (s *backend) storePipe(ctx context.Context, pipe redis.Pipeliner, entry internal.StoredEntry, ttl time.Duration) {
	raw, _ := json.Marshal(entry)
	pipe.Set(ctx, s.entryKey(entry.Key), raw, ttl)
	pipe.ZAdd(ctx, s.index, redis.Z{Score: 0, Member: entry.Key})
}
func (s *backend) deletePipe(ctx context.Context, pipe redis.Pipeliner, key string) {
	pipe.Del(ctx, s.entryKey(key))
	pipe.ZRem(ctx, s.index, key)
}
func (s *backend) publishPipe(ctx context.Context, pipe redis.Pipeliner, ev internal.StoredEvent) {
	raw, _ := json.Marshal(ev)
	pipe.Publish(ctx, s.channel, raw)
}
func (s *backend) entryKey(key string) string {
	return s.prefix + ":entry:" + base64.RawURLEncoding.EncodeToString([]byte(key))
}

func redisLexRange(prefix, cursor string) (string, string) {
	min := "-"
	if cursor != "" {
		min = "(" + cursor
	} else if prefix != "" {
		min = "[" + prefix
	}
	max := "+"
	if prefix != "" {
		max = "[" + prefix + "\xff"
	}
	return min, max
}
