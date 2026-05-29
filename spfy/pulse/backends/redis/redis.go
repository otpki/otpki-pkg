package redis

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/otpki/otpki-pkg/spfy/pulse"
	"github.com/otpki/otpki-pkg/spfy/pulse/internal/ids"
	goredis "github.com/redis/go-redis/v9"
	"github.com/samber/oops"
)

type Backend struct {
	client goredis.UniversalClient
	cfg    Config
	closed atomic.Bool
	mu     sync.Mutex
	subs   map[*subscription]struct{}
}

func New(client goredis.UniversalClient, cfg Config) (*Backend, error) {
	if client == nil {
		return nil, oops.New("pulse redis: nil client")
	}
	return &Backend{client: client, cfg: normalizeConfig(cfg), subs: map[*subscription]struct{}{}}, nil
}
func (b *Backend) Capabilities() pulse.Capabilities {
	return pulse.Capabilities{Fanout: true, WorkQueue: true, DurablePubSub: true, DurableRPC: true}
}
func (b *Backend) Shutdown(ctx context.Context) error {
	if b.closed.Swap(true) {
		return nil
	}
	b.mu.Lock()
	subs := make([]*subscription, 0, len(b.subs))
	for s := range b.subs {
		subs = append(subs, s)
	}
	b.subs = map[*subscription]struct{}{}
	b.mu.Unlock()
	for _, s := range subs {
		_ = s.Close(ctx)
	}
	return ctx.Err()
}
func (b *Backend) Publish(ctx context.Context, subject string, msg pulse.Message, opts pulse.PublishOptions) error {
	env := pulse.Envelope{ID: ids.New(), Subject: subject, Message: msg.Clone(), Timestamp: time.Now().UTC()}
	if err := b.publishStream(ctx, env, opts.Reliability); err != nil {
		return err
	}
	if opts.Reliability == pulse.Ephemeral {
		return b.client.Publish(ctx, b.channel(subject), encodeEnvelope(env)).Err()
	}
	return nil
}
func (b *Backend) publishStream(ctx context.Context, env pulse.Envelope, r pulse.Reliability) error {
	if env.Subject == "" {
		return pulse.ErrInvalidName
	}
	if b.closed.Load() {
		return pulse.ErrClosed
	}
	vals, err := streamValues(env)
	if err != nil {
		return err
	}
	args := &goredis.XAddArgs{Stream: b.stream(env.Subject), Values: vals}
	if r == pulse.Ephemeral && b.cfg.StreamMaxLen > 0 {
		args.MaxLen = b.cfg.StreamMaxLen
	}
	return b.client.XAdd(ctx, args).Err()
}
func (b *Backend) Subscribe(ctx context.Context, subject string, handler pulse.Handler, opts pulse.SubscribeOptions) (pulse.Subscription, error) {
	if subject == "" || handler == nil {
		return nil, pulse.ErrInvalidName
	}
	if b.closed.Load() {
		return nil, pulse.ErrClosed
	}
	opts = pulse.NormalizeSubscribeOptions(subject, opts)
	if opts.Delivery == pulse.Fanout && opts.Reliability == pulse.Ephemeral {
		ps := b.client.Subscribe(ctx, b.channel(subject))
		if _, err := ps.Receive(ctx); err != nil {
			_ = ps.Close()
			return nil, err
		}
		s := &subscription{backend: b, pubsub: ps, closed: make(chan struct{})}
		b.mu.Lock()
		b.subs[s] = struct{}{}
		b.mu.Unlock()
		go s.receivePubSub(ctx, handler)
		return s, nil
	}
	return b.subscribeStream(ctx, subject, func(ctx context.Context, msg pulse.Message) (pulse.Message, error) {
		return pulse.Message{}, handler(ctx, msg)
	}, opts, false)
}
func (b *Backend) Handle(ctx context.Context, subject string, handler pulse.RequestHandler, opts pulse.SubscribeOptions) (pulse.Subscription, error) {
	if subject == "" || handler == nil {
		return nil, pulse.ErrInvalidName
	}
	opts.Delivery = pulse.WorkQueue
	return b.subscribeStream(ctx, subject, handler, opts, true)
}
func (b *Backend) subscribeStream(ctx context.Context, subject string, handler pulse.RequestHandler, opts pulse.SubscribeOptions, rpc bool) (pulse.Subscription, error) {
	if b.closed.Load() {
		return nil, pulse.ErrClosed
	}
	opts = pulse.NormalizeSubscribeOptions(subject, opts)
	stream := b.stream(subject)
	group := b.group(subject, opts, rpc)
	_ = b.client.XGroupCreateMkStream(ctx, stream, group, "$").Err()
	s := &subscription{backend: b, stream: stream, group: group, consumer: b.consumerName(), closed: make(chan struct{}), rpc: rpc, reliability: opts.Reliability}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	go s.receiveStream(ctx, handler)
	return s, nil
}
func (b *Backend) Request(ctx context.Context, subject string, req pulse.Message, opts pulse.RequestOptions) (pulse.Message, error) {
	f, err := b.RequestAsync(ctx, subject, req, opts)
	if err != nil {
		return pulse.Message{}, err
	}
	return f.Await(ctx)
}
func (b *Backend) RequestAsync(ctx context.Context, subject string, req pulse.Message, opts pulse.RequestOptions) (pulse.Future[pulse.Message], error) {
	if subject == "" {
		return pulse.Future[pulse.Message]{}, pulse.ErrInvalidName
	}
	if b.closed.Load() {
		return pulse.Future[pulse.Message]{}, pulse.ErrClosed
	}
	ch := make(chan pulse.Result[pulse.Message], 1)
	corr := ids.New()
	if opts.NoReply {
		go func() {
			defer close(ch)
			ch <- pulse.Result[pulse.Message]{Err: b.publishStream(ctx, pulse.Envelope{ID: ids.New(), Subject: subject, Message: req.Clone(), Timestamp: time.Now().UTC(), CorrelationID: corr}, opts.Reliability)}
		}()
		return pulse.NewFuture(ch), nil
	}
	reply := b.cfg.ReplyPrefix + pulse.SafeName(corr)
	replies := make(chan pulse.Message, 1)
	sub, err := b.Subscribe(ctx, reply, func(ctx context.Context, msg pulse.Message) error {
		select {
		case replies <- msg.Clone():
		default:
		}
		return nil
	}, pulse.SubscribeOptions{Delivery: pulse.WorkQueue, Group: corr, Reliability: opts.Reliability})
	if err != nil {
		return pulse.Future[pulse.Message]{}, err
	}
	if err := b.publishStream(ctx, pulse.Envelope{ID: ids.New(), Subject: subject, Message: req.Clone(), Timestamp: time.Now().UTC(), CorrelationID: corr, ReplyTo: reply}, opts.Reliability); err != nil {
		_ = sub.Close(context.Background())
		return pulse.Future[pulse.Message]{}, err
	}
	go func() {
		defer close(ch)
		defer sub.Close(context.Background())
		callCtx := ctx
		var cancel context.CancelFunc
		if _, ok := callCtx.Deadline(); !ok && opts.Timeout > 0 {
			callCtx, cancel = context.WithTimeout(callCtx, opts.Timeout)
		}
		if cancel != nil {
			defer cancel()
		}
		select {
		case msg := <-replies:
			ch <- pulse.Result[pulse.Message]{Value: msg}
		case <-callCtx.Done():
			ch <- pulse.Result[pulse.Message]{Err: callCtx.Err()}
		}
	}()
	return pulse.NewFuture(ch), nil
}
func (b *Backend) stream(subject string) string  { return b.key("stream", subject) }
func (b *Backend) channel(subject string) string { return b.key("chan", subject) }
func (b *Backend) key(kind, subject string) string {
	s := strings.ReplaceAll(subject, "/", ".")
	if b.cfg.KeyPrefix == "" {
		return "pulse:" + kind + ":" + s
	}
	return strings.TrimRight(b.cfg.KeyPrefix, ":") + ":" + kind + ":" + s
}
func (b *Backend) group(subject string, opts pulse.SubscribeOptions, rpc bool) string {
	if rpc {
		return "rpc_" + pulse.SafeName(subject)
	}
	if opts.Group != "" {
		return pulse.SafeName(opts.Group)
	}
	return pulse.SafeName(subject) + "_" + ids.New()
}
func (b *Backend) consumerName() string { return pulse.SafeName(b.cfg.ConsumerName) + "_" + ids.New() }

