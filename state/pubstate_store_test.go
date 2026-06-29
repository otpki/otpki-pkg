package state_test

import (
	"context"
	"fmt"
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
	spfystate "github.com/otpki/otpki-pkg/spfy/anchor"
	spfydep "github.com/otpki/otpki-pkg/spfy/dep"
	"github.com/otpki/otpki-pkg/state"
	"github.com/samber/do/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPubStateStoreContract(t *testing.T) {
	t.Parallel()
	t.Run("in-memory", func(t *testing.T) {
		backend := newPubStateBackend(t, func(cfg *config.Config) {
			cfg.StateStore.Enabled = false
		})
		runPubStateStoreContractTests(t, func() state.Store[string, string] {
			return state.NewPubStateStore[string](config.DefaultConfig(), backend, nil)
		})
	})

	t.Run("redis/miniredis", func(t *testing.T) {
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

		backend := newPubStateBackend(t, func(cfg *config.Config) {
			cfg.StateStore.Enabled = true
			cfg.StateStore.Backends = []config.StateStoreBackend{config.StateStoreBackendRedis}
			cfg.Backends.Redis.Addrs = []string{mr.Addr()}
		})

		runPubStateStoreContractTests(t, func() state.Store[string, string] {
			return state.NewPubStateStore[string](config.DefaultConfig(), backend, nil)
		})
	})

	t.Run("jetstream/nats", func(t *testing.T) {
		srv := newJetStreamServer(t)

		backend := newPubStateBackend(t, func(cfg *config.Config) {
			cfg.StateStore.Enabled = true
			cfg.StateStore.Backends = []config.StateStoreBackend{config.StateStoreBackendNATSJetStream}
			cfg.Backends.NATS.URL = srv.ClientURL()
		})

		runPubStateStoreContractTests(t, func() state.Store[string, string] {
			return state.NewPubStateStore[string](config.DefaultConfig(), backend, nil)
		})
	})

	t.Run("nats/core", func(t *testing.T) {
		srv := newJetStreamServer(t)

		backend := newPubStateBackend(t, func(cfg *config.Config) {
			cfg.StateStore.Enabled = true
			cfg.StateStore.Backends = []config.StateStoreBackend{config.StateStoreBackendNATSCore}
			cfg.Backends.NATS.URL = srv.ClientURL()
		})

		runPubStateStoreContractTests(t, func() state.Store[string, string] {
			return state.NewPubStateStore[string](config.DefaultConfig(), backend, nil)
		})
	})

	t.Run("ristretto", func(t *testing.T) {
		backend := newPubStateBackend(t, func(cfg *config.Config) {
			cfg.StateStore.Enabled = true
			cfg.StateStore.Backends = []config.StateStoreBackend{config.StateStoreBackendRistretto}
		})

		runPubStateStoreContractTests(t, func() state.Store[string, string] {
			return state.NewPubStateStore[string](config.DefaultConfig(), backend, nil)
		})
	})
}

func TestPubStateStoreIntegerKeys(t *testing.T) {
	t.Parallel()
	backend := newPubStateBackend(t, func(cfg *config.Config) {
		cfg.StateStore.Enabled = false
	})

	store := state.NewPubStateStoreWithKey[int, string](
		config.DefaultConfig(),
		backend,
		func() int { return 12345 },
		nil,
	)

	ctx := t.Context()
	require.NoError(t, store.PutWithKey(ctx, 7, "with-int-key", 250*time.Millisecond, 0))

	got, err := store.Get(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, "with-int-key", got)

	require.NoError(t, store.Delete(ctx, 7))
	_, err = store.Get(ctx, 7)
	require.Error(t, err)
}

