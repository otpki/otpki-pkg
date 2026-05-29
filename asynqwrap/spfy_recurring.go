package asynqwrap

import (
	"context"

	"github.com/samber/oops"
)

type spfyRecurringStateStore struct{ broker *spfyBroker }

func (s *spfyRecurringStateStore) Upsert(ctx context.Context, key, version string) (bool, error) {
	stateKey := s.broker.recurringKey(key)
	for range 5 {
		_, etag, existed, err := s.broker.getState(ctx, stateKey)
		if err != nil {
			return false, err
		}
		doc := &spfyRecurringDoc{Kind: spfyKindRecurring, Key: key, Version: version, UpdatedAt: s.broker.now()}
		err = s.broker.applyStateOperation(
			ctx,
			spfySetRequest(stateKey, doc, etag, !existed, s.broker.stateMetadata(0)),
		)
		if err != nil {
			if isETagConflict(err) {
				continue
			}
			return false, err
		}
		return existed, nil
	}
	return false, oops.New("upsert recurring state failed after ETag retries")
}

func (s *spfyRecurringStateStore) Get(ctx context.Context, key string) (string, bool, error) {
	data, _, ok, err := s.broker.getState(ctx, s.broker.recurringKey(key))
	if err != nil || !ok {
		return "", false, err
	}
	doc, err := decodeSpfyRecurringDoc(data)
	if err != nil {
		return "", false, err
	}
	if doc == nil || doc.Version == "" {
		return "", false, nil
	}
	return doc.Version, true, nil
}

func (s *spfyRecurringStateStore) Delete(ctx context.Context, key string) error {
	return s.broker.deleteStateKey(ctx, s.broker.recurringKey(key), nil)
}
