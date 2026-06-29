package anchor

import (
	"context"
	"errors"
	"time"

	"github.com/samber/oops"
)

const tieredAuthoritativeETagMetadataKey = "anchor.internal/tiered-authoritative-etag"

type tieredStoreOptions struct {
	// StrictCacheWrites controls whether cache propagation errors should cause
	// write methods to return an error after the source-of-truth store succeeds.
	//
	// When false, writes succeed if the final/source-of-truth store succeeds,
	// even if one or more faster tiers fail to update.
	//
	// When true, cache update failures are returned. The source-of-truth write is
	// not rolled back.
	StrictCacheWrites bool

	// WatchBuffer is the buffer size for the tiered watch channel.
	// Zero defaults to 64.
	WatchBuffer int
}

type TieredStore[T any] struct {
	stores []Store[T]
	opts   tieredStoreOptions
}

func NewTieredStore[T any](stores ...Store[T]) (*TieredStore[T], error) {
	if len(stores) == 0 {
		return nil, oops.Errorf("%w: tiered store requires at least one store", ErrInvalidBatch)
	}

	opts := tieredStoreOptions{}
	if opts.WatchBuffer <= 0 {
		opts.WatchBuffer = 64
	}

	copied := make([]Store[T], len(stores))
	copy(copied, stores)

	return &TieredStore[T]{
		stores: copied,
		opts:   opts,
	}, nil
}

func (s *TieredStore[T]) sourceOfTruthIndex() int {
	return len(s.stores) - 1
}

func (s *TieredStore[T]) sourceOfTruth() Store[T] {
	return s.stores[s.sourceOfTruthIndex()]
}

func (s *TieredStore[T]) Get(ctx context.Context, key string) (Entry[T], error) {
	var errs []error

	for i, store := range s.stores {
		entry, err := store.Get(ctx, key)
		if err == nil {
			publicEntry := publicEntryFromTier(entry)

			// Promote lower-tier hits into every faster tier above them.
			if i > 0 {
				_ = s.populateTiers(ctx, 0, i, publicEntry)
			}

			return publicEntry, nil
		}

		if errors.Is(err, ErrNotFound) {
			continue
		}

		// Fast tiers are allowed to fail. Keep walking toward the
		// source-of-truth tier.
		if i < s.sourceOfTruthIndex() {
			errs = append(errs, oops.Errorf("tier %d get %q: %w", i, key, err))
			continue
		}

		errs = append(errs, oops.Errorf("source-of-truth get %q: %w", key, err))
		return Entry[T]{}, errors.Join(errs...)
	}

	return Entry[T]{}, ErrNotFound
}

func (s *TieredStore[T]) Put(
	ctx context.Context,
	key string,
	value T,
	opts WriteOptions,
) (Entry[T], error) {
	entry, err := s.sourceOfTruth().Put(ctx, key, value, opts)
	if err != nil {
		return Entry[T]{}, err
	}

	publicEntry := publicEntryFromSourceOfTruth(entry)
	cacheErr := s.populateTiers(ctx, 0, s.sourceOfTruthIndex(), publicEntry)

	if s.opts.StrictCacheWrites && cacheErr != nil {
		return publicEntry, cacheErr
	}

	return publicEntry, nil
}

func (s *TieredStore[T]) Create(
	ctx context.Context,
	key string,
	value T,
	opts WriteOptions,
) (Entry[T], error) {
	entry, err := s.sourceOfTruth().Create(ctx, key, value, opts)
	if err != nil {
		return Entry[T]{}, err
	}

	publicEntry := publicEntryFromSourceOfTruth(entry)
	cacheErr := s.populateTiers(ctx, 0, s.sourceOfTruthIndex(), publicEntry)

	if s.opts.StrictCacheWrites && cacheErr != nil {
		return publicEntry, cacheErr
	}

	return publicEntry, nil
}

func (s *TieredStore[T]) CompareAndSet(
	ctx context.Context,
	key string,
	expected ETag,
	value T,
	opts WriteOptions,
) (Entry[T], error) {
	entry, err := s.sourceOfTruth().CompareAndSet(ctx, key, expected, value, opts)
	if err != nil {
		return Entry[T]{}, err
	}

	publicEntry := publicEntryFromSourceOfTruth(entry)
	cacheErr := s.populateTiers(ctx, 0, s.sourceOfTruthIndex(), publicEntry)

	if s.opts.StrictCacheWrites && cacheErr != nil {
		return publicEntry, cacheErr
	}

	return publicEntry, nil
}

func (s *TieredStore[T]) AppendMetadata(
	ctx context.Context,
	key string,
	metadata Metadata,
) (Entry[T], error) {
	entry, err := s.sourceOfTruth().AppendMetadata(ctx, key, metadata)
	if err != nil {
		return Entry[T]{}, err
	}

	publicEntry := publicEntryFromSourceOfTruth(entry)
	cacheErr := s.populateTiers(ctx, 0, s.sourceOfTruthIndex(), publicEntry)

	if s.opts.StrictCacheWrites && cacheErr != nil {
		return publicEntry, cacheErr
	}

	return publicEntry, nil
}

