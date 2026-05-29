package kafka_test

import (
	"context"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otpki/otpki-pkg/spfy/pulse"
	pulsekafka "github.com/otpki/otpki-pkg/spfy/pulse/backends/kafka"
	"github.com/otpki/otpki-pkg/spfy/pulse/pulsetest"
	segmentio "github.com/segmentio/kafka-go"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
)

var kafkaFixture struct {
	once      sync.Once
	broker    string
	container testcontainers.Container
	err       error
}

func ipv4Localhost(host string) string {
	if host == "localhost" || host == "::1" || host == "[::1]" {
		return "127.0.0.1"
	}
	return host
}

func normalizeBroker(broker string) string {
	host, port, err := net.SplitHostPort(broker)
	if err != nil {
		return strings.Replace(broker, "localhost", "127.0.0.1", 1)
	}
	return net.JoinHostPort(ipv4Localhost(host), port)
}

func waitForKafka(ctx context.Context, broker string) error {
	deadline := time.Now().Add(30 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		conn, err := segmentio.DialContext(ctx, "tcp", broker)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		last = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return last
}

func kafkaBroker(t testing.TB) string {
	t.Helper()
	kafkaFixture.once.Do(func() {
		ctx := context.Background()
		ctr, err := redpanda.Run(ctx, "docker.redpanda.com/redpandadata/redpanda:v23.3.3", redpanda.WithAutoCreateTopics())
		if err != nil {
			kafkaFixture.err = err
			return
		}
		broker, err := ctr.KafkaSeedBroker(ctx)
		if err != nil {
			_ = ctr.Terminate(context.Background())
			kafkaFixture.err = err
			return
		}
		kafkaFixture.container = ctr
		kafkaFixture.broker = normalizeBroker(broker)
		if err := waitForKafka(ctx, kafkaFixture.broker); err != nil {
			_ = ctr.Terminate(context.Background())
			kafkaFixture.err = err
			return
		}
	})
	if kafkaFixture.err != nil {
		t.Fatal(kafkaFixture.err)
	}
	return kafkaFixture.broker
}

func waitForTopic(ctx context.Context, broker, topic string) error {
	deadline := time.Now().Add(10 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		conn, err := segmentio.DialLeader(ctx, "tcp", broker, topic, 0)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		last = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return last
}

func ensureTopic(broker string) pulsekafka.TopicEnsurer {
	var mu sync.Mutex
	seen := map[string]struct{}{}
	return func(ctx context.Context, topic string) error {
		mu.Lock()
		if _, ok := seen[topic]; ok {
			mu.Unlock()
			return nil
		}
		mu.Unlock()

		conn, err := segmentio.DialContext(ctx, "tcp", broker)
		if err != nil {
			return err
		}
		controller, err := conn.Controller()
		_ = conn.Close()
		if err != nil {
			return err
		}

		controllerAddr := net.JoinHostPort(ipv4Localhost(controller.Host), strconv.Itoa(controller.Port))
		conn, err = segmentio.DialContext(ctx, "tcp", controllerAddr)
		if err != nil {
			conn, err = segmentio.DialContext(ctx, "tcp", broker)
			if err != nil {
				return err
			}
		}
		defer conn.Close()

		err = conn.CreateTopics(segmentio.TopicConfig{Topic: topic, NumPartitions: 1, ReplicationFactor: 1})
		if err != nil && !strings.Contains(strings.ToLower(err.Error()), "already") {
			return err
		}
		if err := waitForTopic(ctx, broker, topic); err != nil {
			return err
		}

		mu.Lock()
		seen[topic] = struct{}{}
		mu.Unlock()
		return nil
	}
}

func TestMain(m *testing.M) {
	code := m.Run()
	if kafkaFixture.container != nil {
		_ = kafkaFixture.container.Terminate(context.Background())
	}
	os.Exit(code)
}

func TestIntegrationConformance(t *testing.T) {
	pulsetest.RunConformance(t, pulsetest.Factory{
		Name: "kafka",
		New: func(tb testing.TB) pulse.Backend {
			broker := kafkaBroker(tb)
			writer := &segmentio.Writer{Addr: segmentio.TCP(broker), RequiredAcks: segmentio.RequireOne, Async: false, AllowAutoTopicCreation: true, BatchTimeout: 10 * time.Millisecond}
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
	})
}
