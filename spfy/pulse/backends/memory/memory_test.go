package memory_test

import (
	"testing"

	"github.com/otpki/otpki-pkg/spfy/pulse"
	"github.com/otpki/otpki-pkg/spfy/pulse/backends/memory"
	"github.com/otpki/otpki-pkg/spfy/pulse/pulsetest"
)

func memoryFactory(buffer int) pulsetest.Factory {
	return pulsetest.Factory{Name: "memory", New: func(testing.TB) pulse.Backend { return memory.New(memory.Config{Buffer: buffer}) }}
}

func TestConformance(t *testing.T) {
	pulsetest.RunConformance(t, memoryFactory(0))
}

func TestStress(t *testing.T) {
	pulsetest.RunStressWithOptions(t, memoryFactory(65536), pulsetest.StressOptions{Workers: 64, RequestsPerWorker: 500})
}

func BenchmarkRequestReply(b *testing.B) {
	pulsetest.BenchmarkRequestReply(b, memoryFactory(65536))
}

func BenchmarkRequestAsync(b *testing.B) {
	pulsetest.BenchmarkRequestAsync(b, memoryFactory(65536))
}

func BenchmarkFireAndForget(b *testing.B) {
	pulsetest.BenchmarkFireAndForget(b, memoryFactory(65536))
}

func BenchmarkPublishFanout(b *testing.B) {
	pulsetest.BenchmarkPublishFanout(b, memoryFactory(65536))
}

func BenchmarkPublishWorkQueue(b *testing.B) {
	pulsetest.BenchmarkPublishWorkQueue(b, memoryFactory(65536))
}

func BenchmarkRequestReplyParallel(b *testing.B) {
	pulsetest.BenchmarkRequestReplyParallel(b, memoryFactory(65536))
}
