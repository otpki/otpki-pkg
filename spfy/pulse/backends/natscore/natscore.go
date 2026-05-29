package natscore

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	nats "github.com/nats-io/nats.go"
	"github.com/otpki/otpki-pkg/spfy/pulse"
	"github.com/otpki/otpki-pkg/spfy/pulse/internal/ids"
	"github.com/samber/oops"
)

type Backend struct {
	nc     *nats.Conn
	cfg    Config
	closed atomic.Bool

	mu   sync.Mutex
	subs map[*subscription]struct{}
}

func New(nc *nats.Conn, cfg Config) (*Backend, error) {
	if nc == nil {
		return nil, oops.New("pulse natscore: nil nats connection")
	}
	return &Backend{nc: nc, cfg: normalizeConfig(cfg), subs: make(map[*subscription]struct{})}, nil
}

func (b *Backend) Capabilities() pulse.Capabilities {
	return pulse.Capabilities{Fanout: true, WorkQueue: true, NativeRPC: true}
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
	b.subs = make(map[*subscription]struct{})
	b.mu.Unlock()

	var errs []error
	for _, s := range subs {
		if err := s.Close(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (b *Backend) Publish(ctx context.Context, subject string, msg pulse.Message, opts pulse.PublishOptions) error {
	if subject == "" {
		return pulse.ErrInvalidName
	}
	if opts.Reliability == pulse.Durable {
		return pulse.ErrUnsupported
	}
	if b.closed.Load() {
		return pulse.ErrClosed
	}
	nm := toNATSMsg(b.subject(subject), pulse.Envelope{Subject: subject, Message: msg.Clone(), ID: ids.New(), Timestamp: time.Now().UTC()})
	if err := b.nc.PublishMsg(nm); err != nil {
		return err
	}
	return b.flush(ctx)
}

func (b *Backend) Subscribe(ctx context.Context, subject string, handler pulse.Handler, opts pulse.SubscribeOptions) (pulse.Subscription, error) {
	if subject == "" || handler == nil {
		return nil, pulse.ErrInvalidName
	}
	if opts.Reliability == pulse.Durable {
		return nil, pulse.ErrUnsupported
	}
	if b.closed.Load() {
		return nil, pulse.ErrClosed
	}
	opts = pulse.NormalizeSubscribeOptions(subject, opts)
	cb := func(nm *nats.Msg) { _ = handler(ctx, fromNATSMsg(nm).Message) }
	var ns *nats.Subscription
	var err error
	if opts.Delivery == pulse.WorkQueue {
		ns, err = b.nc.QueueSubscribe(b.subject(subject), b.queue(opts.Group), cb)
	} else {
		ns, err = b.nc.Subscribe(b.subject(subject), cb)
	}
	if err != nil {
		return nil, err
	}
	if err := b.flush(ctx); err != nil {
		_ = ns.Unsubscribe()
		return nil, err
	}
	s := &subscription{backend: b, sub: ns}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
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
	if opts.Reliability == pulse.Durable {
		return pulse.Future[pulse.Message]{}, pulse.ErrUnsupported
	}
	if b.closed.Load() {
		return pulse.Future[pulse.Message]{}, pulse.ErrClosed
	}

	ch := make(chan pulse.Result[pulse.Message], 1)
	if opts.NoReply {
		go func() {
			defer close(ch)
			ch <- pulse.Result[pulse.Message]{Err: b.Publish(ctx, subject, req, pulse.PublishOptions{})}
		}()
		return pulse.NewFuture(ch), nil
	}

	env := pulse.Envelope{Subject: subject, Message: req.Clone(), ID: ids.New(), Timestamp: time.Now().UTC(), CorrelationID: ids.New()}
	nm := toNATSMsg(b.subject(subject), env)
	go func() {
		defer close(ch)
		callCtx := ctx
		var cancel context.CancelFunc
		if _, ok := callCtx.Deadline(); !ok && opts.Timeout > 0 {
			callCtx, cancel = context.WithTimeout(callCtx, opts.Timeout)
		}
		if cancel != nil {
			defer cancel()
		}
		resp, err := b.nc.RequestMsgWithContext(callCtx, nm)
		if err != nil {
			ch <- pulse.Result[pulse.Message]{Err: err}
			return
		}
		out := fromNATSMsg(resp)
		if out.CorrelationID != "" && out.CorrelationID != env.CorrelationID {
			ch <- pulse.Result[pulse.Message]{Err: oops.New("pulse natscore: mismatched correlation id")}
			return
		}
		ch <- pulse.Result[pulse.Message]{Value: out.Message}
	}()
	return pulse.NewFuture(ch), nil
}

func (b *Backend) Handle(ctx context.Context, subject string, handler pulse.RequestHandler, opts pulse.SubscribeOptions) (pulse.Subscription, error) {
	if subject == "" || handler == nil {
		return nil, pulse.ErrInvalidName
	}
	if opts.Reliability == pulse.Durable {
		return nil, pulse.ErrUnsupported
	}
	if b.closed.Load() {
		return nil, pulse.ErrClosed
	}
	opts.Delivery = pulse.WorkQueue
	opts = pulse.NormalizeSubscribeOptions(subject, opts)
	cb := func(nm *nats.Msg) {
		req := fromNATSMsg(nm)
		resp, err := handler(ctx, req.Message)
		if err != nil || nm.Reply == "" {
			return
		}
		out := toNATSMsg(nm.Reply, pulse.Envelope{Subject: nm.Reply, Message: resp.Clone(), CorrelationID: req.CorrelationID, ID: ids.New(), Timestamp: time.Now().UTC()})
		_ = nm.RespondMsg(out)
	}
	ns, err := b.nc.QueueSubscribe(b.subject(subject), b.queue(opts.Group), cb)
	if err != nil {
		return nil, err
	}
	if err := b.flush(ctx); err != nil {
		_ = ns.Unsubscribe()
		return nil, err
	}
	s := &subscription{backend: b, sub: ns}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s, nil
}

func (b *Backend) subject(subject string) string {
	if b.cfg.SubjectPrefix == "" || strings.HasPrefix(subject, "_INBOX") || strings.ContainsAny(subject, "*>") {
		return subject
	}
	prefix := strings.Trim(b.cfg.SubjectPrefix, ".")
	if prefix == "" {
		return subject
	}
	return prefix + "." + strings.TrimPrefix(strings.ReplaceAll(subject, "/", "."), ".")
}

func (b *Backend) queue(group string) string {
	group = pulse.SafeName(group)
	if b.cfg.QueuePrefix == "" {
		return group
	}
	return strings.Trim(b.cfg.QueuePrefix, ".") + "." + group
}

func (b *Backend) flush(ctx context.Context) error {
	timeout := b.cfg.FlushTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if until := time.Until(deadline); until <= 0 {
			return ctx.Err()
		} else if until < timeout {
			timeout = until
		}
	}
	return b.nc.FlushTimeout(timeout)
}

type subscription struct {
	backend *Backend
	sub     *nats.Subscription
	once    sync.Once
}

func (s *subscription) Close(ctx context.Context) error {
	var err error
	s.once.Do(func() {
		done := make(chan error, 1)
		go func() { done <- s.sub.Drain() }()
		select {
		case err = <-done:
		case <-ctx.Done():
			_ = s.sub.Unsubscribe()
			err = ctx.Err()
		case <-time.After(s.backend.cfg.DrainTimeout):
			_ = s.sub.Unsubscribe()
		}
		s.backend.mu.Lock()
		delete(s.backend.subs, s)
		s.backend.mu.Unlock()
	})
	return err
}

func toNATSMsg(subject string, env pulse.Envelope) *nats.Msg {
	nm := &nats.Msg{Subject: subject, Reply: env.ReplyTo, Data: append([]byte(nil), env.Message.Data...), Header: nats.Header{}}
	for k, vs := range env.Message.Header {
		for _, v := range vs {
			nm.Header.Add(k, v)
		}
	}
	if env.ID != "" {
		nm.Header.Set("Pulse-Id", env.ID)
	}
	if env.Subject != "" {
		nm.Header.Set("Pulse-Subject", env.Subject)
	}
	if !env.Timestamp.IsZero() {
		nm.Header.Set("Pulse-Timestamp", env.Timestamp.Format(time.RFC3339Nano))
	}
	if env.CorrelationID != "" {
		nm.Header.Set("Pulse-Correlation-Id", env.CorrelationID)
	}
	if env.ReplyTo != "" {
		nm.Header.Set("Pulse-Reply-To", env.ReplyTo)
	}
	return nm
}

func fromNATSMsg(nm *nats.Msg) pulse.Envelope {
	h := pulse.Header{}
	for k, vs := range nm.Header {
		if isInternalHeader(k) {
			continue
		}
		h[k] = append([]string(nil), vs...)
	}
	ts := time.Now().UTC()
	if raw := nm.Header.Get("Pulse-Timestamp"); raw != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			ts = parsed
		}
	}
	return pulse.Envelope{ID: nm.Header.Get("Pulse-Id"), Subject: first(nm.Header.Get("Pulse-Subject"), nm.Subject), Message: pulse.Message{Header: h, Data: append([]byte(nil), nm.Data...)}, Timestamp: ts, CorrelationID: nm.Header.Get("Pulse-Correlation-Id"), ReplyTo: first(nm.Header.Get("Pulse-Reply-To"), nm.Reply)}
}

func isInternalHeader(k string) bool {
	switch strings.ToLower(k) {
	case "pulse-id", "pulse-subject", "pulse-timestamp", "pulse-correlation-id", "pulse-reply-to":
		return true
	default:
		return false
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