func (s *TieredStore[T]) Delete(ctx context.Context, key string) error {
	if err := s.sourceOfTruth().Delete(ctx, key); err != nil {
		return err
	}

	cacheErr := s.deleteTiers(ctx, 0, s.sourceOfTruthIndex(), key)
	if s.opts.StrictCacheWrites {
		return cacheErr
	}

	return nil
}

func (s *TieredStore[T]) Consume(ctx context.Context, key string) (Entry[T], error) {
	entry, err := s.sourceOfTruth().Consume(ctx, key)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			_ = s.deleteTiers(ctx, 0, s.sourceOfTruthIndex(), key)
		}
		return Entry[T]{}, err
	}

	cacheErr := s.deleteTiers(ctx, 0, s.sourceOfTruthIndex(), key)
	publicEntry := publicEntryFromSourceOfTruth(entry)

	if s.opts.StrictCacheWrites && cacheErr != nil {
		return publicEntry, cacheErr
	}

	return publicEntry, nil
}

func (s *TieredStore[T]) Query(ctx context.Context, query Query) (QueryResult[T], error) {
	result, err := s.sourceOfTruth().Query(ctx, query)
	if err != nil {
		return QueryResult[T]{}, err
	}

	publicEntries := make([]Entry[T], len(result.Entries))
	for i, entry := range result.Entries {
		publicEntries[i] = publicEntryFromSourceOfTruth(entry)
	}

	// Query uses the source-of-truth tier for consistent results. Returned
	// entries are opportunistically promoted into faster tiers.
	for _, entry := range publicEntries {
		_ = s.populateTiers(ctx, 0, s.sourceOfTruthIndex(), entry)
	}

	return QueryResult[T]{
		Entries:    publicEntries,
		NextCursor: result.NextCursor,
	}, nil
}

