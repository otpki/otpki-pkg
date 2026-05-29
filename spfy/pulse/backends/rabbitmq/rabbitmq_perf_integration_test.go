package rabbitmq_test

import (
	"testing"
	"time"

	"github.com/otpki/otpki-pkg/spfy/pulse"
	"github.com/otpki/otpki-pkg/spfy/pulse/backends/rabbitmq"
	"github.com/otpki/otpki-pkg/spfy/pulse/pulsetest"
)

func rabbitmqIntegrationFactory() pulsetest.Factory {
	return pulsetest.Factory{Name: "rabbitmq", New: func(tb testing.TB) pulse.Backend {
		conn := dialRabbit(tb, rabbitURL(tb))
		ch, err := conn.Channel()
		if err != nil {
			_ = conn.Close()
			tb.Fatal(err)
		}
		tb.Cleanup(func() { _ = ch.Close(); _ = conn.Close() })
		prefix := "test." + pulse.SafeName(tb.Name())
		b, err := rabbitmq.New(ch, rabbitmq.Config{ExchangePrefix: prefix, QueuePrefix: prefix})
		if err != nil {
			tb.Fatal(err)
		}
		return b
	}, AfterSubscribe: func(testing.TB) { time.Sleep(300 * time.Millisecond) }}
}

func TestIntegrationStress(t *testing.T) {
	pulsetest.RunStressWithOptions(t, rabbitmqIntegrationFactory(), pulsetest.StressOptions{})
}

func BenchmarkIntegrationRequestReply(b *testing.B) {
	pulsetest.BenchmarkRequestReply(b, rabbitmqIntegrationFactory())
}

func BenchmarkIntegrationRequestAsync(b *testing.B) {
	pulsetest.BenchmarkRequestAsync(b, rabbitmqIntegrationFactory())
}

func BenchmarkIntegrationFireAndForget(b *testing.B) {
	pulsetest.BenchmarkFireAndForget(b, rabbitmqIntegrationFactory())
}

func BenchmarkIntegrationPublishFanout(b *testing.B) {
	pulsetest.BenchmarkPublishFanout(b, rabbitmqIntegrationFactory())
}

func BenchmarkIntegrationPublishWorkQueue(b *testing.B) {
	pulsetest.BenchmarkPublishWorkQueue(b, rabbitmqIntegrationFactory())
}

func BenchmarkIntegrationRequestReplyParallel(b *testing.B) {
	pulsetest.BenchmarkRequestReplyParallel(b, rabbitmqIntegrationFactory())
}
