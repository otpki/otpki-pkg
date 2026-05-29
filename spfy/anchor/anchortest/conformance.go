package anchortest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/otpki/otpki-pkg/spfy/anchor"
	"github.com/otpki/otpki-pkg/spfy/anchor/internal"
)

type Factory struct {
	Name string
	New  func(testing.TB) anchor.Store[StateValue]

	// Skip is an optional function to skip specific tests for this backend
	Skip func(testName string) bool
}

type StateValue struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func RunConformance(t *testing.T, f Factory) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping conformance test in short mode")
	}
	t.Run(f.Name+"/crud_and_etags", func(t *testing.T) { testCRUDAndETags(t, f) })
	t.Run(f.Name+"/query", func(t *testing.T) { testQuery(t, f) })
	t.Run(f.Name+"/batch_atomicity", func(t *testing.T) { testBatchAtomicity(t, f) })
	t.Run(f.Name+"/consume", func(t *testing.T) { testConsume(t, f) })
	t.Run(f.Name+"/ttl", func(t *testing.T) { testTTL(t, f) })
	t.Run(f.Name+"/watch", func(t *testing.T) { testWatch(t, f) })
}

func testCRUDAndETags(t *testing.T, f Factory) {
	if f.Skip != nil && f.Skip("crud_and_etags") {
		t.Skip("skipped by factory")
	}
	ctx := context.Background()
	store := f.New(t)

	created, err := store.Create(ctx, "users/1", StateValue{Name: "one", Count: 1}, anchor.WriteOptions{Metadata: anchor.Metadata{"kind": []string{"user"}}})
	mustNoError(t, err)
	if created.ETag == "" || created.Value.Count != 1 {
		t.Fatalf("unexpected created entry: %+v", created)
	}

	_, err = store.Create(ctx, "users/1", StateValue{}, anchor.WriteOptions{})
	mustErrorIs(t, err, anchor.ErrAlreadyExists)

	got, err := store.Get(ctx, "users/1")
	mustNoError(t, err)
	if got.ETag != created.ETag || got.Value.Name != "one" {
		t.Fatalf("unexpected get: %+v", got)
	}

	_, err = store.CompareAndSet(ctx, "users/1", anchor.ETag("bad"), StateValue{Name: "bad"}, anchor.WriteOptions{})
	mustErrorIs(t, err, anchor.ErrETagMismatch)

	updated, err := store.CompareAndSet(ctx, "users/1", created.ETag, StateValue{Name: "two", Count: 2}, anchor.WriteOptions{Metadata: anchor.Metadata{"role": []string{"admin"}}})
	mustNoError(t, err)
	if updated.ETag == created.ETag || updated.Value.Count != 2 {
		t.Fatalf("CAS did not update: %+v", updated)
	}
	if !internal.MetadataContains(updated.Metadata, anchor.Metadata{"kind": []string{"user"}, "role": []string{"admin"}}) {
		t.Fatalf("metadata was not merged: %+v", updated.Metadata)
	}

	withMeta, err := store.AppendMetadata(ctx, "users/1", anchor.Metadata{"role": []string{"owner"}})
	mustNoError(t, err)
	if !internal.MetadataContains(withMeta.Metadata, anchor.Metadata{"role": []string{"admin", "owner"}}) {
		t.Fatalf("append metadata failed: %+v", withMeta.Metadata)
	}

	mustNoError(t, store.Delete(ctx, "users/1"))
	_, err = store.Get(ctx, "users/1")
	mustErrorIs(t, err, anchor.ErrNotFound)
}

func testQuery(t *testing.T, f Factory) {
	if f.Skip != nil && f.Skip("query") {
		t.Skip("skipped by factory")
	}
	ctx := context.Background()
	store := f.New(t)
	for i := 0; i < 5; i++ {
		_, err := store.Put(ctx, fmt.Sprintf("users/%d", i), StateValue{Name: "user", Count: i}, anchor.WriteOptions{Metadata: anchor.Metadata{"kind": []string{"user"}}})
		mustNoError(t, err)
	}
	_, err := store.Put(ctx, "orders/1", StateValue{Name: "order"}, anchor.WriteOptions{Metadata: anchor.Metadata{"kind": []string{"order"}}})
	mustNoError(t, err)

	page1, err := store.Query(ctx, anchor.Query{Prefix: "users/", Metadata: anchor.Metadata{"kind": []string{"user"}}, Limit: 2})
	mustNoError(t, err)
	if len(page1.Entries) != 2 || page1.NextCursor == "" {
		t.Fatalf("unexpected page1: %+v", page1)
	}
	page2, err := store.Query(ctx, anchor.Query{Prefix: "users/", Metadata: anchor.Metadata{"kind": []string{"user"}}, Limit: 10, Cursor: page1.NextCursor})
	mustNoError(t, err)
	if len(page2.Entries) != 3 || page2.NextCursor != "" {
		t.Fatalf("unexpected page2: %+v", page2)
	}
}

