package asynqwrap

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/nats-io/nats-server/v2/server"
	natsserver "github.com/nats-io/nats-server/v2/test"
	"github.com/otpki/otpki-pkg/config"
	otpkilog "github.com/otpki/otpki-pkg/log"
	spfystate "github.com/otpki/otpki-pkg/spfy/anchor"
	spfydep "github.com/otpki/otpki-pkg/spfy/dep"
	spfypubsub "github.com/otpki/otpki-pkg/spfy/pulse"
	"github.com/samber/do/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

type spfyBackendCase struct {
	name  string
	build func(t *testing.T) Broker
}

const spfyCriticalQueue = "critical"

func TestSpfyBrokerInterfacesFromNew(t *testing.T) {
	t.Parallel()
	runSpfyBackendMatrix(t, func(t *testing.T, broker Broker) {
		t.Helper()
		client := broker.Client()
		server := broker.Server(asynq.Config{Concurrency: 1})
		scheduler := broker.Scheduler(nil)
		inspector := broker.Inspector()

		manager, err := broker.PeriodicTaskManager(PeriodicTaskManagerOpts{
			PeriodicTaskConfigProvider: newFakePeriodicTaskConfigProvider(nil),
			SyncInterval:               25 * time.Millisecond,
		})
		require.NoError(t, err)

		require.NotNil(t, client)
		require.NotNil(t, server)
		require.NotNil(t, scheduler)
		require.NotNil(t, inspector)
		require.NotNil(t, manager)

		require.NoError(t, client.Ping())
		require.NoError(t, server.Ping())
		require.NoError(t, scheduler.Ping())
		require.NoError(t, manager.Start())
		manager.Shutdown()

		_, err = inspector.Queues()
		require.NoError(t, err)
	})
}

func TestSpfyProcessesTask(t *testing.T) {
	t.Parallel()
	runSpfyBackendMatrix(t, func(t *testing.T, broker Broker) {
		t.Helper()
		inspector := broker.Inspector()
		srv := broker.Server(asynq.Config{Concurrency: 1})
		defer srv.Shutdown()

		done := make(chan *asynq.Task, 1)
		require.NoError(t, srv.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
			done <- task
			return nil
		})))

		info, err := broker.Client().Enqueue(
			asynq.NewTask("email:welcome", []byte("hello")),
			asynq.Retention(time.Hour),
		)
		require.NoError(t, err)

		select {
		case got := <-done:
			require.Equal(t, "email:welcome", got.Type())
			require.Equal(t, []byte("hello"), got.Payload())
		case <-time.After(3 * time.Second):
			t.Fatal("task was not processed")
		}

		waitFor(t, 3*time.Second, func() bool {
			tasks, err := inspector.ListCompletedTasks(info.Queue)
			return err == nil && len(tasks) == 1 && tasks[0].ID == info.ID
		})
	})
}

func TestSpfyScheduling(t *testing.T) {
	t.Parallel()
	runSpfyBackendMatrix(t, func(t *testing.T, broker Broker) {
		t.Helper()
		inspector := broker.Inspector()
		srv := broker.Server(asynq.Config{Concurrency: 1})
		defer srv.Shutdown()

		done := make(chan time.Time, 1)
		require.NoError(t, srv.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
			done <- time.Now()
			return nil
		})))

		start := time.Now()
		info, err := EnqueueIn(
			broker.Client(),
			150*time.Millisecond,
			asynq.NewTask("delayed", nil),
			asynq.Retention(time.Hour),
		)
		require.NoError(t, err)

		waitFor(t, 2*time.Second, func() bool {
			tasks, err := inspector.ListScheduledTasks(info.Queue)
			return err == nil && len(tasks) == 1 && tasks[0].ID == info.ID
		})

		time.Sleep(50 * time.Millisecond)
		select {
		case <-done:
			t.Fatal("scheduled task ran too early")
		default:
		}

		select {
		case got := <-done:
			assert.GreaterOrEqual(t, got.Sub(start), 120*time.Millisecond)
		case <-time.After(3 * time.Second):
			t.Fatal("scheduled task was not processed")
		}

		waitFor(t, 3*time.Second, func() bool {
			tasks, err := inspector.ListCompletedTasks(info.Queue)
			return err == nil && len(tasks) == 1 && tasks[0].ID == info.ID
		})
	})
}

