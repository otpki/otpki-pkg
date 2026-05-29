package kafka

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/otpki/otpki-pkg/spfy/pulse"
	"github.com/otpki/otpki-pkg/spfy/pulse/internal/ids"
	"github.com/samber/oops"
	segmentio "github.com/segmentio/kafka-go"
)

type Writer interface {
	WriteMessages(context.Context, ...segmentio.Message) error
}

type Backend struct {
	writer    Writer
	newReader ReaderFactory
	cfg       Config
	closed    atomic.Bool
	mu        sync.Mutex
	subs      map[*subscription]struct{}
	topics    map[string]struct{}

	replyStartMu sync.Mutex
	replyMu      sync.Mutex
	replySubject string
	replyReader  *segmentio.Reader
	replyWaiters map[string]chan pulse.Message
}

func New(writer Writer, newReader ReaderFactory, cfg Config) (*Backend, error) {
	if writer == nil {
		return nil, oops.New("pulse kafka: nil writer")
	}
	if newReader == nil {
		return nil, oops.New("pulse kafka: nil reader factory")
	}
	cfg = normalizeConfig(cfg)
	return &Backend{
		writer:       writer,
		newReader:    newReader,
		cfg:          cfg,
		subs:         map[*subscription]struct{}{},
		topics:       map[string]struct{}{},
		replySubject: cfg.ReplyTopicPrefix + pulse.SafeName(ids.New()),
		replyWaiters: map[string]chan pulse.Message{},
	}, nil
}

func (b *Backend) Capabilities() pulse.Capabilities {
	return pulse.Capabilities{Fanout: true, WorkQueue: true, DurablePubSub: true, DurableRPC: true}
}
func (b *Backend) Shutdown(ctx context.Context) error {
	if b.closed.Swap(true) {
		return nil
	}

	b.replyMu.Lock()
	if b.replyReader != nil {
		_ = b.replyReader.Close()
		b.replyReader = nil
	}
	for correlationID, waiter := range b.replyWaiters {
		delete(b.replyWaiters, correlationID)
		close(waiter)
	}
	b.replyMu.Unlock()

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
	return b.publish(ctx, pulse.Envelope{Subject: subject, Message: msg, ID: ids.New(), Timestamp: time.Now().UTC()})
}
func (b *Backend) publish(ctx context.Context, env pulse.Envelope) error {
	if env.Subject == "" {
		return pulse.ErrInvalidName
	}
	if b.closed.Load() {
		return pulse.ErrClosed
	}
	if env.ID == "" {
		env.ID = ids.New()
	}
	if env.Timestamp.IsZero() {
		env.Timestamp = time.Now().UTC()
	}
	topic := b.topic(env.Subject)
	if err := b.ensureTopic(ctx, topic); err != nil {
		return err
	}
	return b.writer.WriteMessages(ctx, toKafkaMessage(topic, env))
}

func (b *Backend) Subscribe(ctx context.Context, subject string, handler pulse.Handler, opts pulse.SubscribeOptions) (pulse.Subscription, error) {
	if subject == "" || handler == nil {
		return nil, pulse.ErrInvalidName
	}
	return b.subscribe(ctx, subject, func(ctx context.Context, msg pulse.Message) (pulse.Message, error) {
		return pulse.Message{}, handler(ctx, msg)
	}, opts, false)
}
func (b *Backend) Handle(ctx context.Context, subject string, handler pulse.RequestHandler, opts pulse.SubscribeOptions) (pulse.Subscription, error) {
	if subject == "" || handler == nil {
		return nil, pulse.ErrInvalidName
	}
	opts.Delivery = pulse.WorkQueue
	return b.subscribe(ctx, subject, handler, opts, true)
}
func (b *Backend) subscribe(ctx context.Context, subject string, handler pulse.RequestHandler, opts pulse.SubscribeOptions, rpc bool) (pulse.Subscription, error) {
	if b.closed.Load() {
		return nil, pulse.ErrClosed
	}
	opts = pulse.NormalizeSubscribeOptions(subject, opts)
	topic := b.topic(subject)
	if err := b.ensureTopic(ctx, topic); err != nil {
		return nil, err
	}
	group := b.group(subject, opts, rpc)
	cfg := segmentio.ReaderConfig{Topic: topic, GroupID: group, MinBytes: b.cfg.MinBytes, MaxBytes: b.cfg.MaxBytes, CommitInterval: b.cfg.CommitInterval, StartOffset: startOffset(opts)}
	if opts.Delivery == pulse.Fanout && !rpc {
		cfg.GroupID = ""
		cfg.Partition = 0
	}
	r := b.newReader(cfg)
	s := &subscription{backend: b, reader: r, closed: make(chan struct{}), created: time.Now().UTC(), reliability: opts.Reliability, group: cfg.GroupID}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	go s.receive(ctx, handler, rpc)
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
	correlationID := ids.New()
	if opts.NoReply {
		go func() {
			defer close(ch)
			ch <- pulse.Result[pulse.Message]{Err: b.publish(ctx, pulse.Envelope{Subject: subject, Message: req.Clone(), CorrelationID: correlationID})}
		}()
		return pulse.NewFuture(ch), nil
	}
	reply, replies, cleanup, err := b.registerReply(ctx, correlationID)
	if err != nil {
		return pulse.Future[pulse.Message]{}, err
	}
	if err := b.publish(ctx, pulse.Envelope{Subject: subject, Message: req.Clone(), CorrelationID: correlationID, ReplyTo: reply}); err != nil {
		cleanup()
		return pulse.Future[pulse.Message]{}, err
	}
	go func() {
		defer close(ch)
		defer cleanup()
		callCtx := ctx
		var cancel context.CancelFunc
		if _, ok := callCtx.Deadline(); !ok && opts.Timeout > 0 {
			callCtx, cancel = context.WithTimeout(callCtx, opts.Timeout)
		}
		if cancel != nil {
			defer cancel()
		}
		select {
		case msg, ok := <-replies:
			if !ok {
				ch <- pulse.Result[pulse.Message]{Err: pulse.ErrClosed}
				return
			}
			ch <- pulse.Result[pulse.Message]{Value: msg}
		case <-callCtx.Done():
			ch <- pulse.Result[pulse.Message]{Err: callCtx.Err()}
		}
	}()
	return pulse.NewFuture(ch), nil
}

