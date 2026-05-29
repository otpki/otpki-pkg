package natsjetstream

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	nats "github.com/nats-io/nats.go"
	"github.com/otpki/otpki-pkg/spfy/pulse"
	"github.com/otpki/otpki-pkg/spfy/pulse/backends/natscore"
	"github.com/otpki/otpki-pkg/spfy/pulse/internal/ids"
	"github.com/samber/oops"
)

type Backend struct {
	nc   *nats.Conn
	js   nats.JetStreamContext
	core *natscore.Backend
	cfg  Config

	closed atomic.Bool
	mu     sync.Mutex
	subs   map[*subscription]struct{}
	seen   map[string]struct{}
}

func New(nc *nats.Conn, cfg Config) (*Backend, error) {
	if nc == nil {
		return nil, oops.New("pulse natsjetstream: nil nats connection")
	}
	cfg = normalizeConfig(cfg)
	js, err := nc.JetStream()
	if err != nil {
		return nil, err
	}
	core, err := natscore.New(nc, natscore.Config{SubjectPrefix: cfg.SubjectPrefix, QueuePrefix: cfg.QueuePrefix, FlushTimeout: cfg.FlushTimeout, DrainTimeout: cfg.DrainTimeout})
	if err != nil {
		return nil, err
	}
	return &Backend{nc: nc, js: js, core: core, cfg: cfg, subs: make(map[*subscription]struct{}), seen: make(map[string]struct{})}, nil
}