func TestSpfyRetry(t *testing.T) {
	t.Parallel()
	runSpfyBackendMatrix(t, func(t *testing.T, broker Broker) {
		t.Helper()
		inspector := broker.Inspector()
		var attempts atomic.Int32
		done := make(chan struct{}, 1)

		srv := broker.Server(asynq.Config{
			Concurrency: 1,
			RetryDelayFunc: func(n int, err error, task *asynq.Task) time.Duration {
				return 100 * time.Millisecond
			},
		})
		defer srv.Shutdown()

		require.NoError(t, srv.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
			if attempts.Add(1) < 3 {
				return oops.New("boom")
			}
			done <- struct{}{}
			return nil
		})))

		info, err := broker.Client().Enqueue(asynq.NewTask("retry", nil), asynq.MaxRetry(5), asynq.Retention(time.Hour))
		require.NoError(t, err)

		waitForRetryTasks(t, inspector, info.Queue, 1)

		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("retried task was not processed")
		}

		assert.EqualValues(t, 3, attempts.Load())
		waitFor(t, 3*time.Second, func() bool {
			tasks, err := inspector.ListCompletedTasks(info.Queue)
			return err == nil && len(tasks) == 1 && tasks[0].ID == info.ID
		})
	})
}

func TestSpfyUniqueTaskDeduplication(t *testing.T) {
	t.Parallel()
	runSpfyBackendMatrix(t, func(t *testing.T, broker Broker) {
		t.Helper()
		task := asynq.NewTask("unique", []byte("x"))

		_, err := broker.Client().Enqueue(task, asynq.Unique(time.Minute))
		require.NoError(t, err)

		_, err = broker.Client().Enqueue(task, asynq.Unique(time.Minute))
		require.ErrorIs(t, err, asynq.ErrDuplicateTask)
	})
}

func TestSpfyPauseQueue(t *testing.T) {
	t.Parallel()
	runSpfyBackendMatrix(t, func(t *testing.T, broker Broker) {
		t.Helper()
		inspector := broker.Inspector()
		require.NoError(t, inspector.PauseQueue(spfyCriticalQueue))

		srv := broker.Server(asynq.Config{Concurrency: 1, Queues: map[string]int{spfyCriticalQueue: 1}})
		defer srv.Shutdown()

		done := make(chan struct{}, 1)
		require.NoError(t, srv.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
			done <- struct{}{}
			return nil
		})))

		_, err := broker.Client().Enqueue(asynq.NewTask("paused", nil), asynq.Queue(spfyCriticalQueue))
		require.NoError(t, err)

		select {
		case <-done:
			t.Fatal("paused queue should not process tasks")
		case <-time.After(250 * time.Millisecond):
		}

		require.NoError(t, inspector.UnpauseQueue(spfyCriticalQueue))

		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("unpaused queue did not resume")
		}
	})
}

func TestSpfyGroupAggregation(t *testing.T) {
	t.Parallel()
	runSpfyBackendMatrix(t, func(t *testing.T, broker Broker) {
		t.Helper()
		var calls atomic.Int32
		errCh := make(chan error, 1)
		srv := broker.Server(asynq.Config{
			Concurrency:      1,
			GroupGracePeriod: 50 * time.Millisecond,
			GroupMaxSize:     2,
			GroupAggregator: asynq.GroupAggregatorFunc(func(group string, tasks []*asynq.Task) *asynq.Task {
				return asynq.NewTask("batched", fmt.Appendf(nil, "%d", len(tasks)))
			}),
		})
		defer srv.Shutdown()

		done := make(chan struct{}, 1)
		require.NoError(t, srv.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
			calls.Add(1)
			if task.Type() != "batched" {
				errCh <- oops.Errorf("task type = %q, want %q", task.Type(), "batched")
				return nil
			}
			if string(task.Payload()) != "2" {
				errCh <- oops.Errorf("payload = %q, want %q", string(task.Payload()), "2")
				return nil
			}
			done <- struct{}{}
			return nil
		})))

		client := broker.Client()
		_, err := client.Enqueue(asynq.NewTask("grouped", []byte("1")), asynq.Group("emails"))
		require.NoError(t, err)
		_, err = client.Enqueue(asynq.NewTask("grouped", []byte("2")), asynq.Group("emails"))
		require.NoError(t, err)

		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("aggregated task not processed")
		}
		select {
		case err := <-errCh:
			require.NoError(t, err)
		default:
		}
		assert.EqualValues(t, 1, calls.Load())
	})
}