func (s *TieredStore[T]) Watch(
	ctx context.Context,
	opts WatchOptions,
) (<-chan Event[T], error) {
	source, err := s.sourceOfTruth().Watch(ctx, opts)
	if err != nil {
		return nil, err
	}

	out := make(chan Event[T], s.opts.WatchBuffer)

	go func() {
		defer close(out)

		for {
			select {
			case <-ctx.Done():
				return

			case event, ok := <-source:
				if !ok {
					return
				}

				publicEvent := publicEventFromSourceOfTruth(event)

				// Keep faster tiers updated if the source-of-truth changes,
				// including changes made outside this wrapper.
				s.applyEventToFastTiers(ctx, publicEvent)

				select {
				case out <- publicEvent:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return out, nil
}

func (s *TieredStore[T]) Batch(
	ctx context.Context,
	ops []Operation[T],
) ([]Entry[T], error) {
	if len(ops) == 0 {
		return nil, nil
	}

	entries, err := s.sourceOfTruth().Batch(ctx, ops)
	if err != nil {
		return nil, err
	}

	publicEntries := make([]Entry[T], len(entries))
	for i, entry := range entries {
		publicEntries[i] = publicEntryFromSourceOfTruth(entry)
	}

	cacheErr := s.applyBatchResultsToFastTiers(ctx, ops, publicEntries)
	if s.opts.StrictCacheWrites && cacheErr != nil {
		return publicEntries, cacheErr
	}

	return publicEntries, nil
}

func (s *TieredStore[T]) populateTiers(
	ctx context.Context,
	startInclusive int,
	endExclusive int,
	entry Entry[T],
) error {
	if startInclusive >= endExclusive {
		return nil
	}

	if entry.ExpiresAt != nil && !entry.ExpiresAt.After(time.Now()) {
		return s.deleteTiers(ctx, startInclusive, endExclusive, entry.Key)
	}

	cacheEntry := cacheEntryFromPublic(entry)

	opts := WriteOptions{
		TTL:      remainingTTL(cacheEntry),
		Metadata: cloneMetadata(cacheEntry.Metadata),
	}

	var errs []error

	for i := startInclusive; i < endExclusive; i++ {
		// Important: use Delete then Put instead of Put alone.
		//
		// Several backends merge metadata on Put. Since the cache entry contains
		// an internal metadata field for the source-of-truth ETag, merging can
		// leave stale internal ETags around. Delete then Put makes the cache copy
		// represent the latest authoritative entry exactly.
		if err := s.stores[i].Delete(ctx, cacheEntry.Key); err != nil && !errors.Is(err, ErrNotFound) {
			errs = append(errs, oops.Errorf("tier %d delete-before-populate %q: %w", i, cacheEntry.Key, err))
		}

		if _, err := s.stores[i].Put(ctx, cacheEntry.Key, cacheEntry.Value, opts); err != nil {
			errs = append(errs, oops.Errorf("tier %d populate %q: %w", i, cacheEntry.Key, err))
		}
	}

	return errors.Join(errs...)
}

func (s *TieredStore[T]) deleteTiers(
	ctx context.Context,
	startInclusive int,
	endExclusive int,
	key string,
) error {
	if startInclusive >= endExclusive {
		return nil
	}

	var errs []error

	for i := startInclusive; i < endExclusive; i++ {
		err := s.stores[i].Delete(ctx, key)
		if err == nil || errors.Is(err, ErrNotFound) {
			continue
		}

		errs = append(errs, oops.Errorf("tier %d delete %q: %w", i, key, err))
	}

	return errors.Join(errs...)
}

func (s *TieredStore[T]) applyEventToFastTiers(ctx context.Context, event Event[T]) {
	switch event.Type {
	case EventPut, EventCreate, EventCompareAndSet, EventMetadataAppend:
		if event.Entry != nil {
			_ = s.populateTiers(ctx, 0, s.sourceOfTruthIndex(), *event.Entry)
		}

	case EventDelete, EventExpire, EventConsume:
		_ = s.deleteTiers(ctx, 0, s.sourceOfTruthIndex(), event.Key)
	}
}

func (s *TieredStore[T]) applyBatchResultsToFastTiers(
	ctx context.Context,
	ops []Operation[T],
	entries []Entry[T],
) error {
	var errs []error

	resultIndex := 0

	for _, op := range ops {
		switch op.Type {
		case OpDelete:
			if err := s.deleteTiers(ctx, 0, s.sourceOfTruthIndex(), op.Key); err != nil {
				errs = append(errs, err)
			}

		case OpConsume:
			// Consume returns the consumed entry, but the key should disappear
			// from all faster tiers.
			if resultIndex >= len(entries) {
				errs = append(errs, oops.Errorf("%w: missing consume result for key %q", ErrInvalidBatch, op.Key))
			} else {
				resultIndex++
			}

			if err := s.deleteTiers(ctx, 0, s.sourceOfTruthIndex(), op.Key); err != nil {
				errs = append(errs, err)
			}

		case OpPut, OpCreate, OpCompareAndSet, OpMetadataAppend:
			if resultIndex >= len(entries) {
				errs = append(errs, oops.Errorf("%w: missing mutation result for key %q", ErrInvalidBatch, op.Key))
				continue
			}

			entry := entries[resultIndex]
			resultIndex++

			if err := s.populateTiers(ctx, 0, s.sourceOfTruthIndex(), entry); err != nil {
				errs = append(errs, err)
			}

		default:
			errs = append(errs, oops.Errorf("%w: unknown operation type %q", ErrInvalidBatch, op.Type))
		}
	}

	if resultIndex != len(entries) {
		errs = append(errs, oops.Errorf(
			"%w: batch returned %d result entries, consumed %d",
			ErrInvalidBatch,
			len(entries),
			resultIndex,
		))
	}

	return errors.Join(errs...)
}

func publicEventFromSourceOfTruth[T any](event Event[T]) Event[T] {
	if event.Entry == nil {
		return event
	}

	event.Entry = new(publicEntryFromSourceOfTruth(*event.Entry))
	return event
}

func publicEntryFromSourceOfTruth[T any](entry Entry[T]) Entry[T] {
	entry.Metadata = cloneMetadata(entry.Metadata)
	delete(entry.Metadata, tieredAuthoritativeETagMetadataKey)

	if len(entry.Metadata) == 0 {
		entry.Metadata = nil
	}

	return entry
}

func publicEntryFromTier[T any](entry Entry[T]) Entry[T] {
	entry.Metadata = cloneMetadata(entry.Metadata)

	if values := entry.Metadata[tieredAuthoritativeETagMetadataKey]; len(values) > 0 {
		// If a backend merged metadata in the past, the newest authoritative
		// ETag is expected to be last.
		for i := len(values) - 1; i >= 0; i-- {
			if values[i] != "" {
				entry.ETag = ETag(values[i])
				break
			}
		}
	}

	delete(entry.Metadata, tieredAuthoritativeETagMetadataKey)

	if len(entry.Metadata) == 0 {
		entry.Metadata = nil
	}

	return entry
}

func cacheEntryFromPublic[T any](entry Entry[T]) Entry[T] {
	entry.Metadata = cloneMetadata(entry.Metadata)
	if entry.Metadata == nil {
		entry.Metadata = Metadata{}
	}

	entry.Metadata[tieredAuthoritativeETagMetadataKey] = []string{string(entry.ETag)}

	return entry
}

func remainingTTL[T any](entry Entry[T]) time.Duration {
	if entry.ExpiresAt == nil {
		return 0
	}

	ttl := time.Until(*entry.ExpiresAt)
	if ttl <= 0 {
		return 1 * time.Nanosecond
	}

	return ttl
}

func cloneMetadata(metadata Metadata) Metadata {
	if metadata == nil {
		return nil
	}

	cloned := make(Metadata, len(metadata))
	for key, values := range metadata {
		if values == nil {
			cloned[key] = nil
			continue
		}

		copied := make([]string, len(values))
		copy(copied, values)
		cloned[key] = copied
	}

	return cloned
}

var _ Store[struct{}] = (*TieredStore[struct{}])(nil)
