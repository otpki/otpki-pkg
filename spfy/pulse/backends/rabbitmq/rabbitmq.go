package rabbitmq

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/otpki/otpki-pkg/spfy/pulse"
	"github.com/otpki/otpki-pkg/spfy/pulse/internal/ids"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/samber/oops"
)

type Backend struct {
	ch     *amqp.Channel
	cfg    Config
	closed atomic.Bool
	mu     sync.Mutex
	opMu   sync.Mutex
	subs   map[*subscription]struct{}
}

func New(ch *amqp.Channel, cfg Config) (*Backend, error) {
	if ch == nil {
		return nil, oops.New("pulse rabbitmq: nil channel")
	}
	cfg = normalizeConfig(cfg)
	if err := ch.Qos(cfg.Prefetch, 0, false); err != nil {
		return nil, err
	}
	return &Backend{ch: ch, cfg: cfg, subs: map[*subscription]struct{}{}}, nil
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
	return b.publish(ctx, pulse.Envelope{Subject: subject, Message: msg, ID: ids.New(), Timestamp: time.Now().UTC()}, opts.Reliability)
}
func (b *Backend) publish(ctx context.Context, env pulse.Envelope, r pulse.Reliability) error {
	if env.Subject == "" {
		return pulse.ErrInvalidName
	}
	if b.closed.Load() {
		return pulse.ErrClosed
	}
	durable := r == pulse.Durable
	exchange := b.exchange(env.Subject)
	pub, err := publishingFromEnvelope(env, durable)
	if err != nil {
		return err
	}
	b.opMu.Lock()
	defer b.opMu.Unlock()
	if err := b.ch.ExchangeDeclare(exchange, "fanout", durable, !durable, false, false, nil); err != nil {
		return err
	}
	return b.ch.PublishWithContext(ctx, exchange, "", false, false, pub)
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
	durable := opts.Reliability == pulse.Durable
	exchange := b.exchange(subject)
	b.opMu.Lock()
	if err := b.ch.ExchangeDeclare(exchange, "fanout", durable, !durable, false, false, nil); err != nil {
		b.opMu.Unlock()
		return nil, err
	}
	qname := b.queue(subject, opts, rpc)
	exclusive := qname == "" && !durable
	autoDelete := !durable
	q, err := b.ch.QueueDeclare(qname, durable, autoDelete, exclusive, false, nil)
	if err != nil {
		b.opMu.Unlock()
		return nil, err
	}
	if err := b.ch.QueueBind(q.Name, "", exchange, false, nil); err != nil {
		b.opMu.Unlock()
		return nil, err
	}
	tag := b.cfg.ConsumerTagPrefix + ids.New()
	deliveries, err := b.ch.Consume(q.Name, tag, false, exclusive, false, false, nil)
	b.opMu.Unlock()
	if err != nil {
		return nil, err
	}
	s := &subscription{backend: b, queue: q.Name, tag: tag, deliveries: deliveries, closed: make(chan struct{})}
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
	corr := ids.New()
	if opts.NoReply {
		go func() {
			defer close(ch)
			ch <- pulse.Result[pulse.Message]{Err: b.publish(ctx, pulse.Envelope{Subject: subject, Message: req.Clone(), CorrelationID: corr}, opts.Reliability)}
		}()
		return pulse.NewFuture(ch), nil
	}
	reply := b.cfg.ReplyQueuePrefix + pulse.SafeName(corr)
	replies := make(chan pulse.Message, 1)
	sub, err := b.Subscribe(ctx, reply, func(ctx context.Context, msg pulse.Message) error {
		select {
		case replies <- msg.Clone():
		default:
		}
		return nil
	}, pulse.SubscribeOptions{Delivery: pulse.Fanout, Group: corr, Reliability: opts.Reliability})
	if err != nil {
		return pulse.Future[pulse.Message]{}, err
	}
	if err := b.publish(ctx, pulse.Envelope{Subject: subject, Message: req.Clone(), CorrelationID: corr, ReplyTo: reply}, opts.Reliability); err != nil {
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
func (b *Backend) exchange(subject string) string {
	subject = strings.ReplaceAll(subject, "/", ".")
	if b.cfg.ExchangePrefix == "" {
		return subject
	}
	return strings.Trim(b.cfg.ExchangePrefix, ".") + "." + strings.TrimPrefix(subject, ".")
}
func (b *Backend) queue(subject string, opts pulse.SubscribeOptions, rpc bool) string {
	if opts.Delivery == pulse.Fanout && !rpc && opts.Reliability != pulse.Durable {
		return ""
	}
	name := pulse.SafeName(subject)
	if rpc {
		name = "rpc." + name
	} else if opts.Delivery == pulse.WorkQueue {
		name = "wq." + name + "." + pulse.SafeName(opts.Group)
	} else {
		name = "fanout." + name + "." + pulse.SafeName(opts.Group)
	}
	if b.cfg.QueuePrefix != "" {
		name = strings.Trim(b.cfg.QueuePrefix, ".") + "." + name
	}
	return name
}

type wireHeaders struct {
	Header        pulse.Header `json:"h,omitempty"`
	ID            string       `json:"id,omitempty"`
	Subject       string       `json:"subject,omitempty"`
	Timestamp     string       `json:"ts,omitempty"`
	CorrelationID string       `json:"cid,omitempty"`
	ReplyTo       string       `json:"reply,omitempty"`
}

func publishingFromEnvelope(env pulse.Envelope, durable bool) (amqp.Publishing, error) {
	h := wireHeaders{Header: env.Message.Header, ID: env.ID, Subject: env.Subject, Timestamp: env.Timestamp.Format(time.RFC3339Nano), CorrelationID: env.CorrelationID, ReplyTo: env.ReplyTo}
	b, err := json.Marshal(h)
	if err != nil {
		return amqp.Publishing{}, err
	}
	mode := uint8(amqp.Transient)
	if durable {
		mode = amqp.Persistent
	}
	return amqp.Publishing{Headers: amqp.Table{pulse.EncodedHeaderKey: string(b)}, DeliveryMode: mode, Timestamp: env.Timestamp, CorrelationId: env.CorrelationID, ReplyTo: env.ReplyTo, Body: append([]byte(nil), env.Message.Data...)}, nil
}
func envelopeFromDelivery(d amqp.Delivery) pulse.Envelope {
	var wh wireHeaders
	if raw, ok := d.Headers[pulse.EncodedHeaderKey].(string); ok {
		_ = json.Unmarshal([]byte(raw), &wh)
	}
	ts := d.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	return pulse.Envelope{ID: wh.ID, Subject: first(wh.Subject, d.Exchange), Message: pulse.Message{Header: wh.Header, Data: append([]byte(nil), d.Body...)}, Timestamp: ts, CorrelationID: first(wh.CorrelationID, d.CorrelationId), ReplyTo: first(wh.ReplyTo, d.ReplyTo)}
}

type subscription struct {
	backend    *Backend
	queue      string
	tag        string
	deliveries <-chan amqp.Delivery
	closed     chan struct{}
	once       sync.Once
}

func (s *subscription) Close(ctx context.Context) error {
	var err error
	s.once.Do(func() {
		close(s.closed)
		s.backend.opMu.Lock()
		err = s.backend.ch.Cancel(s.tag, false)
		s.backend.opMu.Unlock()
		s.backend.mu.Lock()
		delete(s.backend.subs, s)
		s.backend.mu.Unlock()
	})
	if err != nil {
		return err
	}
	return ctx.Err()
}
func (s *subscription) receive(ctx context.Context, handler pulse.RequestHandler, rpc bool, r pulse.Reliability) {
	for {
		select {
		case <-s.closed:
			return
		case <-ctx.Done():
			return
		case d, ok := <-s.deliveries:
			if !ok {
				return
			}
			env := envelopeFromDelivery(d)
			resp, err := handler(ctx, env.Message)
			if err == nil && rpc && env.ReplyTo != "" {
				pubCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err = s.backend.publish(pubCtx, pulse.Envelope{Subject: env.ReplyTo, Message: resp.Clone(), CorrelationID: env.CorrelationID}, r)
				cancel()
			}
			s.backend.opMu.Lock()
			if err == nil {
				_ = d.Ack(false)
			} else {
				_ = d.Nack(false, true)
			}
			s.backend.opMu.Unlock()
		}
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