func (b *Backend) registerReply(ctx context.Context, correlationID string) (string, <-chan pulse.Message, func(), error) {
	if err := b.ensureReplyRouter(ctx); err != nil {
		return "", nil, nil, err
	}

	waiter := make(chan pulse.Message, 1)
	b.replyMu.Lock()
	if b.closed.Load() {
		b.replyMu.Unlock()
		close(waiter)
		return "", nil, nil, pulse.ErrClosed
	}
	b.replyWaiters[correlationID] = waiter
	reply := b.replySubject
	b.replyMu.Unlock()

	cleanup := func() {
		b.replyMu.Lock()
		if current, ok := b.replyWaiters[correlationID]; ok && current == waiter {
			delete(b.replyWaiters, correlationID)
			close(waiter)
		}
		b.replyMu.Unlock()
	}

	return reply, waiter, cleanup, nil
}

func (b *Backend) ensureReplyRouter(ctx context.Context) error {
	b.replyStartMu.Lock()
	defer b.replyStartMu.Unlock()

	b.replyMu.Lock()
	if b.replyReader != nil {
		b.replyMu.Unlock()
		return nil
	}
	if b.closed.Load() {
		b.replyMu.Unlock()
		return pulse.ErrClosed
	}
	replySubject := b.replySubject
	replyTopic := b.topic(replySubject)
	b.replyMu.Unlock()

	if err := b.ensureTopic(ctx, replyTopic); err != nil {
		return err
	}

	r := b.newReader(segmentio.ReaderConfig{
		Topic:       replyTopic,
		Partition:   0,
		MinBytes:    b.cfg.MinBytes,
		MaxBytes:    b.cfg.MaxBytes,
		StartOffset: segmentio.FirstOffset,
	})

	b.replyMu.Lock()
	if b.closed.Load() {
		b.replyMu.Unlock()
		_ = r.Close()
		return pulse.ErrClosed
	}
	if b.replyReader != nil {
		b.replyMu.Unlock()
		_ = r.Close()
		return nil
	}
	b.replyReader = r
	b.replyMu.Unlock()

	go b.receiveReplies(r)
	return nil
}

func (b *Backend) receiveReplies(r *segmentio.Reader) {
	for {
		km, err := r.FetchMessage(context.Background())
		if err != nil {
			if b.closed.Load() {
				return
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}

		env := fromKafkaMessage(km)
		if env.CorrelationID == "" {
			continue
		}

		b.replyMu.Lock()
		waiter, ok := b.replyWaiters[env.CorrelationID]
		if ok {
			delete(b.replyWaiters, env.CorrelationID)
		}
		b.replyMu.Unlock()

		if !ok {
			continue
		}

		select {
		case waiter <- env.Message.Clone():
		default:
		}
		close(waiter)
	}
}

func (b *Backend) ensureTopic(ctx context.Context, topic string) error {
	b.mu.Lock()
	if _, ok := b.topics[topic]; ok {
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()

	if b.cfg.EnsureTopic != nil {
		if err := b.cfg.EnsureTopic(ctx, topic); err != nil && !isTopicAlreadyExists(err) {
			return err
		}
	}

	b.mu.Lock()
	b.topics[topic] = struct{}{}
	b.mu.Unlock()
	return nil
}

func isTopicAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "already exists") || strings.Contains(s, "topic with this name already exists")
}

