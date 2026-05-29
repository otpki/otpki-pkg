package pulsar

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pulsarclient "github.com/apache/pulsar-client-go/pulsar"
	"github.com/otpki/otpki-pkg/spfy/pulse"
	"github.com/otpki/otpki-pkg/spfy/pulse/internal/ids"
	"github.com/samber/oops"
)

type Backend struct {
	client    pulsarclient.Client
	cfg       Config
	closed    atomic.Bool
	mu        sync.Mutex
	producers map[string]pulsarclient.Producer
	subs      map[*subscription]struct{}
}

func New(client pulsarclient.Client, cfg Config) (*Backend, error) {
	if client == nil {
		return nil, oops.New("pulse pulsar: nil client")
	}
	return &Backend{client: client, cfg: normalizeConfig(cfg), producers: map[string]pulsarclient.Producer{}, subs: map[*subscription]struct{}{}}, nil
}

func (b *Backend) Capabilities() pulse.Capabilities {
	return pulse.Capabilities{Fanout: true, WorkQueue: true, DurablePubSub: true, DurableRPC: true}
}

func (b *Backend) Shutdown(ctx context.Context) error {
	if b.closed.Swap(true) {
		return nil
	}
	b.mu.Lock()
	producers := b.producers
	b.producers = map[string]pulsarclient.Producer{}
	subs := make([]*subscription, 0, len(b.subs))
	for s := range b.subs {
		subs = append(subs, s)
	}
	b.subs = map[*subscription]struct{}{}
	b.mu.Unlock()
	for _, s := range subs {
		_ = s.Close(ctx)
	}
	for _, p := range producers {
		p.Close()
	}
	return ctx.Err()
}

func (b *Backend) Publish(ctx context.Context, subject string, msg pulse.Message, opts pulse.PublishOptions) error {
	return b.publish(ctx, pulse.Envelope{Subject: subject, Message: msg}, opts)
}

func (b *Backend) publish(ctx context.Context, env pulse.Envelope, opts pulse.PublishOptions) error {
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
	topic := b.topic(env.Subject, opts.Reliability)
	p, err := b.producer(topic)
	if err != nil {
		return err
	}
	_, err = p.Send(ctx, &pulsarclient.ProducerMessage{Payload: append([]byte(nil), env.Message.Data...), Properties: propertiesFromEnvelope(env)})
	return err
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
	topic := b.topic(subject, opts.Reliability)
	if err := b.ensureTopic(ctx, topic); err != nil {
		return nil, err
	}
	subName := b.subscriptionName(subject, opts, rpc)
	consumer, err := b.subscribeConsumer(ctx, pulsarclient.ConsumerOptions{Topic: topic, SubscriptionName: subName, Type: b.subscriptionType(opts, rpc), SubscriptionMode: subscriptionMode(opts.Reliability), SubscriptionInitialPosition: initialPosition(opts.Reliability), ReceiverQueueSize: b.cfg.ReceiverQueueSize})
	if err != nil {
		return nil, err
	}
	s := &subscription{backend: b, consumer: consumer, closed: make(chan struct{})}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	go s.receive(ctx, handler, rpc, opts.Reliability)
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
			ch <- pulse.Result[pulse.Message]{Err: b.publish(ctx, pulse.Envelope{Subject: subject, Message: req.Clone(), CorrelationID: correlationID}, pulse.PublishOptions{Reliability: opts.Reliability})}
		}()
		return pulse.NewFuture(ch), nil
	}
	reply := b.cfg.ReplyTopicPrefix + correlationID
	replies := make(chan pulse.Message, 1)
	sub, err := b.Subscribe(ctx, reply, func(ctx context.Context, msg pulse.Message) error {
		select {
		case replies <- msg.Clone():
		default:
		}
		return nil
	}, pulse.SubscribeOptions{Delivery: pulse.Fanout, Group: pulse.SafeName(reply), Reliability: opts.Reliability})
	if err != nil {
		return pulse.Future[pulse.Message]{}, err
	}
	if err := b.publish(ctx, pulse.Envelope{Subject: subject, Message: req.Clone(), CorrelationID: correlationID, ReplyTo: reply}, pulse.PublishOptions{Reliability: opts.Reliability}); err != nil {
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

func (b *Backend) ensureTopic(ctx context.Context, topic string) error {
	var last error
	for i := 0; i < 12; i++ {
		p, err := b.producer(topic)
		if err == nil {
			_ = p.Flush()
			return nil
		}
		last = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff(i)):
		}
	}
	return last
}