func (b *Backend) Capabilities() pulse.Capabilities {
	return pulse.Capabilities{Fanout: true, WorkQueue: true, NativeRPC: true, DurablePubSub: true, DurableRPC: true}
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
	if err := b.core.Shutdown(ctx); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (b *Backend) Publish(ctx context.Context, subject string, msg pulse.Message, opts pulse.PublishOptions) error {
	if opts.Reliability != pulse.Durable {
		return b.core.Publish(ctx, subject, msg, opts)
	}
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
	resolved := b.subject(env.Subject)
	if err := b.ensureStream(resolved); err != nil {
		return err
	}
	nm := toNATSMsg(resolved, env)
	_, err := b.js.PublishMsg(nm, nats.Context(ctx))
	return err
}

func (b *Backend) Subscribe(ctx context.Context, subject string, handler pulse.Handler, opts pulse.SubscribeOptions) (pulse.Subscription, error) {
	if opts.Reliability != pulse.Durable {
		return b.core.Subscribe(ctx, subject, handler, opts)
	}
	if subject == "" || handler == nil {
		return nil, pulse.ErrInvalidName
	}
	if b.closed.Load() {
		return nil, pulse.ErrClosed
	}
	opts = pulse.NormalizeSubscribeOptions(subject, opts)
	resolved := b.subject(subject)
	if err := b.ensureStream(resolved); err != nil {
		return nil, err
	}
	cb := func(nm *nats.Msg) {
		err := handler(ctx, fromNATSMsg(nm).Message)
		if err == nil {
			_ = nm.Ack()
		} else {
			_ = nm.Nak()
		}
	}
	var ns *nats.Subscription
	var err error
	if opts.Delivery == pulse.WorkQueue {
		ns, err = b.js.QueueSubscribe(resolved, b.queue(opts.Group), cb, b.subOpts(subject, opts)...)
	} else {
		ns, err = b.js.Subscribe(resolved, cb, b.subOpts(subject, opts)...)
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
	if opts.Reliability != pulse.Durable {
		return b.core.RequestAsync(ctx, subject, req, opts)
	}
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
			ch <- pulse.Result[pulse.Message]{Err: b.publish(ctx, pulse.Envelope{Subject: subject, Message: req.Clone(), CorrelationID: correlationID}, pulse.PublishOptions{Reliability: pulse.Durable})}
		}()
		return pulse.NewFuture(ch), nil
	}
	reply := "_PULSE_REPLY." + ids.New()
	replies := make(chan pulse.Message, 1)
	sub, err := b.Subscribe(ctx, reply, func(ctx context.Context, msg pulse.Message) error {
		select {
		case replies <- msg.Clone():
		default:
		}
		return nil
	}, pulse.SubscribeOptions{Delivery: pulse.Fanout, Group: pulse.SafeName(reply), Reliability: pulse.Durable})
	if err != nil {
		return pulse.Future[pulse.Message]{}, err
	}
	if err := b.publish(ctx, pulse.Envelope{Subject: subject, Message: req.Clone(), CorrelationID: correlationID, ReplyTo: reply}, pulse.PublishOptions{Reliability: pulse.Durable}); err != nil {
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

func (b *Backend) Handle(ctx context.Context, subject string, handler pulse.RequestHandler, opts pulse.SubscribeOptions) (pulse.Subscription, error) {
	if opts.Reliability != pulse.Durable {
		return b.core.Handle(ctx, subject, handler, opts)
	}
	if subject == "" || handler == nil {
		return nil, pulse.ErrInvalidName
	}
	if b.closed.Load() {
		return nil, pulse.ErrClosed
	}
	opts.Delivery = pulse.WorkQueue
	opts = pulse.NormalizeSubscribeOptions(subject, opts)
	resolved := b.subject(subject)
	if err := b.ensureStream(resolved); err != nil {
		return nil, err
	}
	cb := func(nm *nats.Msg) {
		req := fromNATSMsg(nm)
		resp, err := handler(ctx, req.Message)
		if err != nil {
			_ = nm.Nak()
			return
		}
		if req.ReplyTo != "" {
			pubCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err = b.publish(pubCtx, pulse.Envelope{Subject: req.ReplyTo, Message: resp.Clone(), CorrelationID: req.CorrelationID}, pulse.PublishOptions{Reliability: pulse.Durable})
			cancel()
			if err != nil {
				_ = nm.Nak()
				return
			}
		}
		_ = nm.Ack()
	}
	ns, err := b.js.QueueSubscribe(resolved, b.queue(opts.Group), cb, b.subOpts(subject, opts)...)
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

func (b *Backend) ensureStream(subject string) error {
	key := b.cfg.StreamName + "|" + subject
	b.mu.Lock()
	if _, ok := b.seen[key]; ok {
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.seen[key]; ok {
		return nil
	}
	cfg := &nats.StreamConfig{Name: b.cfg.StreamName, Subjects: []string{subject}, Storage: nats.FileStorage}
	info, err := b.js.StreamInfo(b.cfg.StreamName)
	if err == nil {
		cfg.Subjects = merge(info.Config.Subjects, cfg.Subjects)
		cfg.Storage = info.Config.Storage
		cfg.Replicas = info.Config.Replicas
		_, err = b.js.UpdateStream(cfg)
	} else {
		_, err = b.js.AddStream(cfg)
		if err != nil {
			if info, infoErr := b.js.StreamInfo(b.cfg.StreamName); infoErr == nil {
				cfg.Subjects = merge(info.Config.Subjects, cfg.Subjects)
				cfg.Storage = info.Config.Storage
				cfg.Replicas = info.Config.Replicas
				_, err = b.js.UpdateStream(cfg)
			}
		}
	}
	if err != nil {
		return err
	}
	b.seen[key] = struct{}{}
	return nil
}

func merge(a, c []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(a)+len(c))
	for _, s := range append(append([]string{}, a...), c...) {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func (b *Backend) subOpts(subject string, opts pulse.SubscribeOptions) []nats.SubOpt {
	out := []nats.SubOpt{nats.ManualAck(), nats.AckWait(b.cfg.AckWait), nats.DeliverAll()}
	if opts.Group != "" {
		out = append(out, nats.Durable(b.durableName(subject, opts)))
	}
	return out
}

func (b *Backend) durableName(subject string, opts pulse.SubscribeOptions) string {
	prefix := "pulse"
	if opts.Delivery == pulse.WorkQueue {
		prefix = "pulse_wq"
	}
	return natsConsumerName(prefix + "_" + subject + "_" + opts.Group)
}

func natsConsumerName(s string) string {
	var out strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			out.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			out.WriteRune(r)
		case r >= '0' && r <= '9':
			out.WriteRune(r)
		case r == '-', r == '_':
			out.WriteRune(r)
		default:
			out.WriteByte('_')
		}
	}
	name := strings.Trim(out.String(), "_")
	if name == "" {
		name = "pulse"
	}
	if len(name) <= 120 {
		return name
	}
	sum := sha1.Sum([]byte(name))
	return name[:96] + "_" + hex.EncodeToString(sum[:8])
}

func (b *Backend) subject(subject string) string {
	if b.cfg.SubjectPrefix == "" || strings.HasPrefix(subject, "_INBOX") || strings.HasPrefix(subject, "_PULSE_REPLY") || strings.ContainsAny(subject, "*>") {
		return strings.ReplaceAll(subject, "/", ".")
	}
	return strings.Trim(b.cfg.SubjectPrefix, ".") + "." + strings.TrimPrefix(strings.ReplaceAll(subject, "/", "."), ".")
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
		until := time.Until(deadline)
		if until <= 0 {
			return ctx.Err()
		}
		if until < timeout {
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
		if !internal(k) {
			h[k] = append([]string(nil), vs...)
		}
	}
	ts := time.Now().UTC()
	if raw := nm.Header.Get("Pulse-Timestamp"); raw != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			ts = parsed
		}
	}
	return pulse.Envelope{ID: nm.Header.Get("Pulse-Id"), Subject: first(nm.Header.Get("Pulse-Subject"), nm.Subject), Message: pulse.Message{Header: h, Data: append([]byte(nil), nm.Data...)}, Timestamp: ts, CorrelationID: nm.Header.Get("Pulse-Correlation-Id"), ReplyTo: first(nm.Header.Get("Pulse-Reply-To"), nm.Reply)}
}
func internal(k string) bool {
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
