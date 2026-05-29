package nonce_test

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	miniredissrv "github.com/alicebob/miniredis/v2/server"
	"github.com/nats-io/nats-server/v2/server"
	natsserver "github.com/nats-io/nats-server/v2/test"
	"github.com/otpki/otpki-pkg/config"
	otpkilog "github.com/otpki/otpki-pkg/log"
	"github.com/otpki/otpki-pkg/nonce"
	spfystate "github.com/otpki/otpki-pkg/spfy/anchor"
	anchormemory "github.com/otpki/otpki-pkg/spfy/anchor/backends/memory"
	spfydep "github.com/otpki/otpki-pkg/spfy/dep"
	"github.com/samber/do/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type nonceBackendFixture struct {
	cfg     config.Config
	store   spfystate.Store[[]byte]
	advance func(time.Duration)
}

func TestPubStateNonceManager(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(t *testing.T) nonceBackendFixture
	}{
		{
			name: "in memory",
			setup: func(t *testing.T) nonceBackendFixture {
				t.Helper()
				cfg := config.DefaultConfig()
				cfg.StateStore.Enabled = false
				return nonceBackendFixture{
					cfg:   cfg,
					store: newNonceBackendFromConfig(t, cfg),
				}
			},
		},
		{
			name: "ristretto",
			setup: func(t *testing.T) nonceBackendFixture {
				t.Helper()
				cfg := config.DefaultConfig()
				cfg.StateStore.Enabled = true
				cfg.StateStore.Backend = config.StateStoreBackendRistretto
				return nonceBackendFixture{
					cfg:   cfg,
					store: newNonceBackendFromConfig(t, cfg),
				}
			},
		},
		{
			name: "redis",
			setup: func(t *testing.T) nonceBackendFixture {
				t.Helper()
				mr, err := miniredis.Run()
				require.NoError(t, err)
				t.Cleanup(mr.Close)

				mr.Server().SetPreHook(func(c *miniredissrv.Peer, cmd string, args ...string) bool {
					if cmd == "INFO" && len(args) == 1 && strings.EqualFold(args[0], "replication") {
						c.WriteBulk("# Replication\nrole:master\r\nconnected_slaves:0\r\n")
						return true
					}
					return false
				})

				cfg := config.DefaultConfig()
				cfg.StateStore.Enabled = true
				cfg.StateStore.Backend = config.StateStoreBackendRedis
				cfg.Backends.Redis.Addrs = []string{mr.Addr()}
				cfg.StateStore.Redis.Prefix = sanitizedNonceTestName(t)
				return nonceBackendFixture{
					cfg:     cfg,
					store:   newNonceBackendFromConfig(t, cfg),
					advance: mr.FastForward,
				}
			},
		},
		{
			name: "jetstream",
			setup: func(t *testing.T) nonceBackendFixture {
				t.Helper()
				srv := newJetStreamServer(t)
				cfg := config.DefaultConfig()
				cfg.StateStore.Enabled = true
				cfg.StateStore.Backend = config.StateStoreBackendNATSJetStream
				cfg.Backends.NATS.URL = srv.ClientURL()
				cfg.StateStore.NATSJetStream.StreamName = sanitizedNonceTestName(t)
				return nonceBackendFixture{
					cfg:   cfg,
					store: newNonceBackendFromConfig(t, cfg),
				}
			},
		},
		{
			name: "nats core",
			setup: func(t *testing.T) nonceBackendFixture {
				t.Helper()
				srv := newJetStreamServer(t)
				cfg := config.DefaultConfig()
				cfg.StateStore.Enabled = true
				cfg.StateStore.Backend = config.StateStoreBackendNATSCore
				cfg.Backends.NATS.URL = srv.ClientURL()
				cfg.StateStore.NATSCore.Subject = sanitizedNonceTestName(t)
				return nonceBackendFixture{
					cfg:   cfg,
					store: newNonceBackendFromConfig(t, cfg),
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := tt.setup(t)
			manager := nonce.NewPubStateNonceManager(fixture.cfg, fixture.store)
			runNonceManagerContractTests(t, manager)
			runNonceManagerTTLTests(t, fixture.cfg, fixture.store, fixture.advance)
			runNonceManagerTTLReinsertTests(t, fixture.cfg, fixture.store, fixture.advance)
		})
	}
}

