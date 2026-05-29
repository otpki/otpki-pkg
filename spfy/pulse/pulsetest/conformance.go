package pulsetest

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otpki/otpki-pkg/spfy/pulse"
)

type Factory struct {
	Name string
	New  func(testing.TB) pulse.Backend

	// AfterSubscribe is an optional test hook for backends whose client
	// libraries return before the broker has fully activated a subscription.
	// Production backends should still make Subscribe as synchronous as their
	// broker permits; this hook keeps conformance tests deterministic for
	// brokers such as Kafka where group assignment is asynchronous.
	AfterSubscribe func(testing.TB)
}

func RunConformance(t *testing.T, f Factory) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping conformance test in short mode")
	}
	t.Run(f.Name+"/fanout", func(t *testing.T) { testFanout(t, f) })
	t.Run(f.Name+"/work_queue", func(t *testing.T) { testWorkQueue(t, f) })
	t.Run(f.Name+"/request_reply", func(t *testing.T) { testRequestReply(t, f) })
	t.Run(f.Name+"/request_async", func(t *testing.T) { testRequestAsync(t, f) })
	t.Run(f.Name+"/fire_and_forget", func(t *testing.T) { testFireAndForget(t, f) })
	t.Run(f.Name+"/timeout_or_no_handler", func(t *testing.T) { testTimeoutOrNoHandler(t, f) })
	t.Run(f.Name+"/close_idempotent", func(t *testing.T) { testCloseIdempotent(t, f) })
	backend := f.New(t)
	caps := backend.Capabilities()
	_ = backend.Shutdown(context.Background())
	if caps.DurablePubSub {
		t.Run(f.Name+"/durable_pubsub", func(t *testing.T) { testDurablePubSub(t, f) })
	}
}

func subject(t testing.TB, suffix string) string {
	return "pulse.test." + pulse.SafeName(t.Name()) + "." + suffix + "." + fmt.Sprint(time.Now().UnixNano())
}

func afterSubscribe(t testing.TB, f Factory) {
	t.Helper()
	if f.AfterSubscribe != nil {
		f.AfterSubscribe(t)
	}
}

func testFanout(t *testing.T, f Factory) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	b := f.New(t)
	defer func(b pulse.Backend, ctx context.Context) {
		_ = b.Shutdown(ctx)
	}(b, context.Background())
	subj := subject(t, "fanout")
	got1 := make(chan pulse.Message, 1)
	got2 := make(chan pulse.Message, 1)
	_, err := b.Subscribe(ctx, subj, func(ctx context.Context, msg pulse.Message) error { got1 <- msg; return nil }, pulse.SubscribeOptions{Delivery: pulse.Fanout})
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.Subscribe(ctx, subj, func(ctx context.Context, msg pulse.Message) error { got2 <- msg; return nil }, pulse.SubscribeOptions{Delivery: pulse.Fanout})
	if err != nil {
		t.Fatal(err)
	}
	afterSubscribe(t, f)
	if err := b.Publish(ctx, subj, pulse.Message{Header: pulse.Header{"x-test": {"yes"}}, Data: []byte("hello")}, pulse.PublishOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := string(waitMsg(t, ctx, got1).Data); got != "hello" {
		t.Fatalf("subscriber 1 got %q", got)
	}
	if got := string(waitMsg(t, ctx, got2).Data); got != "hello" {
		t.Fatalf("subscriber 2 got %q", got)
	}
}

func testWorkQueue(t *testing.T, f Factory) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	b := f.New(t)
	defer func(b pulse.Backend, ctx context.Context) {
		_ = b.Shutdown(ctx)
	}(b, context.Background())
	subj := subject(t, "wq")
	var count atomic.Int64
	for i := 0; i < 2; i++ {
		_, err := b.Subscribe(ctx, subj, func(ctx context.Context, msg pulse.Message) error { count.Add(1); return nil }, pulse.SubscribeOptions{Delivery: pulse.WorkQueue, Group: "workers"})
		if err != nil {
			t.Fatal(err)
		}
	}
	afterSubscribe(t, f)
	for i := 0; i < 20; i++ {
		if err := b.Publish(ctx, subj, pulse.Message{Data: []byte("x")}, pulse.PublishOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, 30*time.Second, func() bool { return count.Load() == 20 }, "work queue count")
}