func TestSpfyRecurringTaskMiddlewareReenqueuesAndDeleteStops(t *testing.T) {
	t.Parallel()
	runSpfyBackendMatrix(t, func(t *testing.T, broker Broker) {
		t.Helper()
		inspector := broker.Inspector()
		client := broker.Client()

		var attempts atomic.Int32
		done := make(chan struct{}, 2)

		server := broker.Server(asynq.Config{Concurrency: 1})
		server.Use(SelfReschedulingTaskMiddleware(client, inspector))
		defer server.Shutdown()

		require.NoError(t, server.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
			if attempts.Add(1) <= 2 {
				done <- struct{}{}
			}
			return nil
		})))

		info, err := UpsertRecurringTask(t.Context(), client, inspector, RecurringTaskSpec[string]{
			Key:      "job",
			Task:     asynq.NewTask("recurring", []byte("payload")),
			Interval: 50 * time.Millisecond,
		})
		require.NoError(t, err)
		require.NotEmpty(t, info.ID)

		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("initial recurring task was not processed")
		}

		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("recurring task was not re-enqueued")
		}

		deleted, err := DeleteRecurringTask(t.Context(), inspector, "job")
		require.NoError(t, err)
		require.GreaterOrEqual(t, deleted, 0)

		current := attempts.Load()
		time.Sleep(150 * time.Millisecond)
		assert.Equal(t, current, attempts.Load())
	})
}

func TestSpfyInspectorListsPendingScheduledAndGroups(t *testing.T) {
	t.Parallel()
	runSpfyBackendMatrix(t, func(t *testing.T, broker Broker) {
		t.Helper()
		inspector := broker.Inspector()

		_, err := broker.Client().Enqueue(asynq.NewTask("pending", nil), asynq.Retention(time.Hour))
		require.NoError(t, err)
		_, err = EnqueueIn(broker.Client(), time.Minute, asynq.NewTask("scheduled", nil), asynq.Retention(time.Hour))
		require.NoError(t, err)
		_, err = broker.Client().Enqueue(asynq.NewTask("grouped", nil), asynq.Group("emails"))
		require.NoError(t, err)

		pending, err := inspector.ListPendingTasks("default")
		require.NoError(t, err)
		require.Len(t, pending, 1)
		assert.Equal(t, "pending", pending[0].Type)

		scheduled, err := inspector.ListScheduledTasks("default")
		require.NoError(t, err)
		require.Len(t, scheduled, 1)
		assert.Equal(t, "scheduled", scheduled[0].Type)

		groups, err := inspector.Groups("default")
		require.NoError(t, err)
		require.Len(t, groups, 1)
		assert.Equal(t, "emails", groups[0].Group)
		assert.Equal(t, 1, groups[0].Size)

		queues, err := inspector.Queues()
		require.NoError(t, err)
		assert.Contains(t, queues, "default")

		info, err := inspector.GetQueueInfo("default")
		require.NoError(t, err)
		assert.Equal(t, "default", info.Queue)
		assert.Equal(t, 1, info.Pending)
		assert.Equal(t, 1, info.Scheduled)
		assert.Equal(t, 1, info.Aggregating)
		assert.Equal(t, 3, info.Size)
	})
}

func TestSpfyInspectorCanInspectFailedTasks(t *testing.T) {
	t.Parallel()
	runSpfyBackendMatrix(t, func(t *testing.T, broker Broker) {
		t.Helper()
		inspector := broker.Inspector()
		srv := broker.Server(asynq.Config{
			Concurrency: 1,
			RetryDelayFunc: func(n int, err error, task *asynq.Task) time.Duration {
				return 25 * time.Millisecond
			},
		})
		defer srv.Shutdown()

		failed := make(chan struct{}, 1)
		require.NoError(t, srv.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
			failed <- struct{}{}
			return oops.New("handler exploded")
		})))

		info, err := broker.Client().
			Enqueue(asynq.NewTask("failing", nil), asynq.MaxRetry(0), asynq.Retention(time.Hour))
		require.NoError(t, err)

		select {
		case <-failed:
		case <-time.After(3 * time.Second):
			t.Fatal("task did not fail")
		}

		waitFor(t, 3*time.Second, func() bool {
			taskInfo, err := inspector.GetTaskInfo(info.Queue, info.ID)
			if err != nil || taskInfo.LastErr == "" {
				return false
			}
			archived, err := inspector.ListArchivedTasks(info.Queue)
			return err == nil && len(archived) == 1 && archived[0].LastErr != ""
		})
	})
}