func TestPubStateStoreUsesAppKeyspacePrefix(t *testing.T) {
	t.Parallel()
	backend := newRecordingPubStateStore()
	cfg := config.DefaultConfig()
	store := state.NewPubStateStoreWithKey[string, string](cfg, backend, func() string { return "generated-key" }, nil)

	ctx := t.Context()
	require.NoError(t, store.PutWithKey(ctx, "logical-key", "value", 0, 0))
	require.Equal(t, cfg.App.Keyspace.Key("logical-key"), backend.lastSetKey)

	got, err := store.Get(ctx, "logical-key")
	require.NoError(t, err)
	require.Equal(t, "value", got)
	require.Equal(t, cfg.App.Keyspace.Key("logical-key"), backend.lastGetKey)

	require.NoError(t, store.Delete(ctx, "logical-key"))
	require.Equal(t, cfg.App.Keyspace.Key("logical-key"), backend.lastDeleteKey)
}

func runPubStateStoreContractTests[V comparable](t *testing.T, newStore func() state.Store[string, V]) {
	t.Helper()
	ctx := t.Context()

	t.Run("Put/Get returns stored data", func(t *testing.T) {
		t.Parallel()
		s := newStore()

		want := anyToPubStateT[V](t, "hello")
		key, err := s.Put(ctx, want, 250*time.Millisecond, 0)
		require.NoError(t, err)
		require.NotEmpty(t, key)

		got, err := s.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	})

	t.Run("Delete removes key", func(t *testing.T) {
		t.Parallel()
		s := newStore()

		key, err := s.Put(ctx, anyToPubStateT[V](t, "bye"), 250*time.Millisecond, 0)
		require.NoError(t, err)
		require.NotEmpty(t, key)

		require.NoError(t, s.Delete(ctx, key))

		_, err = s.Get(ctx, key)
		require.Error(t, err)
	})

	t.Run("PeekMultiple returns data for provided keys in order", func(t *testing.T) {
		t.Parallel()
		s := newStore()

		first, err := s.Put(ctx, anyToPubStateT[V](t, "first"), 250*time.Millisecond, 0)
		require.NoError(t, err)
		second, err := s.Put(ctx, anyToPubStateT[V](t, "second"), 250*time.Millisecond, 0)
		require.NoError(t, err)

		got, err := s.PeekMultiple(ctx, []string{second, first})
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, anyToPubStateT[V](t, "second"), got[0])
		assert.Equal(t, anyToPubStateT[V](t, "first"), got[1])
	})

	t.Run("Peek does not renew session", func(t *testing.T) {
		t.Parallel()
		s := newStore()

		ttl := 80 * time.Millisecond
		key, err := s.Put(ctx, anyToPubStateT[V](t, "peek"), ttl, 0)
		require.NoError(t, err)

		time.Sleep(50 * time.Millisecond)
		_, err = s.Peek(ctx, key)
		require.NoError(t, err)

		time.Sleep(40 * time.Millisecond)
		_, err = s.Get(ctx, key)
		require.Error(t, err)
	})

	t.Run("TTL expiry causes Get to error", func(t *testing.T) {
		t.Parallel()
		s := newStore()

		key, err := s.Put(ctx, anyToPubStateT[V](t, "short"), 40*time.Millisecond, 0)
		require.NoError(t, err)
		require.NotEmpty(t, key)

		time.Sleep(60 * time.Millisecond)
		_, err = s.Get(ctx, key)
		require.Error(t, err)
	})

	t.Run("TTL=0 means no TTL expiry (no maxTTL)", func(t *testing.T) {
		t.Parallel()
		s := newStore()

		want := anyToPubStateT[V](t, "forever")
		key, err := s.Put(ctx, want, 0, 0)
		require.NoError(t, err)
		require.NotEmpty(t, key)

		time.Sleep(50 * time.Millisecond)

		got, err := s.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	})

	t.Run("TTL=0 with maxTTL expires at maxTTL", func(t *testing.T) {
		t.Parallel()
		s := newStore()

		key, err := s.Put(ctx, anyToPubStateT[V](t, "maxed"), 0, 40*time.Millisecond)
		require.NoError(t, err)
		require.NotEmpty(t, key)

		time.Sleep(60 * time.Millisecond)
		_, err = s.Get(ctx, key)
		require.Error(t, err)
	})

	t.Run("sliding renewal extends session when >50% TTL elapsed", func(t *testing.T) {
		t.Parallel()
		s := newStore()

		ttl := 80 * time.Millisecond
		key, err := s.Put(ctx, anyToPubStateT[V](t, "slide"), ttl, 0)
		require.NoError(t, err)
		require.NotEmpty(t, key)

		time.Sleep(50 * time.Millisecond)

		_, err = s.Get(ctx, key)
		require.NoError(t, err)

		time.Sleep(40 * time.Millisecond)

		_, err = s.Get(ctx, key)
		require.NoError(t, err)
	})

	t.Run("sliding renewal does not extend beyond maxTTL", func(t *testing.T) {
		t.Parallel()
		s := newStore()

		ttl := 80 * time.Millisecond
		maxTTL := 120 * time.Millisecond
		key, err := s.Put(ctx, anyToPubStateT[V](t, "cap"), ttl, maxTTL)
		require.NoError(t, err)
		require.NotEmpty(t, key)

		time.Sleep(50 * time.Millisecond)
		_, err = s.Get(ctx, key)
		require.NoError(t, err)

		time.Sleep(90 * time.Millisecond)

		_, err = s.Get(ctx, key)
		require.Error(t, err)
	})
}

