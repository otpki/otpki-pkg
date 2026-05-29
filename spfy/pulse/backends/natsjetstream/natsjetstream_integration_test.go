package natsjetstream_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	nats "github.com/nats-io/nats.go"
	"github.com/otpki/otpki-pkg/spfy/pulse"
	"github.com/otpki/otpki-pkg/spfy/pulse/backends/natsjetstream"
	"github.com/otpki/otpki-pkg/spfy/pulse/pulsetest"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var natsFixture struct {
	once      sync.Once
	addr      string
	container testcontainers.Container
	err       error
}

func ipv4Localhost(host string) string {
	if host == "localhost" || host == "::1" || host == "[::1]" {
		return "127.0.0.1"
	}
	return host
}

func natsAddr(t testing.TB) string {
	t.Helper()
	natsFixture.once.Do(func() {
		ctx := context.Background()
		ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        "nats:2.14.1-alpine",
				Cmd:          []string{"-js"},
				ExposedPorts: []string{"4222/tcp"},
				WaitingFor:   wait.ForListeningPort("4222/tcp").WithStartupTimeout(30 * time.Second),
			},
			Started: true,
		})
		if err != nil {
			natsFixture.err = err
			return
		}
		host, err := ctr.Host(ctx)
		if err != nil {
			_ = ctr.Terminate(context.Background())
			natsFixture.err = err
			return
		}
		host = ipv4Localhost(host)
		port, err := ctr.MappedPort(ctx, "4222/tcp")
		if err != nil {
			_ = ctr.Terminate(context.Background())
			natsFixture.err = err
			return
		}
		natsFixture.container = ctr
		natsFixture.addr = fmt.Sprintf("nats://%s:%s", host, port.Port())
	})
	if natsFixture.err != nil {
		t.Fatal(natsFixture.err)
	}
	return natsFixture.addr
}

func TestMain(m *testing.M) {
	code := m.Run()
	if natsFixture.container != nil {
		_ = natsFixture.container.Terminate(context.Background())
	}
	os.Exit(code)
}

func TestIntegrationConformance(t *testing.T) {
	pulsetest.RunConformance(t, pulsetest.Factory{Name: "natsjetstream", New: func(tb testing.TB) pulse.Backend {
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
	}})
}
