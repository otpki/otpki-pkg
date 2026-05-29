package kafka_test

import (
	"testing"
	"time"

	"github.com/otpki/otpki-pkg/spfy/pulse"
	pulsekafka "github.com/otpki/otpki-pkg/spfy/pulse/backends/kafka"
	"github.com/otpki/otpki-pkg/spfy/pulse/pulsetest"
	segmentio "github.com/segmentio/kafka-go"
)

func kafkaIntegrationFactory() pulsetest.Factory {
	return pulsetest.Factory{
		Name: "kafka",
		New: func(tb testing.TB) pulse.Backend {
			broker := kafkaBroker(tb)
			writer := &segmentio.Writer{
				Addr:                   segmentio.TCP(broker),
				RequiredAcks:           segmentio.RequireOne,
				Async:                  false,
				AllowAutoTopicCreation: true,
				BatchTimeout:           10 * time.Millisecond,
			}
			tb.Cleanup(func() { _ = writer.Close() })
			factory := func(cfg segmentio.ReaderConfig) *segmentio.Reader {
				cfg.Brokers = []string{broker}
				return segmentio.NewReader(cfg)
			}
			prefix := "test." + pulse.SafeName(tb.Name())
			b, err := pulsekafka.New(writer, factory, pulsekafka.Config{TopicPrefix: prefix, GroupPrefix: prefix, EnsureTopic: ensureTopic(broker)})
			if err != nil {
				tb.Fatal(err)
			}
			return b
		},
		AfterSubscribe: func(testing.TB) { time.Sleep(2 * time.Second) },
	}
}

func TestIntegrationStress(t *testing.T) {
	pulsetest.RunStressWithOptions(t, kafkaIntegrationFactory(), pulsetest.StressOptions{Workers: 8, RequestsPerWorker: 50, Timeout: 2 * time.Minute})
}

func BenchmarkIntegrationRequestReply(b *testing.B) {
	pulsetest.BenchmarkRequestReply(b, kafkaIntegrationFactory())
}

func BenchmarkIntegrationRequestAsync(b *testing.B) {
	pulsetest.BenchmarkRequestAsync(b, kafkaIntegrationFactory())
}

func BenchmarkIntegrationFireAndForget(b *testing.B) {
	pulsetest.BenchmarkFireAndForget(b, kafkaIntegrationFactory())
}

func BenchmarkIntegrationPublishFanout(b *testing.B) {
	pulsetest.BenchmarkPublishFanout(b, kafkaIntegrationFactory())
}

func BenchmarkIntegrationPublishWorkQueue(b *testing.B) {
	pulsetest.BenchmarkPublishWorkQueue(b, kafkaIntegrationFactory())
}

func BenchmarkIntegrationRequestReplyParallel(b *testing.B) {
	pulsetest.BenchmarkRequestReplyParallel(b, kafkaIntegrationFactory())
}
