package memory

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/otpki/otpki-pkg/spfy/pulse"
)

type Config struct {
	Buffer int `mapstructure:"buffer"`
}

func DefaultConfig() Config { return Config{Buffer: 1024} }

type Backend struct {
	cfg    Config
	closed atomic.Bool

	mu       sync.Mutex
	fanout   map[string]map[*sub]struct{}
	groups   map[string]map[string][]*sub
	groupRR  map[string]int
	rpc      map[string]map[string][]*rpcSub
	rpcRR    map[string]int
	closeAll []closer
}

type closer interface{ close() }

func New(cfg Config) *Backend {
	if cfg.Buffer <= 0 {
		cfg.Buffer = DefaultConfig().Buffer
	}
	return &Backend{
		cfg:     cfg,
		fanout:  make(map[string]map[*sub]struct{}),
		groups:  make(map[string]map[string][]*sub),
		groupRR: make(map[string]int),
		rpc:     make(map[string]map[string][]*rpcSub),
		rpcRR:   make(map[string]int),
	}
}

func (b *Backend) Capabilities() pulse.Capabilities {
	return pulse.Capabilities{Fanout: true, WorkQueue: true, NativeRPC: true}
}

func (b *Backend) Shutdown(ctx context.Context) error {
	if b.closed.Swap(true) {
		return nil
	}
	b.mu.Lock()
	items := append([]closer(nil), b.closeAll...)
	b.closeAll = nil
	b.fanout = make(map[string]map[*sub]struct{})
	b.groups = make(map[string]map[string][]*sub)
	b.rpc = make(map[string]map[string][]*rpcSub)
	b.mu.Unlock()
	for _, c := range items {
		c.close()
	}
	return ctx.Err()
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

	b.mu.Lock()
	fanoutSubs := make([]*sub, 0, len(b.fanout[subject]))
	for s := range b.fanout[subject] {
		fanoutSubs = append(fanoutSubs, s)
	}
	var groupSubs []*sub
	for group, subs := range b.groups[subject] {
		if len(subs) == 0 {
			continue
		}
		key := subject + "\x00" + group
		idx := b.groupRR[key] % len(subs)
		b.groupRR[key]++
		groupSubs = append(groupSubs, subs[idx])
	}
	b.mu.Unlock()

	for _, s := range fanoutSubs {
		if err := s.enqueue(ctx, msg.Clone()); err != nil {
			return err
		}
	}
	for _, s := range groupSubs {
		if err := s.enqueue(ctx, msg.Clone()); err != nil {
			return err
		}
	}
	return nil
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

	s := &sub{
		backend: b,
		subject: subject,
		group:   opts.Group,
		mode:    opts.Delivery,
		handler: handler,
		ctx:     ctx,
		ch:      make(chan pulse.Message, b.cfg.Buffer),
		done:    make(chan struct{}),
	}

	b.mu.Lock()
	if opts.Delivery == pulse.WorkQueue {
		if b.groups[subject] == nil {
			b.groups[subject] = make(map[string][]*sub)
		}
		b.groups[subject][opts.Group] = append(b.groups[subject][opts.Group], s)
	} else {
		if b.fanout[subject] == nil {
			b.fanout[subject] = make(map[*sub]struct{})
		}
		b.fanout[subject][s] = struct{}{}
	}
	b.closeAll = append(b.closeAll, s)
	b.mu.Unlock()

	go s.run()
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
	resultCh := make(chan pulse.Result[pulse.Message], 1)

	b.mu.Lock()
	groups := b.rpc[subject]
	var candidates []*rpcSub
	if len(groups) > 0 {
		// RPC intentionally load balances to one handler. If multiple groups exist,
		// pick the default group first; otherwise pick the first non-empty group.
		if subs := groups[pulse.DefaultGroup(subject)]; len(subs) > 0 {
			candidates = subs
		} else {
			for _, subs := range groups {
				if len(subs) > 0 {
					candidates = subs
					break
				}
			}
		}
	}
	if len(candidates) == 0 {
		b.mu.Unlock()
		resultCh <- pulse.Result[pulse.Message]{Err: pulse.ErrNoHandler}
		close(resultCh)
		return pulse.NewFuture(resultCh), nil
	}
	idx := b.rpcRR[subject] % len(candidates)
	b.rpcRR[subject]++
	h := candidates[idx]
	b.mu.Unlock()

	callCtx := ctx
	var cancel context.CancelFunc
	if _, ok := callCtx.Deadline(); !ok && opts.Timeout > 0 {
		callCtx, cancel = context.WithTimeout(callCtx, opts.Timeout)
	}

	if opts.NoReply {
		select {
		case h.ch <- rpcReq{ctx: ctx, msg: req.Clone()}:
			if cancel != nil {
				cancel()
			}
			resultCh <- pulse.Result[pulse.Message]{}
			close(resultCh)
			return pulse.NewFuture(resultCh), nil
		case <-ctx.Done():
			if cancel != nil {
				cancel()
			}
			resultCh <- pulse.Result[pulse.Message]{Err: ctx.Err()}
			close(resultCh)
			return pulse.NewFuture(resultCh), nil
		case <-h.done:
			if cancel != nil {
				cancel()
			}
			resultCh <- pulse.Result[pulse.Message]{Err: pulse.ErrClosed}
			close(resultCh)
			return pulse.NewFuture(resultCh), nil
		}
	}

	reply := make(chan pulse.Result[pulse.Message], 1)
	select {
	case h.ch <- rpcReq{ctx: callCtx, msg: req.Clone(), reply: reply}:
	case <-ctx.Done():
		if cancel != nil {
			cancel()
		}
		resultCh <- pulse.Result[pulse.Message]{Err: ctx.Err()}
		close(resultCh)
		return pulse.NewFuture(resultCh), nil
	case <-h.done:
		if cancel != nil {
			cancel()
		}
		resultCh <- pulse.Result[pulse.Message]{Err: pulse.ErrClosed}
		close(resultCh)
		return pulse.NewFuture(resultCh), nil
	}

	go func() {
		defer close(resultCh)
		if cancel != nil {
			defer cancel()
		}
		select {
		case res := <-reply:
			resultCh <- res
		case <-callCtx.Done():
			resultCh <- pulse.Result[pulse.Message]{Err: callCtx.Err()}
		}
	}()

	return pulse.NewFuture(resultCh), nil
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

	s := &rpcSub{
		backend: b,
		subject: subject,
		group:   opts.Group,
		handler: handler,
		ctx:     ctx,
		ch:      make(chan rpcReq, b.cfg.Buffer),
		done:    make(chan struct{}),
	}

	b.mu.Lock()
	if b.rpc[subject] == nil {
		b.rpc[subject] = make(map[string][]*rpcSub)
	}
	b.rpc[subject][opts.Group] = append(b.rpc[subject][opts.Group], s)
	b.closeAll = append(b.closeAll, s)
	b.mu.Unlock()

	go s.run()
	return s, nil
}

