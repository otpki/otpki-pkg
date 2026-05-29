package natsjetstream_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/otpki/otpki-pkg/spfy/anchor"
	"github.com/otpki/otpki-pkg/spfy/anchor/anchortest"
	"github.com/otpki/otpki-pkg/spfy/anchor/backends/natsjetstream"
	"github.com/testcontainers/testcontainers-go"
	tcnats "github.com/testcontainers/testcontainers-go/modules/nats"
)

var natsFixture struct {
	once      sync.Once
	nc        *natsgo.Conn
	container testcontainers.Container
	err       error
}

func natsConn(t testing.TB) *natsgo.Conn {
	t.Helper()
	natsFixture.once.Do(func() {
		ctx := context.Background()
		ctr, err := tcnats.Run(
			ctx,
			"nats:2.12-alpine",
			tcnats.WithConfigFile(strings.NewReader("jetstream: enabled\n")),
		)
		if err != nil {
			natsFixture.err = err
			return
		}
		uri, err := ctr.ConnectionString(ctx)
		if err != nil {
			_ = testcontainers.TerminateContainer(ctr)
			natsFixture.err = err
			return
		}
		nc, err := natsgo.Connect(uri)
		if err != nil {
			_ = testcontainers.TerminateContainer(ctr)
			natsFixture.err = err
			return
		}
		natsFixture.container = ctr
		natsFixture.nc = nc
	})
	if natsFixture.err != nil {
		t.Fatal(natsFixture.err)
	}
	return natsFixture.nc
}

func TestMain(m *testing.M) {
	code := m.Run()
	if natsFixture.nc != nil {
		natsFixture.nc.Close()
	}
	if natsFixture.container != nil {
		_ = testcontainers.TerminateContainer(natsFixture.container)
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
		Name: "natsjetstream",
		New: func(tb testing.TB) anchor.Store[anchortest.StateValue] {
			ctx := context.Background()
			nc := natsConn(tb)
			conf := natsjetstream.DefaultConfig()
			conf.SubjectPrefix = "statestore." + safeName(tb.Name())
			store, err := natsjetstream.New[anchortest.StateValue](
				ctx,
				nc,
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
		Name: "natsjetstream",
		New: func(tb testing.TB) anchor.Store[anchortest.StateValue] {
			ctx := context.Background()
			nc := natsConn(tb)
			conf := natsjetstream.DefaultConfig()
			conf.SubjectPrefix = "statestore." + safeName(tb.Name())
			store, err := natsjetstream.New[anchortest.StateValue](
				ctx,
				nc,
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
