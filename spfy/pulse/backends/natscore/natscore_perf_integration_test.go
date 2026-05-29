package natscore_test

import (
	"testing"
	"time"

	nats "github.com/nats-io/nats.go"
	"github.com/otpki/otpki-pkg/spfy/pulse"
	"github.com/otpki/otpki-pkg/spfy/pulse/backends/natscore"
	"github.com/otpki/otpki-pkg/spfy/pulse/pulsetest"
)

func natscoreIntegrationFactory() pulsetest.Factory {
	return pulsetest.Factory{Name: "natscore", New: func(tb testing.TB) pulse.Backend {
		nc, err := nats.Connect(natsAddr(tb), nats.RetryOnFailedConnect(true), nats.MaxReconnects(-1), nats.ReconnectWait(100*time.Millisecond), nats.Timeout(10*time.Second))
		if err != nil {
			tb.Fatal(err)
		}
		tb.Cleanup(func() { _ = nc.Drain() })
		b, err := natscore.New(nc, natscore.Config{SubjectPrefix: "test." + pulse.SafeName(tb.Name())})
		if err != nil {
			tb.Fatal(err)
		}
		return b
	}}
}

func TestIntegrationStress(t *testing.T) {
	pulsetest.RunStressWithOptions(t, natscoreIntegrationFactory(), pulsetest.StressOptions{})
}

func BenchmarkIntegrationRequestReply(b *testing.B) {
	pulsetest.BenchmarkRequestReply(b, natscoreIntegrationFactory())
}

func BenchmarkIntegrationRequestAsync(b *testing.B) {
	pulsetest.BenchmarkRequestAsync(b, natscoreIntegrationFactory())
}

func BenchmarkIntegrationFireAndForget(b *testing.B) {
	pulsetest.BenchmarkFireAndForget(b, natscoreIntegrationFactory())
}

func BenchmarkIntegrationPublishFanout(b *testing.B) {
	pulsetest.BenchmarkPublishFanout(b, natscoreIntegrationFactory())
}

func BenchmarkIntegrationPublishWorkQueue(b *testing.B) {
	pulsetest.BenchmarkPublishWorkQueue(b, natscoreIntegrationFactory())
}

func BenchmarkIntegrationRequestReplyParallel(b *testing.B) {
	pulsetest.BenchmarkRequestReplyParallel(b, natscoreIntegrationFactory())
}