func TestSpfyInspectorArchiveRunAndBulkOperations(t *testing.T) {
	t.Parallel()
	runSpfyBackendMatrix(t, func(t *testing.T, broker Broker) {
		t.Helper()
		inspector := broker.Inspector()

		pendingInfo, err := broker.Client().Enqueue(asynq.NewTask("archive-me", nil), asynq.Retention(time.Hour))
		require.NoError(t, err)
		require.NoError(t, inspector.ArchiveTask(pendingInfo.Queue, pendingInfo.ID))

		archived, err := inspector.ListArchivedTasks(pendingInfo.Queue)
		require.NoError(t, err)
		require.Len(t, archived, 1)

		require.NoError(t, inspector.RunTask(pendingInfo.Queue, pendingInfo.ID))

		pending, err := inspector.ListPendingTasks(pendingInfo.Queue)
		require.NoError(t, err)
		require.Len(t, pending, 1)
		assert.Equal(t, pendingInfo.ID, pending[0].ID)

		_, err = EnqueueIn(broker.Client(), time.Hour, asynq.NewTask("sched-1", nil), asynq.Retention(time.Hour))
		require.NoError(t, err)
		_, err = EnqueueIn(broker.Client(), time.Hour, asynq.NewTask("sched-2", nil), asynq.Retention(time.Hour))
		require.NoError(t, err)
		require.NoError(t, inspector.PauseQueue("default"))
		info, err := inspector.GetQueueInfo("default")
		require.NoError(t, err)
		assert.True(t, info.Paused)
		require.NoError(t, inspector.UnpauseQueue("default"))

		if n, err := inspector.RunAllScheduledTasks("default"); err != nil || n != 2 {
			t.Fatalf("RunAllScheduledTasks = (%d, %v), want (2, nil)", n, err)
		}

		scheduled, err := inspector.ListScheduledTasks("default")
		require.NoError(t, err)
		require.Empty(t, scheduled)

		info1, err := EnqueueIn(
			broker.Client(),
			time.Hour,
			asynq.NewTask("delete-archived-1", nil),
			asynq.Retention(time.Hour),
		)
		require.NoError(t, err)
		info2, err := EnqueueIn(
			broker.Client(),
			time.Hour,
			asynq.NewTask("delete-archived-2", nil),
			asynq.Retention(time.Hour),
		)
		require.NoError(t, err)
		require.NoError(t, inspector.ArchiveTask(info1.Queue, info1.ID))
		require.NoError(t, inspector.ArchiveTask(info2.Queue, info2.ID))
		if n, err := inspector.DeleteAllArchivedTasks("default"); err != nil || n != 2 {
			t.Fatalf("DeleteAllArchivedTasks = (%d, %v), want (2, nil)", n, err)
		}

		queues, err := inspector.Queues()
		require.NoError(t, err)
		assert.Contains(t, queues, "default")
	})
}

func runSpfyBackendMatrix(t *testing.T, fn func(t *testing.T, broker Broker)) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	for _, tc := range spfyBackendCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			broker := tc.build(t)
			fn(t, broker)
		})
	}
}

