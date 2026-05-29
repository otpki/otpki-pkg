package asynqwrap

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/otpki/otpki-pkg/config"
	otpkilog "github.com/otpki/otpki-pkg/log"
	spfystate "github.com/otpki/otpki-pkg/spfy/anchor"
	spfydep "github.com/otpki/otpki-pkg/spfy/dep"
	spfypubsub "github.com/otpki/otpki-pkg/spfy/pulse"
	"github.com/samber/do/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingAsynqwrapStateStore struct {
	spfystate.Store[[]byte]
	mu   sync.Mutex
	keys []string
}

func (s *recordingAsynqwrapStateStore) record(key string) {
	if key == "" {
		return
	}
	s.mu.Lock()
	s.keys = append(s.keys, key)
	s.mu.Unlock()
}

func (s *recordingAsynqwrapStateStore) Get(ctx context.Context, key string) (spfystate.Entry[[]byte], error) {
	s.record(key)
	return s.Store.Get(ctx, key)
}

func (s *recordingAsynqwrapStateStore) Put(
	ctx context.Context,
	key string,
	value []byte,
	opts spfystate.WriteOptions,
) (spfystate.Entry[[]byte], error) {
	s.record(key)
	return s.Store.Put(ctx, key, value, opts)
}

func (s *recordingAsynqwrapStateStore) Create(
	ctx context.Context,
	key string,
	value []byte,
	opts spfystate.WriteOptions,
) (spfystate.Entry[[]byte], error) {
	s.record(key)
	return s.Store.Create(ctx, key, value, opts)
}

func (s *recordingAsynqwrapStateStore) CompareAndSet(
	ctx context.Context,
	key string,
	expected spfystate.ETag,
	value []byte,
	opts spfystate.WriteOptions,
) (spfystate.Entry[[]byte], error) {
	s.record(key)
	return s.Store.CompareAndSet(ctx, key, expected, value, opts)
}

func (s *recordingAsynqwrapStateStore) AppendMetadata(
	ctx context.Context,
	key string,
	metadata spfystate.Metadata,
) (spfystate.Entry[[]byte], error) {
	s.record(key)
	return s.Store.AppendMetadata(ctx, key, metadata)
}

func (s *recordingAsynqwrapStateStore) Delete(ctx context.Context, key string) error {
	s.record(key)
	return s.Store.Delete(ctx, key)
}

func (s *recordingAsynqwrapStateStore) Consume(ctx context.Context, key string) (spfystate.Entry[[]byte], error) {
	s.record(key)
	return s.Store.Consume(ctx, key)
}

func (s *recordingAsynqwrapStateStore) Query(
	ctx context.Context,
	query spfystate.Query,
) (spfystate.QueryResult[[]byte], error) {
	s.record(query.Prefix)
	return s.Store.Query(ctx, query)
}

func (s *recordingAsynqwrapStateStore) Watch(
	ctx context.Context,
	opts spfystate.WatchOptions,
) (<-chan spfystate.Event[[]byte], error) {
	s.record(opts.Key)
	s.record(opts.Prefix)
	return s.Store.Watch(ctx, opts)
}

func (s *recordingAsynqwrapStateStore) Batch(
	ctx context.Context,
	ops []spfystate.Operation[[]byte],
) ([]spfystate.Entry[[]byte], error) {
	for _, op := range ops {
		s.record(op.Key)
	}
	return s.Store.Batch(ctx, ops)
}

func (s *recordingAsynqwrapStateStore) recordedKeys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.keys...)
}

type recordingAsynqwrapPubSub struct {
	spfypubsub.Backend
	mu       sync.Mutex
	subjects []string
}

func (p *recordingAsynqwrapPubSub) record(subject string) {
	if subject == "" {
		return
	}
	p.mu.Lock()
	p.subjects = append(p.subjects, subject)
	p.mu.Unlock()
}

func (p *recordingAsynqwrapPubSub) Publish(
	ctx context.Context,
	subject string,
	msg spfypubsub.Message,
	opts spfypubsub.PublishOptions,
) error {
	p.record(subject)
	return p.Backend.Publish(ctx, subject, msg, opts)
}

