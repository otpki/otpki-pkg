package ristretto_test

import (
	"testing"

	ristrettocache "github.com/dgraph-io/ristretto/v2"
	"github.com/otpki/otpki-pkg/spfy/anchor"
	"github.com/otpki/otpki-pkg/spfy/anchor/anchortest"
	"github.com/otpki/otpki-pkg/spfy/anchor/backends/ristretto"
)

func ristrettoFactory() anchortest.Factory {
	return anchortest.Factory{
		Name: "ristretto",
		New: func(tb testing.TB) anchor.Store[anchortest.StateValue] {
			cache, err := ristrettocache.NewCache(&ristrettocache.Config[string, []byte]{
				NumCounters: 1e4,
				MaxCost:     1 << 20,
				BufferItems: 64,
			})
			if err != nil {
				tb.Fatal(err)
			}
			tb.Cleanup(cache.Close)
			store, err := ristretto.New[anchortest.StateValue](
				ristretto.DefaultConfig(),
				anchor.JSONCodec[anchortest.StateValue]{},
			)
			if err != nil {
				tb.Fatal(err)
			}
			return store
		},
	}
}

func TestConformance(t *testing.T) {
	anchortest.RunConformance(t, ristrettoFactory())
}

func TestStress(t *testing.T) {
	anchortest.RunStressWithOptions(
		t,
		ristrettoFactory(),
		anchortest.StressOptions{Workers: 64, OperationsPerWorker: 500},
	)
}

func BenchmarkPut(b *testing.B) {
	anchortest.BenchmarkPut(b, ristrettoFactory())
}

func BenchmarkGet(b *testing.B) {
	anchortest.BenchmarkGet(b, ristrettoFactory())
}

func BenchmarkDelete(b *testing.B) {
	anchortest.BenchmarkDelete(b, ristrettoFactory())
}

func BenchmarkQuery(b *testing.B) {
	anchortest.BenchmarkQuery(b, ristrettoFactory())
}

func BenchmarkBatch(b *testing.B) {
	anchortest.BenchmarkBatch(b, ristrettoFactory())
}

func BenchmarkCompareAndSet(b *testing.B) {
	anchortest.BenchmarkCompareAndSet(b, ristrettoFactory())
}
