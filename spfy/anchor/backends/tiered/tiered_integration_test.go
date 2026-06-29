package tiered_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/otpki/otpki-pkg/spfy/anchor"
	"github.com/otpki/otpki-pkg/spfy/anchor/anchortest"
	"github.com/otpki/otpki-pkg/spfy/anchor/backends/memory"
	"github.com/otpki/otpki-pkg/spfy/anchor/backends/postgres"
	"github.com/otpki/otpki-pkg/spfy/anchor/backends/redis"
	"github.com/otpki/otpki-pkg/spfy/anchor/backends/ristretto"
	redisclient "github.com/redis/go-redis/v9"
	"github.com/samber/oops"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

var fixture struct {
	postgresOnce sync.Once
	postgresPool *pgxpool.Pool
	postgresCtr  testcontainers.Container
	postgresErr  error

	redisOnce   sync.Once
	redisClient *redisclient.Client
	redisCtr    testcontainers.Container
	redisErr    error
}

func TestMain(m *testing.M) {
	code := m.Run()

	if fixture.redisClient != nil {
		_ = fixture.redisClient.Close()
	}
	if fixture.redisCtr != nil {
		_ = testcontainers.TerminateContainer(fixture.redisCtr)
	}

	if fixture.postgresPool != nil {
		fixture.postgresPool.Close()
	}
	if fixture.postgresCtr != nil {
		_ = testcontainers.TerminateContainer(fixture.postgresCtr)
	}

	os.Exit(code)
}

func TestConformance(t *testing.T) {
	anchortest.RunConformance(t, tieredFactory())
}