func testBatchAtomicity(t *testing.T, f Factory) {
	if f.Skip != nil && f.Skip("batch_atomicity") {
		t.Skip("skipped by factory")
	}
	ctx := context.Background()
	store := f.New(t)
	_, err := store.Create(ctx, "a", StateValue{Name: "a"}, anchor.WriteOptions{})
	mustNoError(t, err)

	_, err = store.Batch(ctx, []anchor.Operation[StateValue]{
		{Type: anchor.OpCreate, Key: "b", Value: StateValue{Name: "b"}},
		{Type: anchor.OpCreate, Key: "a", Value: StateValue{Name: "duplicate"}},
	})
	mustErrorIs(t, err, anchor.ErrAlreadyExists)

	_, err = store.Get(ctx, "b")
	mustErrorIs(t, err, anchor.ErrNotFound)

	entries, err := store.Batch(ctx, []anchor.Operation[StateValue]{
		{Type: anchor.OpPut, Key: "a", Value: StateValue{Name: "a2"}},
		{Type: anchor.OpCreate, Key: "b", Value: StateValue{Name: "b"}},
	})
	mustNoError(t, err)
	if len(entries) != 2 {
		t.Fatalf("unexpected batch entries: %+v", entries)
	}
}

func testConsume(t *testing.T, f Factory) {
	if f.Skip != nil && f.Skip("consume") {
		t.Skip("skipped by factory")
	}
	ctx := context.Background()
	store := f.New(t)
	created, err := store.Put(ctx, "queue/1", StateValue{Name: "payload"}, anchor.WriteOptions{})
	mustNoError(t, err)

	consumed, err := store.Consume(ctx, "queue/1")
	mustNoError(t, err)
	if consumed.ETag != created.ETag || consumed.Value.Name != "payload" {
		t.Fatalf("unexpected consumed entry: %+v", consumed)
	}
	_, err = store.Get(ctx, "queue/1")
	mustErrorIs(t, err, anchor.ErrNotFound)
	_, err = store.Consume(ctx, "queue/1")
	mustErrorIs(t, err, anchor.ErrNotFound)
}

func testTTL(t *testing.T, f Factory) {
	if f.Skip != nil && f.Skip("ttl") {
		t.Skip("skipped by factory")
	}
	ctx := context.Background()
	store := f.New(t)
	_, err := store.Put(ctx, "ttl", StateValue{Name: "ttl"}, anchor.WriteOptions{TTL: 30 * time.Millisecond})
	mustNoError(t, err)
	time.Sleep(80 * time.Millisecond)
	_, err = store.Get(ctx, "ttl")
	mustErrorIs(t, err, anchor.ErrNotFound)
}

func testWatch(t *testing.T, f Factory) {
	if f.Skip != nil && f.Skip("watch") {
		t.Skip("skipped by factory")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := f.New(t)
	_, err := store.Put(ctx, "users/1", StateValue{Name: "one"}, anchor.WriteOptions{})
	mustNoError(t, err)

	watch, err := store.Watch(ctx, anchor.WatchOptions{Prefix: "users/", IncludeCurrent: true})
	mustNoError(t, err)
	ev := receiveEvent(t, watch)
	if ev.Type != anchor.EventPut || ev.Key != "users/1" || ev.Entry == nil || ev.Entry.Value.Name != "one" {
		t.Fatalf("unexpected current event: %+v", ev)
	}

	_, err = store.Put(ctx, "users/2", StateValue{Name: "two"}, anchor.WriteOptions{})
	mustNoError(t, err)
	ev = receiveEvent(t, watch)
	if ev.Type != anchor.EventPut || ev.Key != "users/2" {
		t.Fatalf("unexpected live event: %+v", ev)
	}
}

func receiveEvent(t *testing.T, ch <-chan anchor.Event[StateValue]) anchor.Event[StateValue] {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
		return anchor.Event[StateValue]{}
	}
}

func mustNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func mustErrorIs(t *testing.T, err error, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("got error %v, want %v", err, target)
	}
}

// eventually waits for a condition to become true within a timeout
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