func testRequestReply(t *testing.T, f Factory) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	b := f.New(t)
	defer func(b pulse.Backend, ctx context.Context) {
		_ = b.Shutdown(ctx)
	}(b, context.Background())
	subj := subject(t, "rpc")
	_, err := b.Handle(ctx, subj, func(ctx context.Context, msg pulse.Message) (pulse.Message, error) {
		return pulse.Message{Header: msg.Header.Clone(), Data: append([]byte("echo:"), msg.Data...)}, nil
	}, pulse.SubscribeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	afterSubscribe(t, f)
	resp, err := b.Request(ctx, subj, pulse.Message{Header: pulse.Header{"k": {"v1", "v2"}}, Data: []byte("hello")}, pulse.RequestOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(resp.Data); got != "echo:hello" {
		t.Fatalf("got %q", got)
	}
	if fmt.Sprint(resp.Header.Values("k")) != "[v1 v2]" {
		t.Fatalf("headers did not round-trip: %#v", resp.Header)
	}
}

func testRequestAsync(t *testing.T, f Factory) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	b := f.New(t)
	defer func(b pulse.Backend, ctx context.Context) {
		_ = b.Shutdown(ctx)
	}(b, context.Background())
	subj := subject(t, "async")
	_, err := b.Handle(ctx, subj, func(ctx context.Context, msg pulse.Message) (pulse.Message, error) {
		time.Sleep(25 * time.Millisecond)
		return pulse.Message{Data: []byte("done")}, nil
	}, pulse.SubscribeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	afterSubscribe(t, f)
	future, err := b.RequestAsync(ctx, subj, pulse.Message{Data: []byte("go")}, pulse.RequestOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := future.Await(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(resp.Data); got != "done" {
		t.Fatalf("got %q", got)
	}
}

func testFireAndForget(t *testing.T, f Factory) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	b := f.New(t)
	defer func(b pulse.Backend, ctx context.Context) {
		_ = b.Shutdown(ctx)
	}(b, context.Background())
	subj := subject(t, "fire")
	var count atomic.Int64
	_, err := b.Handle(ctx, subj, func(ctx context.Context, msg pulse.Message) (pulse.Message, error) {
		count.Add(1)
		return pulse.Message{}, nil
	}, pulse.SubscribeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	afterSubscribe(t, f)
	future, err := b.RequestAsync(ctx, subj, pulse.Message{Data: []byte("go")}, pulse.RequestOptions{NoReply: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := future.Await(ctx); err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, func() bool { return count.Load() == 1 }, "fire-and-forget count")
}

func testTimeoutOrNoHandler(t *testing.T, f Factory) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	b := f.New(t)
	defer func(b pulse.Backend, ctx context.Context) {
		_ = b.Shutdown(ctx)
	}(b, context.Background())
	_, err := b.Request(ctx, subject(t, "missing"), pulse.Message{Data: []byte("x")}, pulse.RequestOptions{Timeout: 100 * time.Millisecond})
	if err == nil {
		t.Fatal("expected error")
	}
}

func testCloseIdempotent(t *testing.T, f Factory) {
	ctx := context.Background()
	b := f.New(t)
	if err := b.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	err := b.Publish(ctx, subject(t, "closed"), pulse.Message{Data: []byte("x")}, pulse.PublishOptions{})
	if err == nil || !errors.Is(err, pulse.ErrClosed) {
		t.Fatalf("expected close error, got %v", err)
	}
}

func testDurablePubSub(t *testing.T, f Factory) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	b := f.New(t)
	defer func(b pulse.Backend, ctx context.Context) {
		_ = b.Shutdown(ctx)
	}(b, context.Background())
	subj := subject(t, "durable")
	ch := make(chan pulse.Message, 1)
	_, err := b.Subscribe(ctx, subj, func(ctx context.Context, msg pulse.Message) error { ch <- msg; return nil }, pulse.SubscribeOptions{Delivery: pulse.WorkQueue, Group: "durable", Reliability: pulse.Durable})
	if err != nil {
		t.Fatal(err)
	}
	afterSubscribe(t, f)
	if err := b.Publish(ctx, subj, pulse.Message{Data: []byte("durable")}, pulse.PublishOptions{Reliability: pulse.Durable}); err != nil {
		t.Fatal(err)
	}
	if got := string(waitMsg(t, ctx, ch).Data); got != "durable" {
		t.Fatalf("got %q", got)
	}
}

func waitMsg(t testing.TB, ctx context.Context, ch <-chan pulse.Message) pulse.Message {
	t.Helper()
	select {
	case msg := <-ch:
		return msg
	case <-ctx.Done():
		t.Fatalf("timed out waiting for message: %v", ctx.Err())
		return pulse.Message{}
	}
}

func eventually(t testing.TB, timeout time.Duration, fn func() bool, name string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("condition did not become true: %s", name)
}