func spfyBackendCases(t *testing.T) []spfyBackendCase {
	t.Helper()
	return []spfyBackendCase{
		{
			name: "in-memory",
			build: func(t *testing.T) Broker {
				t.Helper()
				return newSpfyBrokerForTest(t, func(cfg *config.Config) {
					cfg.StateStore.Enabled = false
					cfg.PubSub.Enabled = false
				})
			},
		},
		{
			name: "redis/testcontainers",
			build: func(t *testing.T) Broker {
				t.Helper()
				ctx := t.Context()
				ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
					ContainerRequest: testcontainers.ContainerRequest{
						Image:        "redis:7-alpine",
						ExposedPorts: []string{"6379/tcp"},
						WaitingFor:   wait.ForListeningPort("6379/tcp").WithStartupTimeout(2 * time.Minute),
					},
					Started: true,
				})
				require.NoError(t, err)
				t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })

				host, err := ctr.Host(ctx)
				require.NoError(t, err)
				port, err := ctr.MappedPort(ctx, "6379/tcp")
				require.NoError(t, err)
				addr := host + ":" + port.Port()

				return newSpfyBrokerForTest(t, func(cfg *config.Config) {
					cfg.StateStore.Enabled = true
					cfg.StateStore.Backend = config.StateStoreBackendRedis
					cfg.Backends.Redis.Addrs = []string{addr}

					cfg.PubSub.Enabled = true
					cfg.PubSub.Backend = config.PubSubBackendRedis
					cfg.PubSub.Redis.ConsumerName = sanitizedSpfyTestName(t)
				})
			},
		},
		{
			name: "nats/jetstream",
			build: func(t *testing.T) Broker {
				t.Helper()
				srv := newJetStreamServer(t)
				name := sanitizedSpfyTestName(t)

				return newSpfyBrokerForTest(t, func(cfg *config.Config) {
					cfg.StateStore.Enabled = true
					cfg.StateStore.Backend = config.StateStoreBackendNATSJetStream
					cfg.Backends.NATS.URL = srv.ClientURL()
					cfg.Backends.NATS.Name = name

					cfg.PubSub.Enabled = true
					cfg.PubSub.Backend = config.PubSubBackendNATSJetStream
					cfg.PubSub.NATSJetStream.StreamName = name + "-stream"
					cfg.PubSub.NATSJetStream.AckWait = 10 * time.Second
				})
			},
		},
		//{
		//	name: "nats/core+pulsar",
		//	build: func(t *testing.T) Broker {
		//		t.Helper()
		//
		//		srv := newJetStreamServer(t)
		//		name := sanitizedSpfyTestName(t)
		//
		//		ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
		//		t.Cleanup(cancel)
		//
		//		ctr, err := pulsar.Run(ctx, "apachepulsar/pulsar:4.2.1")
		//		require.NoError(t, err)
		//		t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })
		//
		//		brokerURL, err := ctr.BrokerURL(ctx)
		//		require.NoError(t, err)
		//
		//		return newSpfyBrokerForTest(t, func(cfg *config.Config) {
		//			cfg.StateStore.Enabled = true
		//			cfg.StateStore.Backend = config.StateStoreBackendNATSCore
		//			cfg.Backends.NATS.URL = srv.ClientURL()
		//			cfg.Backends.NATS.Name = name
		//
		//			cfg.PubSub.Enabled = true
		//			cfg.PubSub.Backend = config.PubSubBackendPulsar
		//			cfg.Backends.Pulsar.URL = brokerURL
		//			cfg.PubSub.Pulsar.Tenant = "public"
		//			cfg.PubSub.Pulsar.Namespace = "default"
		//			cfg.PubSub.Pulsar.TopicPrefix = ""
		//			cfg.PubSub.Pulsar.SubscriptionName = sanitizedSpfyTestName(t)
		//			cfg.PubSub.Pulsar.DisableBatching = true
		//		})
		//	},
		//},
	}
}

func newSpfyBrokerForTest(t *testing.T, mutate func(*config.Config)) Broker {
	t.Helper()

	cfg := config.DefaultConfig()
	mutate(&cfg)

	injector := do.New(spfydep.Package)
	do.ProvideValue(injector, t.Context())
	do.ProvideValue(injector, cfg)
	do.ProvideValue(injector, otpkilog.GetDefaultLogger(io.Discard, nil))

	stateStore := do.MustInvoke[spfystate.Store[[]byte]](injector)
	pubSub := do.MustInvoke[spfypubsub.Backend](injector)

	broker, err := New(Config{
		Mode:          ModeSpfy,
		RuntimeConfig: cfg,
		Spfy: SpfyConfig{
			StateStore:     stateStore,
			PubSub:         pubSub,
			WorkerLeaseTTL: time.Second,
			PollInterval:   25 * time.Millisecond,
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = broker.Close()
		_ = pubSub.Shutdown(context.Background())
		_ = injector.Shutdown()
	})
	return broker
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

func sanitizedSpfyTestName(t *testing.T) string {
	t.Helper()
	return strings.NewReplacer("/", "-", " ", "-").Replace("spfy-test-" + t.Name())
}

func asynqwrapTopicPrefix(cfg config.Config) string {
	return prefixedTopicPath(cfg, "queue")
}
