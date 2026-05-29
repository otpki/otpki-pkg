package redis_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otpki/otpki-pkg/spfy/anchor"
	"github.com/otpki/otpki-pkg/spfy/anchor/anchortest"
	"github.com/otpki/otpki-pkg/spfy/anchor/backends/redis"
	redisclient "github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

var redisFixture struct {
	once      sync.Once
	client    *redisclient.Client
	container testcontainers.Container
	err       error
}

func redisClient(t testing.TB) *redisclient.Client {
	t.Helper()
	redisFixture.once.Do(func() {
		ctx := context.Background()
		ctr, err := tcredis.Run(ctx, "redis:7")
		if err != nil {
			redisFixture.err = err
			return
		}
		uri, err := ctr.ConnectionString(ctx)
		if err != nil {
			_ = testcontainers.TerminateContainer(ctr)
			redisFixture.err = err
			return
		}
		opt, err := redisclient.ParseURL(uri)
		if err != nil {
			_ = testcontainers.TerminateContainer(ctr)
			redisFixture.err = err
			return
		}
		client := redisclient.NewClient(opt)
		redisFixture.container = ctr
		redisFixture.client = client
	})
	if redisFixture.err != nil {
		t.Fatal(redisFixture.err)
	}
	return redisFixture.client
}

func TestMain(m *testing.M) {
	code := m.Run()
	if redisFixture.client != nil {
		redisFixture.client.Close()
	}
	if redisFixture.container != nil {
		_ = testcontainers.TerminateContainer(redisFixture.container)
	}
	os.Exit(code)
}

func safeName(name string) string {
	name = strings.NewReplacer("/", "_", " ", "_", "-", "_", ".", "_").Replace(name)
	name = fmt.Sprintf("%s%d", name, time.Now().UnixNano())
	if len(name) > 50 {
		return name[len(name)-50:]
	}
	return name
}

func TestConformance(t *testing.T) {
	anchortest.RunConformance(t, anchortest.Factory{
		Name: "redis",
		New: func(tb testing.TB) anchor.Store[anchortest.StateValue] {
			client := redisClient(tb)
			conf := redis.DefaultConfig()
			conf.Prefix = "statestore:" + safeName(tb.Name())
			return redis.New[anchortest.StateValue](client, conf, anchor.JSONCodec[anchortest.StateValue]{})
		},
	})
}

func TestStress(t *testing.T) {
	anchortest.RunStressWithOptions(t, anchortest.Factory{
		Name: "redis",
		New: func(tb testing.TB) anchor.Store[anchortest.StateValue] {
			client := redisClient(tb)
			conf := redis.DefaultConfig()
			conf.Prefix = "statestore:" + safeName(tb.Name())
			return redis.New[anchortest.StateValue](client, conf, anchor.JSONCodec[anchortest.StateValue]{})
		},
	}, anchortest.StressOptions{Workers: 16, OperationsPerWorker: 100})
}