func TestPubStateNonceManagerUsesAppKeyspacePrefix(t *testing.T) {
	t.Parallel()
	backend := newRecordingNonceStore()
	cfg := config.DefaultConfig()
	manager := nonce.NewPubStateNonceManager(cfg, backend)

	ctx := t.Context()
	const logicalKey = "logical-nonce"
	expectedKey := cfg.App.Keyspace.Key("nonces", logicalKey)

	_, err := manager.AddNonce(ctx, logicalKey)
	require.NoError(t, err)
	require.Equal(t, expectedKey, backend.lastSetKey)

	ok, err := manager.CheckNonce(ctx, logicalKey)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, expectedKey, backend.lastGetKey)

	added, err := manager.AddUniqueNonce(ctx, "unique-nonce")
	require.NoError(t, err)
	require.True(t, added)
	require.Equal(t, cfg.App.Keyspace.Key("nonces", "unique-nonce"), backend.lastSetKey)
}

func runNonceManagerContractTests(t *testing.T, manager nonce.NonceManager) {
	t.Helper()
	ctx := t.Context()

	n1, err := manager.CreateNonce(ctx)
	require.NoError(t, err)
	n2, err := manager.CreateNonce(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, n1)
	require.NotEmpty(t, n2)
	assert.NotEqual(t, n1, n2)

	ok, err := manager.CheckNonce(ctx, n1)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = manager.CheckNonce(ctx, n1)
	require.NoError(t, err)
	assert.False(t, ok)

	ok, err = manager.CheckNonce(ctx, n2)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = manager.CheckNonce(ctx, n2)
	require.NoError(t, err)
	assert.False(t, ok)

	const manual = "manual-nonce"
	added, err := manager.AddUniqueNonce(ctx, manual)
	require.NoError(t, err)
	assert.True(t, added)

	added, err = manager.AddUniqueNonce(ctx, manual)
	require.NoError(t, err)
	assert.False(t, added)

	ok, err = manager.CheckNonce(ctx, manual)
	require.NoError(t, err)
	assert.True(t, ok)

	const explicit = "explicit-nonce"
	stored, err := manager.AddNonce(ctx, explicit)
	require.NoError(t, err)
	assert.Equal(t, explicit, stored)

	ok, err = manager.CheckNonce(ctx, explicit)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = manager.CheckNonce(ctx, "missing-nonce")
	require.NoError(t, err)
	assert.False(t, ok)

	const concurrent = "concurrent-nonce"
	stored, err = manager.AddNonce(ctx, concurrent)
	require.NoError(t, err)
	assert.Equal(t, concurrent, stored)

	results := make(chan bool, 8)
	errs := make(chan error, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := manager.CheckNonce(ctx, concurrent)
			if err != nil {
				errs <- err
				return
			}
			results <- ok
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	consumed := 0
	for ok := range results {
		if ok {
			consumed++
		}
	}
	assert.Equal(t, 1, consumed)

	ok, err = manager.CheckNonce(ctx, concurrent)
	require.NoError(t, err)
	assert.False(t, ok)
}

func runNonceManagerTTLTests(t *testing.T, cfg config.Config, backend spfystate.Store[[]byte], advance func(time.Duration)) {
	t.Helper()
	ctx := t.Context()
	manager := nonce.NewPubStateNonceManagerWithTTL(cfg, backend, 2*time.Second)

	const ttlNonce = "ttl-nonce"
	stored, err := manager.AddNonce(ctx, ttlNonce)
	require.NoError(t, err)
	assert.Equal(t, ttlNonce, stored)

	advanceOrWaitTTL(advance, 3*time.Second)

	ok, err := manager.CheckNonce(ctx, ttlNonce)
	require.NoError(t, err)
	assert.False(t, ok)
}

func runNonceManagerTTLReinsertTests(
	t *testing.T,
	cfg config.Config,
	backend spfystate.Store[[]byte],
	advance func(time.Duration),
) {
	t.Helper()

	ctx := t.Context()
	manager := nonce.NewPubStateNonceManagerWithTTL(cfg, backend, 2*time.Second)

	const ttlUniqueNonce = "ttl-unique-nonce"
	added, err := manager.AddUniqueNonce(ctx, ttlUniqueNonce)
	require.NoError(t, err)
	assert.True(t, added)

	added, err = manager.AddUniqueNonce(ctx, ttlUniqueNonce)
	require.NoError(t, err)
	assert.False(t, added)

	advanceOrWaitTTL(advance, 3*time.Second)

	added, err = manager.AddUniqueNonce(ctx, ttlUniqueNonce)
	require.NoError(t, err)
	assert.True(t, added)

	ok, err := manager.CheckNonce(ctx, ttlUniqueNonce)
	require.NoError(t, err)
	assert.True(t, ok)
}

func advanceOrWaitTTL(advance func(time.Duration), d time.Duration) {
	if advance != nil {
		advance(d)
	}
	time.Sleep(d)
}

func newNonceBackendFromConfig(t *testing.T, cfg config.Config) spfystate.Store[[]byte] {
	t.Helper()
	return mustNewNonceStore(t, cfg)
}

func newJetStreamServer(t *testing.T) *server.Server {
	t.Helper()
	opts := natsserver.DefaultTestOptions
	opts.Port = -1
	opts.JetStream = true
	opts.StoreDir = t.TempDir()

	srv := natsserver.RunServer(&opts)
	t.Cleanup(srv.Shutdown)
	return srv
}

func sanitizedNonceTestName(t *testing.T) string {
	t.Helper()
	return strings.NewReplacer("/", "-", " ", "-").Replace("nonce-test-" + t.Name())
}

type recordingNonceStore struct {
	store         spfystate.Store[[]byte]
	lastSetKey    string
	lastGetKey    string
	lastDeleteKey string
}

func newRecordingNonceStore() *recordingNonceStore {
	return &recordingNonceStore{
		store: anchormemory.New(anchormemory.DefaultConfig(), passthroughCodec{}),
	}
}

func (r *recordingNonceStore) Get(ctx context.Context, key string) (spfystate.Entry[[]byte], error) {
	r.lastGetKey = key
	return r.store.Get(ctx, key)
}

func (r *recordingNonceStore) Put(ctx context.Context, key string, value []byte, opts spfystate.WriteOptions) (spfystate.Entry[[]byte], error) {
	r.lastSetKey = key
	return r.store.Put(ctx, key, value, opts)
}

func (r *recordingNonceStore) Create(ctx context.Context, key string, value []byte, opts spfystate.WriteOptions) (spfystate.Entry[[]byte], error) {
	r.lastSetKey = key
	return r.store.Create(ctx, key, value, opts)
}

func (r *recordingNonceStore) CompareAndSet(ctx context.Context, key string, expected spfystate.ETag, value []byte, opts spfystate.WriteOptions) (spfystate.Entry[[]byte], error) {
	r.lastSetKey = key
	return r.store.CompareAndSet(ctx, key, expected, value, opts)
}

func (r *recordingNonceStore) AppendMetadata(ctx context.Context, key string, metadata spfystate.Metadata) (spfystate.Entry[[]byte], error) {
	return r.store.AppendMetadata(ctx, key, metadata)
}

func (r *recordingNonceStore) Delete(ctx context.Context, key string) error {
	r.lastDeleteKey = key
	return r.store.Delete(ctx, key)
}

func (r *recordingNonceStore) Consume(ctx context.Context, key string) (spfystate.Entry[[]byte], error) {
	return r.store.Consume(ctx, key)
}

func (r *recordingNonceStore) Query(ctx context.Context, query spfystate.Query) (spfystate.QueryResult[[]byte], error) {
	return r.store.Query(ctx, query)
}

func (r *recordingNonceStore) Watch(ctx context.Context, opts spfystate.WatchOptions) (<-chan spfystate.Event[[]byte], error) {
	return r.store.Watch(ctx, opts)
}

func (r *recordingNonceStore) Batch(ctx context.Context, ops []spfystate.Operation[[]byte]) ([]spfystate.Entry[[]byte], error) {
	return r.store.Batch(ctx, ops)
}

func mustNewNonceStore(t *testing.T, cfg config.Config) spfystate.Store[[]byte] {
	t.Helper()

	injector := do.New(spfydep.Package)
	do.ProvideValue(injector, t.Context())
	do.ProvideValue(injector, cfg)
	do.ProvideValue(injector, otpkilog.GetDefaultLogger(io.Discard, nil))

	store := do.MustInvoke[spfystate.Store[[]byte]](injector)
	t.Cleanup(func() {
		_ = injector.Shutdown()
	})
	return store
}

type passthroughCodec struct{}

func (passthroughCodec) Marshal(v []byte) ([]byte, error)   { return append([]byte(nil), v...), nil }
func (passthroughCodec) Unmarshal(v []byte) ([]byte, error) { return append([]byte(nil), v...), nil }
