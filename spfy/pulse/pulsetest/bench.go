package pulsetest

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/otpki/otpki-pkg/spfy/pulse"
)

func warmupRequestReply(tb testing.TB, backend pulse.Backend, subject string, payload pulse.Message) {
	tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := backend.Request(ctx, subject, payload, pulse.RequestOptions{Timeout: 30 * time.Second}); err != nil {
		tb.Fatalf("warmup request/reply failed: %v", err)
	}
}

func warmupFireAndForget(tb testing.TB, backend pulse.Backend, subject string, payload pulse.Message) {
	tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	future, err := backend.RequestAsync(ctx, subject, payload, pulse.RequestOptions{NoReply: true, Timeout: 30 * time.Second})
	if err != nil {
		tb.Fatalf("warmup fire-and-forget failed: %v", err)
	}
	if _, err := future.Await(ctx); err != nil {
		tb.Fatalf("warmup fire-and-forget await failed: %v", err)
	}
}

func warmupPublish(tb testing.TB, backend pulse.Backend, subject string, payload pulse.Message) {
	tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := backend.Publish(ctx, subject, payload, pulse.PublishOptions{}); err != nil {
		tb.Fatalf("warmup publish failed: %v", err)
	}
}

func BenchmarkRequestReply(b *testing.B, f Factory) {
	ctx := context.Background()
	backend := f.New(b)
	defer func(backend pulse.Backend, ctx context.Context) {
		_ = backend.Shutdown(ctx)
	}(backend, context.Background())
	subj := subject(b, "bench-rpc")
	_, err := backend.Handle(ctx, subj, func(ctx context.Context, msg pulse.Message) (pulse.Message, error) { return msg, nil }, pulse.SubscribeOptions{})
	if err != nil {
		b.Fatal(err)
	}
	afterSubscribe(b, f)
	payload := pulse.Message{Data: []byte("payload")}
	warmupRequestReply(b, backend, subj, payload)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := backend.Request(ctx, subj, payload, pulse.RequestOptions{Timeout: 5 * time.Second}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRequestAsync(b *testing.B, f Factory) {
	ctx := context.Background()
	backend := f.New(b)
	defer func(backend pulse.Backend, ctx context.Context) {
		_ = backend.Shutdown(ctx)
	}(backend, context.Background())
	subj := subject(b, "bench-async")
	_, err := backend.Handle(ctx, subj, func(ctx context.Context, msg pulse.Message) (pulse.Message, error) { return msg, nil }, pulse.SubscribeOptions{})
	if err != nil {
		b.Fatal(err)
	}
	afterSubscribe(b, f)
	payload := pulse.Message{Data: []byte("payload")}
	warmupRequestReply(b, backend, subj, payload)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		future, err := backend.RequestAsync(ctx, subj, payload, pulse.RequestOptions{Timeout: 5 * time.Second})
		if err != nil {
			b.Fatal(err)
		}
		if _, err := future.Await(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFireAndForget(b *testing.B, f Factory) {
	ctx := context.Background()
	backend := f.New(b)
	defer func(backend pulse.Backend, ctx context.Context) {
		_ = backend.Shutdown(ctx)
	}(backend, context.Background())
	subj := subject(b, "bench-fire")
	_, err := backend.Handle(ctx, subj, func(ctx context.Context, msg pulse.Message) (pulse.Message, error) { return pulse.Message{}, nil }, pulse.SubscribeOptions{})
	if err != nil {
		b.Fatal(err)
	}
	afterSubscribe(b, f)
	payload := pulse.Message{Data: []byte("payload")}
	warmupFireAndForget(b, backend, subj, payload)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		future, err := backend.RequestAsync(ctx, subj, payload, pulse.RequestOptions{NoReply: true, Timeout: 5 * time.Second})
		if err != nil {
			b.Fatal(err)
		}
		if _, err := future.Await(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPublishFanout(b *testing.B, f Factory) {
	ctx := context.Background()
	backend := f.New(b)
	defer func(backend pulse.Backend, ctx context.Context) {
		_ = backend.Shutdown(ctx)
	}(backend, context.Background())
	subj := subject(b, "bench-pub")
	_, err := backend.Subscribe(ctx, subj, func(context.Context, pulse.Message) error { return nil }, pulse.SubscribeOptions{Delivery: pulse.Fanout})
	if err != nil {
		b.Fatal(err)
	}
	afterSubscribe(b, f)
	payload := pulse.Message{Data: []byte("payload")}
	warmupPublish(b, backend, subj, payload)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := backend.Publish(ctx, subj, payload, pulse.PublishOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPublishWorkQueue(b *testing.B, f Factory) {
	ctx := context.Background()
	backend := f.New(b)
	defer func(backend pulse.Backend, ctx context.Context) {
		_ = backend.Shutdown(ctx)
	}(backend, context.Background())
	subj := subject(b, "bench-workqueue")
	_, err := backend.Subscribe(ctx, subj, func(context.Context, pulse.Message) error { return nil }, pulse.SubscribeOptions{Delivery: pulse.WorkQueue, Group: "bench-workers"})
	if err != nil {
		b.Fatal(err)
	}
	afterSubscribe(b, f)
	payload := pulse.Message{Data: []byte("payload")}
	warmupPublish(b, backend, subj, payload)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := backend.Publish(ctx, subj, payload, pulse.PublishOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRequestReplyParallel(b *testing.B, f Factory) {
	ctx := context.Background()
	backend := f.New(b)
	defer func(backend pulse.Backend, ctx context.Context) {
		_ = backend.Shutdown(ctx)
	}(backend, context.Background())
	subj := subject(b, "bench-rpc-parallel")
	_, err := backend.Handle(ctx, subj, func(ctx context.Context, msg pulse.Message) (pulse.Message, error) { return msg, nil }, pulse.SubscribeOptions{})
	if err != nil {
		b.Fatal(err)
	}
	afterSubscribe(b, f)
	payload := pulse.Message{Data: []byte("payload")}
	warmupRequestReply(b, backend, subj, payload)
	var once sync.Once
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := backend.Request(ctx, subj, payload, pulse.RequestOptions{Timeout: 5 * time.Second}); err != nil {
				once.Do(func() { b.Error(err) })
				return
			}
		}
	})
}
