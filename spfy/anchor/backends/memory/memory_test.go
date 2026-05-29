package memory_test

import (
	"testing"

	"github.com/otpki/otpki-pkg/spfy/anchor"
	"github.com/otpki/otpki-pkg/spfy/anchor/anchortest"
	"github.com/otpki/otpki-pkg/spfy/anchor/backends/memory"
)

func memoryFactory() anchortest.Factory {
	return anchortest.Factory{
		Name: "memory",
		New: func(testing.TB) anchor.Store[anchortest.StateValue] {
			return memory.New(memory.DefaultConfig(), anchor.JSONCodec[anchortest.StateValue]{})
		},
	}
}

func TestConformance(t *testing.T) {
	anchortest.RunConformance(t, memoryFactory())
}

func TestStress(t *testing.T) {
	anchortest.RunStressWithOptions(t, memoryFactory(), anchortest.StressOptions{Workers: 64, OperationsPerWorker: 500})
}

func BenchmarkPut(b *testing.B) {
	anchortest.BenchmarkPut(b, memoryFactory())
}

func BenchmarkGet(b *testing.B) {
	anchortest.BenchmarkGet(b, memoryFactory())
}

func BenchmarkDelete(b *testing.B) {
	anchortest.BenchmarkDelete(b, memoryFactory())
}

func BenchmarkQuery(b *testing.B) {
	anchortest.BenchmarkQuery(b, memoryFactory())
}

func BenchmarkBatch(b *testing.B) {
	anchortest.BenchmarkBatch(b, memoryFactory())
}

func BenchmarkCompareAndSet(b *testing.B) {
	anchortest.BenchmarkCompareAndSet(b, memoryFactory())
}
