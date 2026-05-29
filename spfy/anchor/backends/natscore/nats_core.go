package natscore

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/otpki/otpki-pkg/spfy/anchor"
	"github.com/otpki/otpki-pkg/spfy/anchor/backends/memory"
	"github.com/otpki/otpki-pkg/spfy/anchor/internal"
)

type backend struct {
	nc        *nats.Conn
	subject   string
	origin    string
	mem       *memory.Backend
	eventSub  *nats.Subscription
	snapSub   *nats.Subscription
	opTimeout time.Duration
}

type natsCoreEnvelope struct {
	Origin string               `json:"origin"`
	Event  internal.StoredEvent `json:"event"`
}

type natsCoreSnapshot struct {
	Entries []internal.StoredEntry `json:"entries"`
}

func newBackend(ctx context.Context, nc *nats.Conn, cfg Config) (*backend, error) {
	defaults := DefaultConfig()
	if cfg.Subject == "" {
		cfg.Subject = defaults.Subject
	}
	if cfg.Origin == "" {
		cfg.Origin = string(anchor.NewETag())
	}
	if cfg.OperationTimeout <= 0 {
		cfg.OperationTimeout = defaults.OperationTimeout
	}
	if cfg.BootstrapTimeout == 0 {
		cfg.BootstrapTimeout = defaults.BootstrapTimeout
	}
	s := &backend{
		nc:        nc,
		subject:   cfg.Subject,
		origin:    cfg.Origin,
		mem:       memory.NewBackend(memory.Config{WatchBuffer: cfg.WatchBuffer}),
		opTimeout: cfg.OperationTimeout,
	}
	if cfg.BootstrapTimeout > 0 {
		if err := s.bootstrap(
			ctx,
			cfg.BootstrapTimeout,
		); err != nil && !errors.Is(err, nats.ErrNoResponders) &&
			!errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
	}
	eventSub, err := nc.Subscribe(s.subject+".events", func(msg *nats.Msg) {
		var env natsCoreEnvelope
		if json.Unmarshal(msg.Data, &env) != nil || env.Origin == s.origin {
			return
		}
		_ = s.mem.ApplyReplicatedEvent(context.Background(), env.Event)
	})
	if err != nil {
		return nil, err
	}
	s.eventSub = eventSub
	snapSub, err := nc.Subscribe(s.subject+".snapshot", func(msg *nats.Msg) {
		entries, _ := s.mem.AllEntries(context.Background())
		inner, _ := json.Marshal(natsCoreSnapshot{Entries: entries})
		_ = msg.Respond(inner)
	})
	if err != nil {
		_ = eventSub.Unsubscribe()
		return nil, err
	}
	s.snapSub = snapSub
	flushCtx, flushCancel := internal.NATSDeadlineContext(ctx, s.opTimeout)
	if err := nc.FlushWithContext(flushCtx); err != nil {
		flushCancel()
		_ = eventSub.Unsubscribe()
		_ = snapSub.Unsubscribe()
		return nil, err
	}
	flushCancel()
	return s, nil
}

func New[T any](
	ctx context.Context,
	nc *nats.Conn,
	cfg Config,
	codec anchor.Codec[T],
) (anchor.Store[T], error) {
	inner, err := newBackend(ctx, nc, cfg)
	if err != nil {
		return nil, err
	}
	return internal.NewStoreAdapter[T](inner, codec), nil
}

func (s *backend) Close() error {
	if s.eventSub != nil {
		_ = s.eventSub.Unsubscribe()
	}
	if s.snapSub != nil {
		_ = s.snapSub.Unsubscribe()
	}
	return nil
}

func (s *backend) bootstrap(ctx context.Context, timeout time.Duration) error {
	bctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	msg, err := s.nc.RequestWithContext(bctx, s.subject+".snapshot", nil)
	if err != nil {
		return err
	}
	var snap natsCoreSnapshot
	if err := json.Unmarshal(msg.Data, &snap); err != nil {
		return err
	}
	return s.mem.ReplaceAll(ctx, snap.Entries)
}