func (b *Backend) subscribeConsumer(ctx context.Context, opts pulsarclient.ConsumerOptions) (pulsarclient.Consumer, error) {
	var last error
	for i := 0; i < 12; i++ {
		consumer, err := b.client.Subscribe(opts)
		if err == nil {
			return consumer, nil
		}
		last = err
		if ensureErr := b.ensureTopic(ctx, opts.Topic); ensureErr != nil {
			last = ensureErr
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff(i)):
		}
	}
	return nil, last
}

func backoff(i int) time.Duration {
	d := time.Duration(i+1) * 250 * time.Millisecond
	if d > 2*time.Second {
		return 2 * time.Second
	}
	return d
}

func (b *Backend) producer(topic string) (pulsarclient.Producer, error) {
	b.mu.Lock()
	if b.closed.Load() {
		b.mu.Unlock()
		return nil, pulse.ErrClosed
	}
	if p, ok := b.producers[topic]; ok {
		b.mu.Unlock()
		return p, nil
	}
	b.mu.Unlock()
	opts := pulsarclient.ProducerOptions{Topic: topic, DisableBatching: b.cfg.DisableBatching, BatchingMaxPublishDelay: b.cfg.BatchingMaxPublishDelay, BatchingMaxMessages: b.cfg.BatchingMaxMessages, BatchingMaxSize: b.cfg.BatchingMaxSize}
	p, err := b.client.CreateProducer(opts)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed.Load() {
		p.Close()
		return nil, pulse.ErrClosed
	}
	if existing, ok := b.producers[topic]; ok {
		p.Close()
		return existing, nil
	}
	b.producers[topic] = p
	return p, nil
}

func (b *Backend) topic(subject string, reliability pulse.Reliability) string {
	if strings.Contains(subject, "://") {
		return subject
	}

	local := strings.Trim(subject, "/.")
	local = strings.ReplaceAll(local, "/", ".")
	local = pulse.SafeName(local)
	if prefix := strings.Trim(b.cfg.TopicPrefix, "/."); prefix != "" {
		prefix = strings.ReplaceAll(prefix, "/", ".")
		local = pulse.SafeName(prefix) + "." + local
	}
	if local == "" {
		local = "default"
	}

	// Use persistent topics for both reliability modes. Ephemeral Pulse semantics
	// are represented by non-durable subscriptions, while persistent topics avoid
	// broker-specific non-persistent-topic lookup races in standalone/test setups.
	return "persistent://" + b.cfg.Tenant + "/" + b.cfg.Namespace + "/" + local
}

func (b *Backend) subscriptionName(subject string, opts pulse.SubscribeOptions, rpc bool) string {
	if rpc {
		return "pulse-rpc-" + pulse.SafeName(subject)
	}

	if opts.Delivery == pulse.WorkQueue {
		// WorkQueue delivery is implemented by having all consumers in the same
		// logical Pulse group join the same Pulsar Shared subscription. The prior
		// implementation generated a unique subscription name for ephemeral
		// WorkQueue subscribers, which turned the work queue into fanout: every
		// subscriber received every message. Keep Fanout subscribers unique, but
		// make WorkQueue subscription names stable per subject/group.
		if opts.Reliability == pulse.Durable && b.cfg.SubscriptionName != "" {
			return b.cfg.SubscriptionName
		}
		group := opts.Group
		if group == "" {
			group = pulse.DefaultGroup(subject)
		}
		return "pulse-wq-" + pulse.SafeName(subject) + "-" + pulse.SafeName(group)
	}

	if opts.Reliability == pulse.Durable {
		if b.cfg.SubscriptionName != "" {
			return b.cfg.SubscriptionName
		}
		if opts.Group != "" {
			return "pulse-" + pulse.SafeName(subject) + "-" + pulse.SafeName(opts.Group)
		}
	}
	return "pulse-" + pulse.SafeName(subject) + "-" + ids.New()
}