func newPubStateBackend(t *testing.T, mutate func(*config.Config)) spfystate.Store[[]byte] {
	t.Helper()
	cfg := config.DefaultConfig()
	mutate(&cfg)
	injector := do.New(spfydep.Package)
	do.ProvideValue(injector, t.Context())
	do.ProvideValue(injector, cfg)
	do.ProvideValue(injector, otpkilog.GetDefaultLogger(io.Discard, nil))

	backend := do.MustInvoke[spfystate.Store[[]byte]](injector)
	t.Cleanup(func() { _ = injector.Shutdown() })
	return backend
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

func sanitizedTestName(t *testing.T) string {
	t.Helper()
	return strings.NewReplacer("/", "-", " ", "-").Replace("state-test-" + t.Name())
}

func anyToPubStateT[T comparable](t *testing.T, v any) T {
	t.Helper()
	typed, ok := v.(T)
	require.Truef(t, ok, "test uses T=%T but tried to assign value of type %T", *new(T), v)
	return typed
}

type recordingPubStateStore struct {
	mu            sync.Mutex
	values        map[string][]byte
	expiresAt     map[string]time.Time
	lastSetKey    string
	lastGetKey    string
	lastDeleteKey string
	nextETag      int64
}

func newRecordingPubStateStore() *recordingPubStateStore {
	return &recordingPubStateStore{
		values:    make(map[string][]byte),
		expiresAt: make(map[string]time.Time),
	}
}

func (r *recordingPubStateStore) Get(_ context.Context, key string) (spfystate.Entry[[]byte], error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.lastGetKey = key
	if r.expiredLocked(key, time.Now()) {
		delete(r.values, key)
		delete(r.expiresAt, key)
		return spfystate.Entry[[]byte]{}, spfystate.ErrNotFound
	}

	value, ok := r.values[key]
	if !ok {
		return spfystate.Entry[[]byte]{}, spfystate.ErrNotFound
	}

	return r.entryLocked(key, value), nil
}

func (r *recordingPubStateStore) Put(
	_ context.Context,
	key string,
	value []byte,
	opts spfystate.WriteOptions,
) (spfystate.Entry[[]byte], error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.lastSetKey = key
	r.values[key] = append([]byte(nil), value...)
	r.setExpiryLocked(key, opts.TTL)
	return r.entryLocked(key, r.values[key]), nil
}

func (r *recordingPubStateStore) Create(
	ctx context.Context,
	key string,
	value []byte,
	opts spfystate.WriteOptions,
) (spfystate.Entry[[]byte], error) {
	if _, err := r.Get(ctx, key); err == nil {
		return spfystate.Entry[[]byte]{}, spfystate.ErrAlreadyExists
	}
	return r.Put(ctx, key, value, opts)
}

func (r *recordingPubStateStore) CompareAndSet(
	ctx context.Context,
	key string,
	_ spfystate.ETag,
	value []byte,
	opts spfystate.WriteOptions,
) (spfystate.Entry[[]byte], error) {
	if _, err := r.Get(ctx, key); err != nil {
		return spfystate.Entry[[]byte]{}, err
	}
	return r.Put(ctx, key, value, opts)
}

func (r *recordingPubStateStore) AppendMetadata(
	ctx context.Context,
	key string,
	_ spfystate.Metadata,
) (spfystate.Entry[[]byte], error) {
	return r.Get(ctx, key)
}

func (r *recordingPubStateStore) Delete(_ context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.lastDeleteKey = key
	delete(r.values, key)
	delete(r.expiresAt, key)
	return nil
}

func (r *recordingPubStateStore) Consume(ctx context.Context, key string) (spfystate.Entry[[]byte], error) {
	entry, err := r.Get(ctx, key)
	if err != nil {
		return spfystate.Entry[[]byte]{}, err
	}
	return entry, r.Delete(ctx, key)
}

func (r *recordingPubStateStore) Query(
	_ context.Context,
	query spfystate.Query,
) (spfystate.QueryResult[[]byte], error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entries := make([]spfystate.Entry[[]byte], 0)
	for key, value := range r.values {
		if query.Prefix != "" && !strings.HasPrefix(key, query.Prefix) {
			continue
		}
		entries = append(entries, r.entryLocked(key, value))
		if query.Limit > 0 && len(entries) >= query.Limit {
			break
		}
	}

	return spfystate.QueryResult[[]byte]{Entries: entries}, nil
}

func (r *recordingPubStateStore) Watch(
	context.Context,
	spfystate.WatchOptions,
) (<-chan spfystate.Event[[]byte], error) {
	ch := make(chan spfystate.Event[[]byte])
	close(ch)
	return ch, nil
}

func (r *recordingPubStateStore) Batch(
	ctx context.Context,
	ops []spfystate.Operation[[]byte],
) ([]spfystate.Entry[[]byte], error) {
	entries := make([]spfystate.Entry[[]byte], 0, len(ops))
	for _, op := range ops {
		var (
			entry spfystate.Entry[[]byte]
			err   error
		)
		switch op.Type {
		case spfystate.OpPut:
			entry, err = r.Put(ctx, op.Key, op.Value, op.Options)
		case spfystate.OpCreate:
			entry, err = r.Create(ctx, op.Key, op.Value, op.Options)
		case spfystate.OpCompareAndSet:
			entry, err = r.CompareAndSet(ctx, op.Key, op.Expected, op.Value, op.Options)
		case spfystate.OpMetadataAppend:
			entry, err = r.AppendMetadata(ctx, op.Key, op.Metadata)
		case spfystate.OpConsume:
			entry, err = r.Consume(ctx, op.Key)
		case spfystate.OpDelete:
			err = r.Delete(ctx, op.Key)
		default:
			err = spfystate.ErrInvalidBatch
		}
		if err != nil {
			return nil, err
		}
		if op.Type != spfystate.OpDelete {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func (r *recordingPubStateStore) entryLocked(key string, value []byte) spfystate.Entry[[]byte] {
	r.nextETag++
	entry := spfystate.Entry[[]byte]{
		Key:   key,
		Value: append([]byte(nil), value...),
		ETag:  spfystate.ETag(fmt.Sprint(r.nextETag)),
	}
	if expiresAt, ok := r.expiresAt[key]; ok {
		entry.ExpiresAt = &expiresAt
	}
	return entry
}

func (r *recordingPubStateStore) setExpiryLocked(key string, ttl time.Duration) {
	if ttl <= 0 {
		delete(r.expiresAt, key)
		return
	}
	r.expiresAt[key] = time.Now().Add(ttl)
}

func (r *recordingPubStateStore) expiredLocked(key string, now time.Time) bool {
	expiresAt, ok := r.expiresAt[key]
	return ok && now.After(expiresAt)
}
