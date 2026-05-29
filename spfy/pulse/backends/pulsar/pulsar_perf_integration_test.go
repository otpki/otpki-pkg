package pulsar_test

import (
	"testing"
	"time"

	pulsarclient "github.com/apache/pulsar-client-go/pulsar"
	pulsarlog "github.com/apache/pulsar-client-go/pulsar/log"
	"github.com/otpki/otpki-pkg/spfy/pulse"
	pulsepulsar "github.com/otpki/otpki-pkg/spfy/pulse/backends/pulsar"
	"github.com/otpki/otpki-pkg/spfy/pulse/pulsetest"
)

func pulsarIntegrationFactory() pulsetest.Factory {
	return pulsetest.Factory{Name: "pulsar", New: func(tb testing.TB) pulse.Backend {
		client, err := pulsarclient.NewClient(pulsarclient.ClientOptions{URL: pulsarURL(tb), OperationTimeout: 10 * time.Second, ConnectionTimeout: 10 * time.Second, Logger: pulsarlog.DefaultNopLogger()})
		if err != nil {
			tb.Fatal(err)
		}
		tb.Cleanup(client.Close)
		b, err := pulsepulsar.New(client, pulsepulsar.Config{TopicPrefix: "test/" + pulse.SafeName(tb.Name())})
		if err != nil {
			tb.Fatal(err)
		}
		return b
	}, AfterSubscribe: func(testing.TB) { time.Sleep(750 * time.Millisecond) }}
}

func TestIntegrationStress(t *testing.T) {
	pulsetest.RunStressWithOptions(t, pulsarIntegrationFactory(), pulsetest.StressOptions{Workers: 8, RequestsPerWorker: 50, Timeout: 2 * time.Minute})
}

func BenchmarkIntegrationRequestReply(b *testing.B) {
	pulsetest.BenchmarkRequestReply(b, pulsarIntegrationFactory())
}

func BenchmarkIntegrationRequestAsync(b *testing.B) {
	pulsetest.BenchmarkRequestAsync(b, pulsarIntegrationFactory())
}

func BenchmarkIntegrationFireAndForget(b *testing.B) {
	pulsetest.BenchmarkFireAndForget(b, pulsarIntegrationFactory())
}

func BenchmarkIntegrationPublishFanout(b *testing.B) {
	pulsetest.BenchmarkPublishFanout(b, pulsarIntegrationFactory())
}

func BenchmarkIntegrationPublishWorkQueue(b *testing.B) {
	pulsetest.BenchmarkPublishWorkQueue(b, pulsarIntegrationFactory())
}

func BenchmarkIntegrationRequestReplyParallel(b *testing.B) {
	pulsetest.BenchmarkRequestReplyParallel(b, pulsarIntegrationFactory())
}
