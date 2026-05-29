package anchortest

import (
	"context"
	"fmt"
	"testing"

	"github.com/otpki/otpki-pkg/spfy/anchor"
)

func BenchmarkPut(b *testing.B, f Factory) {
	ctx := context.Background()
	store := f.New(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench/put/%d", i)
		if _, err := store.Put(ctx, key, StateValue{Name: "bench", Count: i}, anchor.WriteOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGet(b *testing.B, f Factory) {
	ctx := context.Background()
	store := f.New(b)
	// Pre-populate
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench/get/%d", i)
		if _, err := store.Put(ctx, key, StateValue{Name: "bench", Count: i}, anchor.WriteOptions{}); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench/get/%d", i)
		if _, err := store.Get(ctx, key); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDelete(b *testing.B, f Factory) {
	ctx := context.Background()
	store := f.New(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench/delete/%d", i)
		if _, err := store.Put(ctx, key, StateValue{Name: "bench", Count: i}, anchor.WriteOptions{}); err != nil {
			b.Fatal(err)
		}
		if err := store.Delete(ctx, key); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkQuery(b *testing.B, f Factory) {
	ctx := context.Background()
	store := f.New(b)
	// Pre-populate
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("bench/query/%d", i)
		if _, err := store.Put(ctx, key, StateValue{Name: "bench", Count: i}, anchor.WriteOptions{}); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.Query(ctx, anchor.Query{Prefix: "bench/query/", Limit: 100}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBatch(b *testing.B, f Factory) {
	ctx := context.Background()
	store := f.New(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ops := make([]anchor.Operation[StateValue], 10)
		for j := 0; j < 10; j++ {
			ops[j] = anchor.Operation[StateValue]{
				Type:  anchor.OpPut,
				Key:   fmt.Sprintf("bench/batch/%d/%d", i, j),
				Value: StateValue{Name: "bench", Count: j},
			}
		}
		if _, err := store.Batch(ctx, ops); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompareAndSet(b *testing.B, f Factory) {
	ctx := context.Background()
	store := f.New(b)
	key := "bench/cas"
	if _, err := store.Put(ctx, key, StateValue{Name: "bench", Count: 0}, anchor.WriteOptions{}); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry, err := store.Get(ctx, key)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := store.CompareAndSet(ctx, key, entry.ETag, StateValue{Name: "bench", Count: i}, anchor.WriteOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}