func TestPostgresSurvivesEphemeralTierLossAndRepopulatesCaches(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	table := fmt.Sprintf("statestore_%s", safeName(t.Name()))

	// First tiered store:
	// memory -> ristretto -> redis prefix A -> postgres table.
	original, _, _, _, err := newTieredStoreWithNames(
		t,
		table,
		"statestore:"+safeName(t.Name())+":redis-a:",
	)
	if err != nil {
		t.Fatal(err)
	}

	written, err := original.Put(
		ctx,
		"session/1",
		anchortest.StateValue{Name: "persisted", Count: 42},
		anchor.WriteOptions{
			Metadata: anchor.Metadata{"kind": []string{"session"}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	// Second tiered store:
	// brand new memory, brand new ristretto, brand new Redis prefix,
	// same Postgres table.
	//
	// This simulates process-local tiers being lost and Redis being empty for
	// this new tiered instance. The only shared backend is Postgres.
	restarted, memoryTier, ristrettoTier, redisTier, err := newTieredStoreWithNames(
		t,
		table,
		"statestore:"+safeName(t.Name())+":redis-b:",
	)
	if err != nil {
		t.Fatal(err)
	}

	got, err := restarted.Get(ctx, "session/1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ETag != written.ETag {
		t.Fatalf("etag = %q, want %q", got.ETag, written.ETag)
	}
	if got.Value.Name != "persisted" || got.Value.Count != 42 {
		t.Fatalf("unexpected value: %+v", got.Value)
	}

	// The read came from Postgres and should have been promoted back up into
	// the faster tiers.
	assertEventuallyPromoted(t, memoryTier, "session/1", written)
	assertEventuallyPromoted(t, ristrettoTier, "session/1", written)
	assertEventuallyPromoted(t, redisTier, "session/1", written)
}

func tieredFactory() anchortest.Factory {
	return anchortest.Factory{
		Name: "tiered/memory_ristretto_redis_postgres",
		New: func(tb testing.TB) anchor.Store[anchortest.StateValue] {
			table := fmt.Sprintf("statestore_%s", safeName(tb.Name()))
			redisPrefix := "statestore:" + safeName(tb.Name()) + ":"

			store, _, _, _, err := newTieredStoreWithNames(tb, table, redisPrefix)
			if err != nil {
				tb.Fatal(err)
			}

			return store
		},
	}
}

func newTieredStoreWithNames(
	tb testing.TB,
	postgresTable string,
	redisPrefix string,
) (
	anchor.Store[anchortest.StateValue],
	anchor.Store[anchortest.StateValue],
	anchor.Store[anchortest.StateValue],
	anchor.Store[anchortest.StateValue],
	error,
) {
	tb.Helper()

	codec := anchor.JSONCodec[anchortest.StateValue]{}

	memoryStore := memory.New(memory.DefaultConfig(), codec)

	ristrettoStore, err := ristretto.New[anchortest.StateValue](
		ristretto.DefaultConfig(),
		codec,
	)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	redisConf := redis.DefaultConfig()
	redisConf.Prefix = redisPrefix
	redisStore := redis.New[anchortest.StateValue](
		redisClient(tb),
		redisConf,
		codec,
	)

	postgresConf := postgres.DefaultConfig()
	postgresConf.Table = postgresTable
	postgresStore, err := postgres.New[anchortest.StateValue](
		context.Background(),
		postgresPool(tb),
		postgresConf,
		codec,
	)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	tieredStore, err := anchor.NewTieredStore[anchortest.StateValue](
		memoryStore,
		ristrettoStore,
		redisStore,
		postgresStore,
	)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	return tieredStore, memoryStore, ristrettoStore, redisStore, nil
}

func postgresPool(tb testing.TB) *pgxpool.Pool {
	tb.Helper()

	fixture.postgresOnce.Do(func() {
		ctx := context.Background()

		ctr, err := tcpostgres.Run(
			ctx,
			"postgres:16-alpine",
			tcpostgres.WithDatabase("statestore"),
			tcpostgres.WithUsername("statestore"),
			tcpostgres.WithPassword("statestore"),
			tcpostgres.BasicWaitStrategies(),
		)
		if err != nil {
			fixture.postgresErr = err
			return
		}

		connStr, err := ctr.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			_ = testcontainers.TerminateContainer(ctr)
			fixture.postgresErr = err
			return
		}

		pool, err := pgxpool.New(ctx, connStr)
		if err != nil {
			_ = testcontainers.TerminateContainer(ctr)
			fixture.postgresErr = err
			return
		}

		fixture.postgresCtr = ctr
		fixture.postgresPool = pool
	})

	if fixture.postgresErr != nil {
		tb.Fatal(fixture.postgresErr)
	}

	return fixture.postgresPool
}

func redisClient(tb testing.TB) *redisclient.Client {
	tb.Helper()

	fixture.redisOnce.Do(func() {
		ctx := context.Background()

		ctr, err := tcredis.Run(ctx, "redis:7")
		if err != nil {
			fixture.redisErr = err
			return
		}

		uri, err := ctr.ConnectionString(ctx)
		if err != nil {
			_ = testcontainers.TerminateContainer(ctr)
			fixture.redisErr = err
			return
		}

		opt, err := redisclient.ParseURL(uri)
		if err != nil {
			_ = testcontainers.TerminateContainer(ctr)
			fixture.redisErr = err
			return
		}

		fixture.redisCtr = ctr
		fixture.redisClient = redisclient.NewClient(opt)
	})

	if fixture.redisErr != nil {
		tb.Fatal(fixture.redisErr)
	}

	return fixture.redisClient
}

func safeName(name string) string {
	name = strings.NewReplacer(
		"/", "_",
		" ", "_",
		"-", "_",
		".", "_",
	).Replace(name)

	name = fmt.Sprintf("%s_%d", name, time.Now().UnixNano())

	if len(name) > 50 {
		return name[len(name)-50:]
	}

	return name
}

func assertEventuallyPromoted(
	tb testing.TB,
	store anchor.Store[anchortest.StateValue],
	key string,
	want anchor.Entry[anchortest.StateValue],
) {
	tb.Helper()

	deadline := time.Now().Add(2 * time.Second)
	var lastErr error

	for time.Now().Before(deadline) {
		got, err := store.Get(context.Background(), key)
		if err == nil &&
			got.Value == want.Value &&
			metadataContainsValue(
				got.Metadata,
				"anchor.internal/tiered-authoritative-etag",
				string(want.ETag),
			) {
			return
		}

		if err != nil {
			lastErr = err
		} else {
			lastErr = oops.Errorf(
				"value = %+v, local etag = %q, metadata = %+v; want value %+v with authoritative etag metadata %q",
				got.Value,
				got.ETag,
				got.Metadata,
				want.Value,
				want.ETag,
			)
		}

		time.Sleep(20 * time.Millisecond)
	}

	tb.Fatalf("key %q was not promoted to cache tier: %v", key, lastErr)
}

func metadataContainsValue(metadata anchor.Metadata, key string, value string) bool {
	for _, got := range metadata[key] {
		if got == value {
			return true
		}
	}
	return false
}
