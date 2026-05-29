package rabbitmq_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/otpki/otpki-pkg/spfy/pulse"
	"github.com/otpki/otpki-pkg/spfy/pulse/backends/rabbitmq"
	"github.com/otpki/otpki-pkg/spfy/pulse/pulsetest"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var rabbitFixture struct {
	once      sync.Once
	url       string
	container testcontainers.Container
	err       error
}

func ipv4Localhost(host string) string {
	if host == "localhost" || host == "::1" || host == "[::1]" {
		return "127.0.0.1"
	}
	return host
}

func rabbitURL(t testing.TB) string {
	t.Helper()
	rabbitFixture.once.Do(func() {
		ctx := context.Background()
		ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        "rabbitmq:3.13-alpine",
				ExposedPorts: []string{"5672/tcp"},
				WaitingFor: wait.ForAll(
					wait.ForListeningPort("5672/tcp"),
					wait.ForLog("Server startup complete"),
				).WithStartupTimeout(90 * time.Second),
			},
			Started: true,
		})
		if err != nil {
			rabbitFixture.err = err
			return
		}
		host, err := ctr.Host(ctx)
		if err != nil {
			_ = ctr.Terminate(context.Background())
			rabbitFixture.err = err
			return
		}
		host = ipv4Localhost(host)
		port, err := ctr.MappedPort(ctx, "5672/tcp")
		if err != nil {
			_ = ctr.Terminate(context.Background())
			rabbitFixture.err = err
			return
		}
		rabbitFixture.container = ctr
		rabbitFixture.url = fmt.Sprintf("amqp://guest:guest@%s:%s/", host, port.Port())
	})
	if rabbitFixture.err != nil {
		t.Fatal(rabbitFixture.err)
	}
	return rabbitFixture.url
}

func dialRabbit(tb testing.TB, url string) *amqp.Connection {
	tb.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		conn, err := amqp.Dial(url)
		if err == nil {
			return conn
		}
		last = err
		time.Sleep(200 * time.Millisecond)
	}
	tb.Fatal(last)
	return nil
}

func TestMain(m *testing.M) {
	code := m.Run()
	if rabbitFixture.container != nil {
		_ = rabbitFixture.container.Terminate(context.Background())
	}
	os.Exit(code)
}

func TestIntegrationConformance(t *testing.T) {
	pulsetest.RunConformance(t, pulsetest.Factory{Name: "rabbitmq", New: func(tb testing.TB) pulse.Backend {
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
	}, AfterSubscribe: func(testing.TB) { time.Sleep(300 * time.Millisecond) }})
}