func (p *recordingAsynqwrapPubSub) Subscribe(
	ctx context.Context,
	subject string,
	handler spfypubsub.Handler,
	opts spfypubsub.SubscribeOptions,
) (spfypubsub.Subscription, error) {
	p.record(subject)
	return p.Backend.Subscribe(ctx, subject, handler, opts)
}

func (p *recordingAsynqwrapPubSub) recordedSubjects() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.subjects...)
}

func TestSpfyUsesAppKeyspacePrefix(t *testing.T) {
	t.Parallel()
	cfg := config.DefaultConfig()
	cfg.App.Keyspace.App = "otpki-test"
	cfg.App.Keyspace.TenantID = "tenant-a"
	cfg.StateStore.Enabled = false
	cfg.PubSub.Enabled = false

	injector := do.New(spfydep.Package)
	do.ProvideValue(injector, t.Context())
	do.ProvideValue(injector, cfg)
	do.ProvideValue(injector, otpkilog.GetDefaultLogger(io.Discard, nil))
	t.Cleanup(func() { _ = injector.Shutdown() })

	stateStore := do.MustInvoke[spfystate.Store[[]byte]](injector)
	pubSub := do.MustInvoke[spfypubsub.Backend](injector)
	t.Cleanup(func() { _ = pubSub.Shutdown(context.Background()) })

	recordingStore := &recordingAsynqwrapStateStore{Store: stateStore}
	recordingPubSub := &recordingAsynqwrapPubSub{Backend: pubSub}

	broker, err := New(Config{
		Mode:          ModeSpfy,
		RuntimeConfig: cfg,
		Spfy: SpfyConfig{
			StateStore:     recordingStore,
			PubSub:         recordingPubSub,
			WorkerLeaseTTL: time.Second,
			PollInterval:   25 * time.Millisecond,
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = broker.Close() })

	server := broker.Server(asynq.Config{Concurrency: 1, Queues: map[string]int{"critical": 1}})
	require.NoError(t, server.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		return nil
	})))
	defer server.Shutdown()

	_, err = broker.Client().Enqueue(asynq.NewTask("prefixed", nil), asynq.Queue("critical"))
	require.NoError(t, err)

	waitFor(t, 3*time.Second, func() bool {
		return len(recordingStore.recordedKeys()) > 0 && len(recordingPubSub.recordedSubjects()) > 0
	})

	statePrefix := cfg.App.Keyspace.Key("asynqwrap")
	for _, key := range recordingStore.recordedKeys() {
		assert.True(t, strings.HasPrefix(key, statePrefix+"/"), "key %q missing prefix %q", key, statePrefix+"/")
	}

	topicPrefix := prefixedTopicPath(cfg, "queue")
	for _, subject := range recordingPubSub.recordedSubjects() {
		assert.True(
			t,
			strings.HasPrefix(subject, topicPrefix+"."),
			"subject %q missing prefix %q",
			subject,
			topicPrefix+".",
		)
	}
}

func TestRecurringTaskRedisStateUsesAppKeyspacePrefix(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	runtimeCfg := config.DefaultConfig()
	broker, err := New(Config{
		Mode:          ModeRedis,
		RuntimeConfig: runtimeCfg,
		Redis: asynq.RedisClientOpt{
			Addr: mr.Addr(),
		},
	})
	require.NoError(t, err)
	defer broker.Close()

	_, err = UpsertRecurringTask(t.Context(), broker.Client(), broker.Inspector(), RecurringTaskSpec[string]{
		Key:      "job-prefixed",
		Task:     asynq.NewTask("redis:recurring-prefix", []byte(`{"count":1}`)),
		Interval: time.Minute,
	})
	require.NoError(t, err)

	keys := mr.Keys()
	assert.Contains(t, keys, runtimeCfg.App.Keyspace.Key("asynqwrap", "recurring-state", `"job-prefixed"`))
}