func (s *backend) Get(ctx context.Context, key string) (internal.StoredEntry, error) {
	return s.mem.Get(ctx, key)
}
func (s *backend) Query(ctx context.Context, query anchor.Query) (internal.StoredQueryResult, error) {
	return s.mem.Query(ctx, query)
}
func (s *backend) Watch(ctx context.Context, opts anchor.WatchOptions) (<-chan internal.StoredEvent, error) {
	return s.mem.Watch(ctx, opts)
}

func (s *backend) Put(ctx context.Context, key string, value []byte, opts anchor.WriteOptions) (internal.StoredEntry, error) {
	entry, err := s.mem.Put(ctx, key, value, opts)
	if err == nil {
		s.publish(internal.StoredEventFor(anchor.EventPut, key, &entry, time.Now().UTC()))
	}
	return entry, err
}

func (s *backend) Create(
	ctx context.Context,
	key string,
	value []byte,
	opts anchor.WriteOptions,
) (internal.StoredEntry, error) {
	entry, err := s.mem.Create(ctx, key, value, opts)
	if err == nil {
		s.publish(internal.StoredEventFor(anchor.EventCreate, key, &entry, time.Now().UTC()))
	}
	return entry, err
}

func (s *backend) CompareAndSet(
	ctx context.Context,
	key string,
	expected anchor.ETag,
	value []byte,
	opts anchor.WriteOptions,
) (internal.StoredEntry, error) {
	entry, err := s.mem.CompareAndSet(ctx, key, expected, value, opts)
	if err == nil {
		s.publish(internal.StoredEventFor(anchor.EventCompareAndSet, key, &entry, time.Now().UTC()))
	}
	return entry, err
}
func (s *backend) AppendMetadata(ctx context.Context, key string, metadata anchor.Metadata) (internal.StoredEntry, error) {
	entry, err := s.mem.AppendMetadata(ctx, key, metadata)
	if err == nil {
		s.publish(internal.StoredEventFor(anchor.EventMetadataAppend, key, &entry, time.Now().UTC()))
	}
	return entry, err
}
func (s *backend) Delete(ctx context.Context, key string) error {
	err := s.mem.Delete(ctx, key)
	if err == nil {
		s.publish(internal.StoredEventFor(anchor.EventDelete, key, nil, time.Now().UTC()))
	}
	return err
}
func (s *backend) Consume(ctx context.Context, key string) (internal.StoredEntry, error) {
	entry, err := s.mem.Consume(ctx, key)
	if err == nil {
		s.publish(internal.StoredEventFor(anchor.EventConsume, key, &entry, time.Now().UTC()))
	}
	return entry, err
}
func (s *backend) Batch(ctx context.Context, ops []internal.StoredOperation) ([]internal.StoredEntry, error) {
	entries, err := s.mem.Batch(ctx, ops)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	resultIdx := 0
	for _, op := range ops {
		switch op.Type {
		case anchor.OpPut:
			e := entries[resultIdx]
			resultIdx++
			s.publish(internal.StoredEventFor(anchor.EventPut, op.Key, &e, now))
		case anchor.OpCreate:
			e := entries[resultIdx]
			resultIdx++
			s.publish(internal.StoredEventFor(anchor.EventCreate, op.Key, &e, now))
		case anchor.OpCompareAndSet:
			e := entries[resultIdx]
			resultIdx++
			s.publish(internal.StoredEventFor(anchor.EventCompareAndSet, op.Key, &e, now))
		case anchor.OpMetadataAppend:
			e := entries[resultIdx]
			resultIdx++
			s.publish(internal.StoredEventFor(anchor.EventMetadataAppend, op.Key, &e, now))
		case anchor.OpDelete:
			s.publish(internal.StoredEventFor(anchor.EventDelete, op.Key, nil, now))
		case anchor.OpConsume:
			e := entries[resultIdx]
			resultIdx++
			s.publish(internal.StoredEventFor(anchor.EventConsume, op.Key, &e, now))
		}
	}
	return entries, nil
}

func (s *backend) publish(ev internal.StoredEvent) {
	inner, _ := json.Marshal(natsCoreEnvelope{Origin: s.origin, Event: ev})
	_ = s.nc.Publish(s.subject+".events", inner)
}