type subscription struct {
	backend                 *Backend
	pubsub                  *goredis.PubSub
	stream, group, consumer string
	closed                  chan struct{}
	once                    sync.Once
	rpc                     bool
	reliability             pulse.Reliability
}

func (s *subscription) Close(ctx context.Context) error {
	var err error
	s.once.Do(func() {
		close(s.closed)
		if s.pubsub != nil {
			err = s.pubsub.Close()
		}
		s.backend.mu.Lock()
		delete(s.backend.subs, s)
		s.backend.mu.Unlock()
	})
	if err != nil {
		return err
	}
	return ctx.Err()
}
func (s *subscription) receivePubSub(ctx context.Context, handler pulse.Handler) {
	ch := s.pubsub.Channel()
	for {
		select {
		case <-s.closed:
			return
		case <-ctx.Done():
			return
		case pm, ok := <-ch:
			if !ok {
				return
			}
			env, err := decodeEnvelope(pm.Payload)
			if err == nil {
				_ = handler(ctx, env.Message)
			}
		}
	}
}
func (s *subscription) receiveStream(ctx context.Context, handler pulse.RequestHandler) {
	for {
		select {
		case <-s.closed:
			return
		default:
		}
		res, err := s.backend.client.XReadGroup(ctx, &goredis.XReadGroupArgs{Group: s.group, Consumer: s.consumer, Streams: []string{s.stream, ">"}, Count: 1, Block: s.backend.cfg.BlockTimeout}).Result()
		if err != nil {
			if errors.Is(err, goredis.Nil) {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			select {
			case <-s.closed:
				return
			default:
			}
			continue
		}
		for _, stream := range res {
			for _, xm := range stream.Messages {
				env, err := envelopeFromValues(xm.Values)
				if err != nil {
					continue
				}
				resp, err := handler(ctx, env.Message)
				if err == nil && s.rpc && env.ReplyTo != "" {
					pubCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					err = s.backend.publishStream(pubCtx, pulse.Envelope{ID: ids.New(), Subject: env.ReplyTo, Message: resp.Clone(), Timestamp: time.Now().UTC(), CorrelationID: env.CorrelationID}, s.reliability)
					cancel()
				}
				if err == nil {
					_ = s.backend.client.XAck(ctx, s.stream, s.group, xm.ID).Err()
				}
			}
		}
	}
}
func streamValues(env pulse.Envelope) (map[string]any, error) {
	enc, err := pulse.EncodeHeader(env.Message.Header)
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": env.ID, "subject": env.Subject, "ts": env.Timestamp.Format(time.RFC3339Nano), "cid": env.CorrelationID, "reply": env.ReplyTo, "headers": enc, "data": base64.RawStdEncoding.EncodeToString(env.Message.Data)}, nil
}
func envelopeFromValues(v map[string]any) (pulse.Envelope, error) {
	get := func(k string) string {
		if x, ok := v[k]; ok && x != nil {
			return toString(x)
		}
		return ""
	}
	h, _ := pulse.DecodeHeader(get("headers"))
	data, _ := base64.RawStdEncoding.DecodeString(get("data"))
	ts := time.Now().UTC()
	if raw := get("ts"); raw != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			ts = parsed
		}
	}
	return pulse.Envelope{ID: get("id"), Subject: get("subject"), Message: pulse.Message{Header: h, Data: data}, Timestamp: ts, CorrelationID: get("cid"), ReplyTo: get("reply")}, nil
}
func encodeEnvelope(env pulse.Envelope) string {
	vals, _ := streamValues(env)
	b, _ := jsonMarshal(vals)
	return string(b)
}
func decodeEnvelope(s string) (pulse.Envelope, error) {
	var vals map[string]any
	if err := jsonUnmarshal([]byte(s), &vals); err != nil {
		return pulse.Envelope{}, err
	}
	return envelopeFromValues(vals)
}
func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	default:
		return ""
	}
}

// tiny wrappers keep json import localized in generated object code.
var jsonMarshal = func(v any) ([]byte, error) { return json.Marshal(v) }
var jsonUnmarshal = func(b []byte, v any) error { return json.Unmarshal(b, v) }

var _ pulse.Backend = (*Backend)(nil)
var _ pulse.Subscription = (*subscription)(nil)