func (b *Backend) subscriptionType(opts pulse.SubscribeOptions, rpc bool) pulsarclient.SubscriptionType {
	if rpc || opts.Delivery == pulse.WorkQueue {
		return pulsarclient.Shared
	}
	switch b.cfg.SubscriptionType {
	case SubscriptionTypeFailover:
		return pulsarclient.Failover
	case SubscriptionTypeShared:
		return pulsarclient.Shared
	case SubscriptionTypeKeyShared:
		return pulsarclient.KeyShared
	default:
		return pulsarclient.Exclusive
	}
}

func subscriptionMode(r pulse.Reliability) pulsarclient.SubscriptionMode {
	if r == pulse.Durable {
		return pulsarclient.Durable
	}
	return pulsarclient.NonDurable
}
func initialPosition(r pulse.Reliability) pulsarclient.SubscriptionInitialPosition {
	if r == pulse.Durable {
		return pulsarclient.SubscriptionPositionEarliest
	}
	return pulsarclient.SubscriptionPositionLatest
}

func propertiesFromEnvelope(env pulse.Envelope) map[string]string {
	props := map[string]string{"id": env.ID, "subject": env.Subject, "timestamp": env.Timestamp.Format(time.RFC3339Nano), "correlation_id": env.CorrelationID, "reply_to": env.ReplyTo}
	if encoded, err := pulse.EncodeHeader(env.Message.Header); err == nil && encoded != "" {
		props[pulse.EncodedHeaderKey] = encoded
	}
	return props
}

func envelopeFromMessage(m pulsarclient.Message) pulse.Envelope {
	props := m.Properties()
	h, _ := pulse.DecodeHeader(props[pulse.EncodedHeaderKey])
	ts := time.Now().UTC()
	if raw := props["timestamp"]; raw != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			ts = parsed
		}
	}
	return pulse.Envelope{ID: props["id"], Subject: first(props["subject"], m.Topic()), Message: pulse.Message{Header: h, Data: append([]byte(nil), m.Payload()...)}, Timestamp: ts, CorrelationID: props["correlation_id"], ReplyTo: props["reply_to"]}
}

type subscription struct {
	backend  *Backend
	consumer pulsarclient.Consumer
	closed   chan struct{}
	once     sync.Once
}

func (s *subscription) Close(ctx context.Context) error {
	s.once.Do(func() {
		close(s.closed)
		s.consumer.Close()
		s.backend.mu.Lock()
		delete(s.backend.subs, s)
		s.backend.mu.Unlock()
	})
	return ctx.Err()
}
func (s *subscription) receive(ctx context.Context, handler pulse.RequestHandler, rpc bool, reliability pulse.Reliability) {
	for {
		select {
		case <-s.closed:
			return
		default:
		}
		m, err := s.consumer.Receive(ctx)
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
		env := envelopeFromMessage(m)
		resp, err := handler(ctx, env.Message)
		if err != nil {
			s.consumer.Nack(m)
			continue
		}
		if rpc && env.ReplyTo != "" {
			pubCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err = s.backend.publish(pubCtx, pulse.Envelope{Subject: env.ReplyTo, Message: resp.Clone(), CorrelationID: env.CorrelationID}, pulse.PublishOptions{Reliability: reliability})
			cancel()
			if err != nil {
				s.consumer.Nack(m)
				continue
			}
		}
		s.consumer.Ack(m)
	}
}

func first(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

var _ pulse.Backend = (*Backend)(nil)
var _ pulse.Subscription = (*subscription)(nil)
