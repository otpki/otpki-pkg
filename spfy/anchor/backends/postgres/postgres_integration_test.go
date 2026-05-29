package postgres_test

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
	"github.com/otpki/otpki-pkg/spfy/anchor/backends/postgres"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

var postgresFixture struct {
	once      sync.Once
	pool      *pgxpool.Pool
	container testcontainers.Container
	err       error
}

func postgresPool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	postgresFixture.once.Do(func() {
		ctx := context.Background()
		ctr, err := tcpostgres.Run(ctx,
			"postgres:16-alpine",
			tcpostgres.WithDatabase("statestore"),
			tcpostgres.WithUsername("statestore"),
			tcpostgres.WithPassword("statestore"),
			tcpostgres.BasicWaitStrategies(),
		)
		if err != nil {
			postgresFixture.err = err
			return
		}
		connStr, err := ctr.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			_ = testcontainers.TerminateContainer(ctr)
			postgresFixture.err = err
			return
		}
		pool, err := pgxpool.New(ctx, connStr)
		if err != nil {
			_ = testcontainers.TerminateContainer(ctr)
			postgresFixture.err = err
			return
		}
		postgresFixture.container = ctr
		postgresFixture.pool = pool
	})
	if postgresFixture.err != nil {
		t.Fatal(postgresFixture.err)
	}
	return postgresFixture.pool
}

func TestMain(m *testing.M) {
	code := m.Run()
	if postgresFixture.pool != nil {
		postgresFixture.pool.Close()
	}
	if postgresFixture.container != nil {
		_ = testcontainers.TerminateContainer(postgresFixture.container)
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
		Name: "postgres",
		New: func(tb testing.TB) anchor.Store[anchortest.StateValue] {
			ctx := context.Background()
			pool := postgresPool(tb)
			conf := postgres.DefaultConfig()
			conf.Table = fmt.Sprintf("statestore_%s", safeName(tb.Name()))
			store, err := postgres.New[anchortest.StateValue](
				ctx,
				pool,
				conf,
				anchor.JSONCodec[anchortest.StateValue]{},
			)
			if err != nil {
				tb.Fatal(err)
			}
			return store
		},
	})
}

func TestStress(t *testing.T) {
	anchortest.RunStressWithOptions(t, anchortest.Factory{
		Name: "postgres",
		New: func(tb testing.TB) anchor.Store[anchortest.StateValue] {
			ctx := context.Background()
			pool := postgresPool(tb)
			conf := postgres.DefaultConfig()
			conf.Table = fmt.Sprintf("statestore_%s", safeName(tb.Name()))
			store, err := postgres.New[anchortest.StateValue](
				ctx,
				pool,
				conf,
				anchor.JSONCodec[anchortest.StateValue]{},
			)
			if err != nil {
				tb.Fatal(err)
			}
			return store
		},
	}, anchortest.StressOptions{Workers: 16, OperationsPerWorker: 100})
}