func (b *Backend) topic(subject string) string {
	subject = strings.ReplaceAll(subject, "/", ".")
	if b.cfg.TopicPrefix == "" {
		return subject
	}
	return strings.Trim(b.cfg.TopicPrefix, ".") + "." + strings.TrimPrefix(subject, ".")
}
func (b *Backend) group(subject string, opts pulse.SubscribeOptions, rpc bool) string {
	var g string
	if rpc {
		g = "pulse-rpc-" + pulse.SafeName(subject)
	} else if opts.Delivery == pulse.WorkQueue {
		g = opts.Group
	} else {
		g = opts.Group
		if g == "" {
			g = ids.New()
		}
	}
	if b.cfg.GroupPrefix != "" {
		g = strings.Trim(b.cfg.GroupPrefix, ".") + "." + g
	}
	return pulse.SafeName(g)
}
func startOffset(pulse.SubscribeOptions) int64 {
	// Start at the beginning and let the envelope timestamp filter enforce
	// ephemeral semantics. This avoids Kafka group/direct-reader races where a
	// message published immediately after Subscribe could be missed if the
	// reader lazily resolves LastOffset on first FetchMessage.
	return segmentio.FirstOffset
}

type subscription struct {
	backend     *Backend
	reader      *segmentio.Reader
	closed      chan struct{}
	once        sync.Once
	created     time.Time
	reliability pulse.Reliability
	group       string
}

func (s *subscription) Close(ctx context.Context) error {
	var err error
	s.once.Do(func() {
		close(s.closed)
		err = s.reader.Close()
		s.backend.mu.Lock()
		delete(s.backend.subs, s)
		s.backend.mu.Unlock()
	})
	if err != nil {
		return err
	}
	return ctx.Err()
}
func (s *subscription) receive(ctx context.Context, handler pulse.RequestHandler, rpc bool) {
	for {
		select {
		case <-s.closed:
			return
		default:
		}
		km, err := s.reader.FetchMessage(ctx)
		if err != nil {
			select {
			case <-s.closed:
				return
			default:
			}
			if ctx.Err() != nil {
				return
			}
			continue
		}
		env := fromKafkaMessage(km)
		if s.reliability == pulse.Ephemeral && env.Timestamp.Before(s.created) {
			if s.group != "" {
				_ = s.reader.CommitMessages(ctx, km)
			}
			continue
		}
		resp, err := handler(ctx, env.Message)
		if err == nil && rpc && env.ReplyTo != "" {
			pubCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err = s.backend.publish(pubCtx, pulse.Envelope{Subject: env.ReplyTo, Message: resp.Clone(), CorrelationID: env.CorrelationID})
			cancel()
		}
		if err == nil && s.group != "" {
			_ = s.reader.CommitMessages(ctx, km)
		}
	}
}

func toKafkaMessage(topic string, env pulse.Envelope) segmentio.Message {
	headers := []segmentio.Header{{Key: "pulse-id", Value: []byte(env.ID)}, {Key: "pulse-subject", Value: []byte(env.Subject)}, {Key: "pulse-timestamp", Value: []byte(env.Timestamp.Format(time.RFC3339Nano))}, {Key: "pulse-correlation-id", Value: []byte(env.CorrelationID)}, {Key: "pulse-reply-to", Value: []byte(env.ReplyTo)}}
	for k, vs := range env.Message.Header {
		for _, v := range vs {
			headers = append(headers, segmentio.Header{Key: k, Value: []byte(v)})
		}
	}
	return segmentio.Message{Topic: topic, Key: []byte(env.CorrelationID), Value: append([]byte(nil), env.Message.Data...), Time: env.Timestamp, Headers: headers}
}
func fromKafkaMessage(km segmentio.Message) pulse.Envelope {
	h := pulse.Header{}
	env := pulse.Envelope{Message: pulse.Message{Header: h, Data: append([]byte(nil), km.Value...)}, Timestamp: km.Time}
	if env.Timestamp.IsZero() {
		env.Timestamp = time.Now().UTC()
	}
	for _, hdr := range km.Headers {
		v := string(hdr.Value)
		switch strings.ToLower(hdr.Key) {
		case "pulse-id":
			env.ID = v
		case "pulse-subject":
			env.Subject = v
		case "pulse-timestamp":
			if ts, err := time.Parse(time.RFC3339Nano, v); err == nil {
				env.Timestamp = ts
			}
		case "pulse-correlation-id":
			env.CorrelationID = v
		case "pulse-reply-to":
			env.ReplyTo = v
		default:
			h[hdr.Key] = append(h[hdr.Key], v)
		}
	}
	if env.Subject == "" {
		env.Subject = km.Topic
	}
	return env
}

var _ pulse.Backend = (*Backend)(nil)
var _ pulse.Subscription = (*subscription)(nil)
