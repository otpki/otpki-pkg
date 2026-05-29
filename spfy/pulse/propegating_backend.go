package pulse

import "context"

type PropagationOption func(*propagationConfig)

type propagationConfig struct {
	registry         *ContextPropagatorRegistry
	additional       []ContextPropagator
	static           []ContextPropagator
	useStaticOnly    bool
	disablePropagate bool
}

// WithContextPropagatorRegistry makes the propagating backend read from a custom
// live registry instead of the global registry.
func WithContextPropagatorRegistry(registry *ContextPropagatorRegistry) PropagationOption {
	return func(c *propagationConfig) {
		if registry != nil {
			c.registry = registry
			c.useStaticOnly = false
		}
	}
}

// WithAdditionalPropagators appends fixed propagators after the live registry
// propagators. These do not disable dynamic global/custom registration.
func WithAdditionalPropagators(propagators ...ContextPropagator) PropagationOption {
	return func(c *propagationConfig) {
		c.additional = append(c.additional, cleanContextPropagators(propagators)...)
	}
}

// WithStaticPropagators disables the live registry for this backend and uses
// exactly this fixed list instead.
//
// Most services should not use this. Prefer the default live global registry.
func WithStaticPropagators(propagators ...ContextPropagator) PropagationOption {
	return func(c *propagationConfig) {
		c.static = cleanContextPropagators(propagators)
		c.useStaticOnly = true
	}
}

// WithoutContextPropagation disables propagation for this wrapper.
func WithoutContextPropagation() PropagationOption {
	return func(c *propagationConfig) {
		c.disablePropagate = true
	}
}

// NewPropagatingBackend wraps a backend so context values are propagated through
// message headers for Publish, Subscribe, Request, RequestAsync, and Handle.
//
// By default this uses the live global registry. Propagators registered after
// this backend is created are observed on subsequent sends/receives.
func NewPropagatingBackend(inner Backend, opts ...PropagationOption) Backend {
	if inner == nil {
		return nil
	}

	cfg := propagationConfig{
		registry: globalContextPropagators,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	return &propagatingBackend{
		inner:            inner,
		registry:         cfg.registry,
		additional:       cleanContextPropagators(cfg.additional),
		static:           cleanContextPropagators(cfg.static),
		useStaticOnly:    cfg.useStaticOnly,
		disablePropagate: cfg.disablePropagate,
	}
}

type propagatingBackend struct {
	inner Backend

	registry   *ContextPropagatorRegistry
	additional []ContextPropagator

	static        []ContextPropagator
	useStaticOnly bool

	disablePropagate bool
}

func (b *propagatingBackend) Publish(
	ctx context.Context,
	subject string,
	msg Message,
	opts PublishOptions,
) error {
	if b.disablePropagate {
		return b.inner.Publish(ctx, subject, msg, opts)
	}

	return b.inner.Publish(ctx, subject, b.outbound(ctx, msg), opts)
}

func (b *propagatingBackend) Subscribe(
	ctx context.Context,
	subject string,
	handler Handler,
	opts SubscribeOptions,
) (Subscription, error) {
	if b.disablePropagate {
		return b.inner.Subscribe(ctx, subject, handler, opts)
	}

	return b.inner.Subscribe(ctx, subject, func(handlerCtx context.Context, msg Message) error {
		handlerCtx = b.inbound(handlerCtx, msg)
		return handler(handlerCtx, msg)
	}, opts)
}

func (b *propagatingBackend) Request(
	ctx context.Context,
	subject string,
	req Message,
	opts RequestOptions,
) (Message, error) {
	if b.disablePropagate {
		return b.inner.Request(ctx, subject, req, opts)
	}

	return b.inner.Request(ctx, subject, b.outbound(ctx, req), opts)
}

func (b *propagatingBackend) RequestAsync(
	ctx context.Context,
	subject string,
	req Message,
	opts RequestOptions,
) (Future[Message], error) {
	if b.disablePropagate {
		return b.inner.RequestAsync(ctx, subject, req, opts)
	}

	return b.inner.RequestAsync(ctx, subject, b.outbound(ctx, req), opts)
}

func (b *propagatingBackend) Handle(
	ctx context.Context,
	subject string,
	handler RequestHandler,
	opts SubscribeOptions,
) (Subscription, error) {
	if b.disablePropagate {
		return b.inner.Handle(ctx, subject, handler, opts)
	}

	return b.inner.Handle(ctx, subject, func(handlerCtx context.Context, req Message) (Message, error) {
		handlerCtx = b.inbound(handlerCtx, req)
		return handler(handlerCtx, req)
	}, opts)
}

func (b *propagatingBackend) Shutdown(ctx context.Context) error {
	return b.inner.Shutdown(ctx)
}

func (b *propagatingBackend) Capabilities() Capabilities {
	return b.inner.Capabilities()
}

func (b *propagatingBackend) outbound(ctx context.Context, msg Message) Message {
	propagators := b.currentPropagators()
	props := extractContext(ctx, propagators)
	if len(props) == 0 {
		return msg
	}

	out := msg.Clone()
	if out.Header == nil {
		out.Header = Header{}
	}

	for k, v := range props {
		out.Header.Set(k, v)
	}

	return out
}

func (b *propagatingBackend) inbound(ctx context.Context, msg Message) context.Context {
	return injectContext(ctx, msg.Header, b.currentPropagators())
}

func (b *propagatingBackend) currentPropagators() []ContextPropagator {
	if b.useStaticOnly {
		return b.static
	}

	registryProps := b.registry.snapshot()
	if len(b.additional) == 0 {
		return registryProps
	}

	out := make([]ContextPropagator, 0, len(registryProps)+len(b.additional))
	out = append(out, registryProps...)
	out = append(out, b.additional...)
	return out
}
