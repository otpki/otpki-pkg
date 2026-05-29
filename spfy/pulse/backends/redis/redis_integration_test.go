package redis_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/otpki/otpki-pkg/spfy/pulse"
	pulsedis "github.com/otpki/otpki-pkg/spfy/pulse/backends/redis"
	"github.com/otpki/otpki-pkg/spfy/pulse/pulsetest"
	goredis "github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var redisFixture struct {
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

func redisAddr(t testing.TB) string {
	t.Helper()
	redisFixture.once.Do(func() {
		ctx := context.Background()
		ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        "redis:7-alpine",
				ExposedPorts: []string{"6379/tcp"},
				WaitingFor:   wait.ForListeningPort("6379/tcp").WithStartupTimeout(30 * time.Second),
			},
			Started: true,
		})
		if err != nil {
			redisFixture.err = err
			return
		}
		host, err := ctr.Host(ctx)
		if err != nil {
			_ = ctr.Terminate(context.Background())
			redisFixture.err = err
			return
		}
		host = ipv4Localhost(host)
		port, err := ctr.MappedPort(ctx, "6379/tcp")
		if err != nil {
			_ = ctr.Terminate(context.Background())
			redisFixture.err = err
			return
		}
		redisFixture.container = ctr
		redisFixture.addr = fmt.Sprintf("%s:%s", host, port.Port())
	})
	if redisFixture.err != nil {
		t.Fatal(redisFixture.err)
	}
	return redisFixture.addr
}

func waitRedis(tb testing.TB, client *goredis.Client) {
	tb.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		if err := client.Ping(context.Background()).Err(); err == nil {
			return
		} else {
			last = err
		}
		time.Sleep(100 * time.Millisecond)
	}
	tb.Fatal(last)
}

func TestMain(m *testing.M) {
	code := m.Run()
	if redisFixture.container != nil {
		_ = redisFixture.container.Terminate(context.Background())
	}
	os.Exit(code)
}

func TestIntegrationConformance(t *testing.T) {
	pulsetest.RunConformance(t, pulsetest.Factory{Name: "redis", New: func(tb testing.TB) pulse.Backend {
		client := goredis.NewClient(&goredis.Options{Addr: redisAddr(tb)})
		waitRedis(tb, client)
		tb.Cleanup(func() { _ = client.Close() })
		b, err := pulsedis.New(client, pulsedis.Config{KeyPrefix: "test:" + pulse.SafeName(tb.Name())})
		if err != nil {
			tb.Fatal(err)
		}
		return b
	}})
}
