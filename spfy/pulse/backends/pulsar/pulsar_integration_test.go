package pulsar_test

import (
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	pulsarclient "github.com/apache/pulsar-client-go/pulsar"
	pulsarlog "github.com/apache/pulsar-client-go/pulsar/log"
	"github.com/otpki/otpki-pkg/spfy/pulse"
	pulsepulsar "github.com/otpki/otpki-pkg/spfy/pulse/backends/pulsar"
	"github.com/otpki/otpki-pkg/spfy/pulse/pulsetest"
	"github.com/testcontainers/testcontainers-go"
	tcpulsar "github.com/testcontainers/testcontainers-go/modules/pulsar"
	"github.com/testcontainers/testcontainers-go/wait"
)

var pulsarFixture struct {
	once      sync.Once
	url       string
	container testcontainers.Container
	err       error
}

func normalizePulsarURL(url string) string {
	return strings.Replace(url, "localhost", "127.0.0.1", 1)
}

func waitForPulsar(ctx context.Context, url string) error {
	deadline := time.Now().Add(90 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		client, err := pulsarclient.NewClient(pulsarclient.ClientOptions{URL: url, OperationTimeout: 5 * time.Second, ConnectionTimeout: 5 * time.Second, Logger: pulsarlog.DefaultNopLogger()})
		if err == nil {
			topic := "persistent://public/default/pulse-readiness-" + pulse.SafeName(time.Now().Format("150405.000000000"))
			producer, prodErr := client.CreateProducer(pulsarclient.ProducerOptions{Topic: topic, DisableBatching: true})
			if prodErr == nil {
				producer.Close()
				client.Close()
				return nil
			}
			last = prodErr
			client.Close()
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(750 * time.Millisecond):
		}
	}
	return last
}

func pulsarURL(t testing.TB) string {
	t.Helper()
	pulsarFixture.once.Do(func() {
		ctx := context.Background()
		ctr, err := tcpulsar.Run(ctx, "apachepulsar/pulsar:4.2.1", testcontainers.WithWaitStrategyAndDeadline(120*time.Second,
			wait.ForListeningPort("6650/tcp").WithStartupTimeout(90*time.Second),
			wait.ForHTTP("/admin/v2/clusters").WithPort("8080/tcp").WithForcedIPv4LocalHost().WithResponseMatcher(func(r io.Reader) bool {
				body, _ := io.ReadAll(r)
				return strings.Contains(string(body), "standalone")
			}).WithStartupTimeout(90*time.Second),
		))
		if err != nil {
			pulsarFixture.err = err
			return
		}
		url, err := ctr.BrokerURL(ctx)
		if err != nil {
			_ = ctr.Terminate(context.Background())
			pulsarFixture.err = err
			return
		}
		pulsarFixture.container = ctr
		pulsarFixture.url = normalizePulsarURL(url)
		if err := waitForPulsar(ctx, pulsarFixture.url); err != nil {
			_ = ctr.Terminate(context.Background())
			pulsarFixture.err = err
			return
		}
	})
	if pulsarFixture.err != nil {
		t.Fatal(pulsarFixture.err)
	}
	return pulsarFixture.url
}

func TestMain(m *testing.M) {
	code := m.Run()
	if pulsarFixture.container != nil {
		_ = pulsarFixture.container.Terminate(context.Background())
	}
	os.Exit(code)
}

func TestIntegrationConformance(t *testing.T) {
	pulsetest.RunConformance(t, pulsetest.Factory{Name: "pulsar", New: func(tb testing.TB) pulse.Backend {
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
	}, AfterSubscribe: func(testing.TB) { time.Sleep(750 * time.Millisecond) }})
}