type sub struct {
	backend *Backend
	subject string
	group   string
	mode    pulse.Delivery
	handler pulse.Handler
	ctx     context.Context
	ch      chan pulse.Message
	done    chan struct{}
	once    sync.Once
}

func (s *sub) enqueue(ctx context.Context, msg pulse.Message) error {
	select {
	case s.ch <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return pulse.ErrClosed
	}
}

func (s *sub) run() {
	for {
		select {
		case msg := <-s.ch:
			_ = s.handler(s.ctx, msg)
		case <-s.ctx.Done():
			s.close()
			return
		case <-s.done:
			return
		}
	}
}

func (s *sub) Close(ctx context.Context) error { s.close(); return ctx.Err() }
func (s *sub) close() {
	s.once.Do(func() {
		close(s.done)
		s.backend.mu.Lock()
		defer s.backend.mu.Unlock()
		if s.mode == pulse.WorkQueue {
			removeSub(s.backend.groups[s.subject], s.group, s)
		} else if subs := s.backend.fanout[s.subject]; subs != nil {
			delete(subs, s)
		}
	})
}

func removeSub(m map[string][]*sub, group string, target *sub) {
	if m == nil {
		return
	}
	subs := m[group]
	for i, s := range subs {
		if s == target {
			copy(subs[i:], subs[i+1:])
			subs = subs[:len(subs)-1]
			break
		}
	}
	if len(subs) == 0 {
		delete(m, group)
		return
	}
	m[group] = subs
}

type rpcReq struct {
	ctx   context.Context
	msg   pulse.Message
	reply chan pulse.Result[pulse.Message]
}

type rpcSub struct {
	backend *Backend
	subject string
	group   string
	handler pulse.RequestHandler
	ctx     context.Context
	ch      chan rpcReq
	done    chan struct{}
	once    sync.Once
}

func (s *rpcSub) run() {
	for {
		select {
		case req := <-s.ch:
			ctx := req.ctx
			if ctx == nil {
				ctx = s.ctx
			}
			if req.reply == nil {
				_, _ = s.handler(ctx, req.msg)
				continue
			}
			msg, err := s.handler(ctx, req.msg)
			select {
			case req.reply <- pulse.Result[pulse.Message]{Value: msg.Clone(), Err: err}:
			case <-ctx.Done():
			}
		case <-s.ctx.Done():
			s.close()
			return
		case <-s.done:
			return
		}
	}
}

func (s *rpcSub) Close(ctx context.Context) error { s.close(); return ctx.Err() }
func (s *rpcSub) close() {
	s.once.Do(func() {
		close(s.done)
		s.backend.mu.Lock()
		defer s.backend.mu.Unlock()
		groups := s.backend.rpc[s.subject]
		if groups == nil {
			return
		}
		subs := groups[s.group]
		for i, item := range subs {
			if item == s {
				copy(subs[i:], subs[i+1:])
				subs = subs[:len(subs)-1]
				break
			}
		}
		if len(subs) == 0 {
			delete(groups, s.group)
		} else {
			groups[s.group] = subs
		}
	})
}

var _ pulse.Backend = (*Backend)(nil)
var _ pulse.Subscription = (*sub)(nil)
var _ pulse.Subscription = (*rpcSub)(nil)

func sleepOrDone(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
