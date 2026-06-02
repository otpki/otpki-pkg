package oopsie

import (
	"context"
	"sync"

	"github.com/samber/oops"
)

// ContextExtractor enriches an oops builder from context. Extractors should add
// metadata such as trace IDs, user IDs, or operation names, and should avoid
// overwriting explicit builder configuration like code or public message.
type ContextExtractor func(ctx context.Context, builder oops.OopsErrorBuilder) oops.OopsErrorBuilder

var contextExtractors struct {
	mu  sync.RWMutex
	fns []ContextExtractor
}

// RegisterContextExtractor registers an optional context enricher used by the
// oopsie wrapper API. By default, no context enrichment is performed.
func RegisterContextExtractor(extractor ContextExtractor) {
	if extractor == nil {
		return
	}

	contextExtractors.mu.Lock()
	defer contextExtractors.mu.Unlock()
	contextExtractors.fns = append(contextExtractors.fns, extractor)
}

type Builder struct {
	oops.OopsErrorBuilder
}

type BoundBuilder struct {
	oops.OopsErrorBuilder
}

func Base() BoundBuilder {
	return wrapBound(oops.FromContext(context.Background()))
}

func From(ctx context.Context) BoundBuilder {
	return wrapBound(applyContext(ctx, oops.FromContext(ctx)))
}

func FromContext(ctx context.Context) BoundBuilder {
	return From(ctx)
}

func Code(code any) Builder {
	return wrapBuilder(oops.Code(code))
}

func In(domain string) Builder {
	return wrapBuilder(oops.In(domain))
}

func Public(message string) Builder {
	return wrapBuilder(oops.Public(message))
}

func Trace(trace string) Builder {
	return wrapBuilder(oops.Trace(trace))
}

func Span(span string) Builder {
	return wrapBuilder(oops.Span(span))
}

func Tags(tags ...string) Builder {
	return wrapBuilder(oops.Tags(tags...))
}

func With(kv ...any) Builder {
	return wrapBuilder(oops.With(kv...))
}

func WithContext(ctx context.Context, keys ...any) BoundBuilder {
	return wrapBound(oops.WithContext(ctx, keys...))
}

func GetPublic(err error, defaultPublicMessage string) string {
	return oops.GetPublic(err, defaultPublicMessage)
}

func WithBuilder(ctx context.Context, builder Builder) context.Context {
	return oops.WithBuilder(ctx, builder.OopsErrorBuilder)
}

func Wrap(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	return From(ctx).Wrap(err)
}

func Wrapf(ctx context.Context, err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	return From(ctx).Wrapf(err, format, args...)
}

func Errorf(ctx context.Context, format string, args ...any) error {
	return From(ctx).Errorf(format, args...)
}

func New(ctx context.Context, message string) error {
	return From(ctx).New(message)
}

func (b Builder) Base() BoundBuilder {
	return wrapBound(b.OopsErrorBuilder)
}

func (b Builder) From(ctx context.Context) BoundBuilder {
	return wrapBound(applyContext(ctx, b.OopsErrorBuilder))
}

func (b Builder) WithContext(ctx context.Context) BoundBuilder {
	return b.From(ctx)
}

func (b Builder) Oops() oops.OopsErrorBuilder {
	return b.OopsErrorBuilder
}

func (b Builder) Code(code any) Builder {
	return wrapBuilder(b.OopsErrorBuilder.Code(code))
}

func (b Builder) In(domain string) Builder {
	return wrapBuilder(b.OopsErrorBuilder.In(domain))
}

func (b Builder) Public(message string) Builder {
	return wrapBuilder(b.OopsErrorBuilder.Public(message))
}

func (b Builder) Trace(trace string) Builder {
	return wrapBuilder(b.OopsErrorBuilder.Trace(trace))
}

func (b Builder) Span(span string) Builder {
	return wrapBuilder(b.OopsErrorBuilder.Span(span))
}

func (b Builder) Tags(tags ...string) Builder {
	return wrapBuilder(b.OopsErrorBuilder.Tags(tags...))
}

func (b Builder) With(kv ...any) Builder {
	return wrapBuilder(b.OopsErrorBuilder.With(kv...))
}

func (b Builder) Wrap(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	return b.From(ctx).Wrap(err)
}

func (b Builder) Wrapf(ctx context.Context, err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	return b.From(ctx).Wrapf(err, format, args...)
}

func (b Builder) Errorf(ctx context.Context, format string, args ...any) error {
	return b.From(ctx).Errorf(format, args...)
}

func (b Builder) New(ctx context.Context, message string) error {
	return b.From(ctx).New(message)
}

func wrapBuilder(builder oops.OopsErrorBuilder) Builder {
	return Builder{OopsErrorBuilder: builder}
}

func wrapBound(builder oops.OopsErrorBuilder) BoundBuilder {
	return BoundBuilder{OopsErrorBuilder: builder}
}

func applyContext(ctx context.Context, builder oops.OopsErrorBuilder) oops.OopsErrorBuilder {
	contextExtractors.mu.RLock()
	defer contextExtractors.mu.RUnlock()

	for _, extractor := range contextExtractors.fns {
		builder = extractor(ctx, builder)
	}

	return builder
}
