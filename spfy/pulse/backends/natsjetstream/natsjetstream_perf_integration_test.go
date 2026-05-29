package natsjetstream_test

import (
	"testing"
	"time"

	nats "github.com/nats-io/nats.go"
	"github.com/otpki/otpki-pkg/spfy/pulse"
	"github.com/otpki/otpki-pkg/spfy/pulse/backends/natsjetstream"
	"github.com/otpki/otpki-pkg/spfy/pulse/pulsetest"
)

func natsjetstreamIntegrationFactory() pulsetest.Factory {
	return pulsetest.Factory{Name: "natsjetstream", New: func(tb testing.TB) pulse.Backend {
		nc, err := nats.Connect(natsAddr(tb), nats.RetryOnFailedConnect(true), nats.MaxReconnects(-1), nats.ReconnectWait(100*time.Millisecond), nats.Timeout(10*time.Second))
		if err != nil {
			tb.Fatal(err)
		}
		tb.Cleanup(func() { _ = nc.Drain() })
		b, err := natsjetstream.New(nc, natsjetstream.Config{SubjectPrefix: "test." + pulse.SafeName(tb.Name()), StreamName: "PULSE" + pulse.SafeName(tb.Name())})
		if err != nil {
			tb.Fatal(err)
		}
		return b
	}}
}

func TestIntegrationStress(t *testing.T) {
	pulsetest.RunStressWithOptions(t, natsjetstreamIntegrationFactory(), pulsetest.StressOptions{})
}

func BenchmarkIntegrationRequestReply(b *testing.B) {
	pulsetest.BenchmarkRequestReply(b, natsjetstreamIntegrationFactory())
}

func BenchmarkIntegrationRequestAsync(b *testing.B) {
	pulsetest.BenchmarkRequestAsync(b, natsjetstreamIntegrationFactory())
}

func BenchmarkIntegrationFireAndForget(b *testing.B) {
	pulsetest.BenchmarkFireAndForget(b, natsjetstreamIntegrationFactory())
}

func BenchmarkIntegrationPublishFanout(b *testing.B) {
	pulsetest.BenchmarkPublishFanout(b, natsjetstreamIntegrationFactory())
}

func BenchmarkIntegrationPublishWorkQueue(b *testing.B) {
	pulsetest.BenchmarkPublishWorkQueue(b, natsjetstreamIntegrationFactory())
}

func BenchmarkIntegrationRequestReplyParallel(b *testing.B) {
	pulsetest.BenchmarkRequestReplyParallel(b, natsjetstreamIntegrationFactory())
}
