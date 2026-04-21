package asynqwrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

type fakePeriodicTaskConfigProvider struct {
	cfg atomic.Pointer[[]*asynq.PeriodicTaskConfig]
}

func newFakePeriodicTaskConfigProvider(cfgs []*asynq.PeriodicTaskConfig) *fakePeriodicTaskConfigProvider {
	p := &fakePeriodicTaskConfigProvider{}
	p.SetConfigs(cfgs)
	return p
}

func (p *fakePeriodicTaskConfigProvider) SetConfigs(cfgs []*asynq.PeriodicTaskConfig) {
	p.cfg.Store(new(append([]*asynq.PeriodicTaskConfig(nil), cfgs...)))
}

func (p *fakePeriodicTaskConfigProvider) GetConfigs() ([]*asynq.PeriodicTaskConfig, error) {
	cfgs := p.cfg.Load()
	if cfgs == nil {
		return nil, nil
	}
	return append([]*asynq.PeriodicTaskConfig(nil), *cfgs...), nil
}

type captureLogger struct {
	mu   sync.Mutex
	logs []string
}

func (l *captureLogger) Debug(args ...any) { l.append(args...) }
func (l *captureLogger) Info(args ...any)  { l.append(args...) }
func (l *captureLogger) Warn(args ...any)  { l.append(args...) }
func (l *captureLogger) Error(args ...any) { l.append(args...) }
func (l *captureLogger) Fatal(args ...any) { l.append(args...) }

func (l *captureLogger) append(args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, strings.TrimSpace(fmt.Sprint(args...)))
}

func (l *captureLogger) contains(substr string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, entry := range l.logs {
		if strings.Contains(entry, substr) {
			return true
		}
	}
	return false
}

func TestBrokerInterfacesFromNew(t *testing.T) {
	t.Parallel()
	broker, err := New(Config{Mode: ModeMemory})
	require.NoError(t, err)
	defer broker.Close()

	var (
		client          = broker.Client()
		server          = broker.Server(asynq.Config{Concurrency: 1})
		scheduler       = broker.Scheduler(nil)
		periodicManager PeriodicTaskManager
		inspector       = broker.Inspector()
	)
	periodicManager, err = broker.PeriodicTaskManager(PeriodicTaskManagerOpts{
		PeriodicTaskConfigProvider: newFakePeriodicTaskConfigProvider(nil),
	})
	require.NoError(t, err)

	require.NotNil(t, client)
	require.NotNil(t, server)
	require.NotNil(t, scheduler)
	require.NotNil(t, periodicManager)
	require.NotNil(t, inspector)
	require.NoError(t, client.Ping())
	require.NoError(t, server.Ping())
	require.NoError(t, scheduler.Ping())
	_, err = inspector.Queues()
	require.NoError(t, err)
}

func TestClientEnqueueContextHonorsCanceledContext(t *testing.T) {
	t.Parallel()
	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := broker.Client().EnqueueContext(ctx, asynq.NewTask("ctx:cancelled", nil))
	require.Error(t, err)
}

func TestMemoryProcessesTask(t *testing.T) {
	t.Parallel()
	broker, err := New(Config{Mode: ModeMemory})
	require.NoError(t, err)
	defer broker.Close()

	srv := broker.Server(asynq.Config{Concurrency: 1})
	done := make(chan struct{}, 1)
	mux := asynq.NewServeMux()
	mux.HandleFunc("email:welcome", func(ctx context.Context, task *asynq.Task) error {
		done <- struct{}{}
		return nil
	})
	require.NoError(t, srv.Start(mux))
	defer srv.Shutdown()

	_, err = broker.Client().Enqueue(asynq.NewTask("email:welcome", nil))
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("task was not processed")
	}
}

func TestServerRunImplementsBlockingInterface(t *testing.T) {
	t.Parallel()
	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()

	server := broker.Server(asynq.Config{Concurrency: 1})
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
			return nil
		}))
	}()

	time.Sleep(50 * time.Millisecond)
	server.Stop()

	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("server.Run did not return after Stop")
	}
}

func TestMemoryScheduling(t *testing.T) {
	t.Parallel()
	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()
	srv := broker.Server(asynq.Config{Concurrency: 1})
	defer srv.Shutdown()
	done := make(chan time.Time, 1)
	require.NoError(t, srv.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		done <- time.Now()
		return nil
	})))
	start := time.Now()
	_, err := broker.Client().Enqueue(asynq.NewTask("delayed", nil), asynq.ProcessIn(200*time.Millisecond))
	require.NoError(t, err)
	select {
	case got := <-done:
		assert.GreaterOrEqual(t, got.Sub(start), 150*time.Millisecond)
	case <-time.After(2 * time.Second):
		t.Fatal("scheduled task was not processed")
	}
}

func TestEnqueueInHelper(t *testing.T) {
	t.Parallel()
	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()
	srv := broker.Server(asynq.Config{Concurrency: 1})
	defer srv.Shutdown()
	done := make(chan time.Time, 1)
	require.NoError(t, srv.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		done <- time.Now()
		return nil
	})))
	start := time.Now()
	_, err := EnqueueIn(broker.Client(), 150*time.Millisecond, asynq.NewTask("delayed:helper", nil))
	require.NoError(t, err)
	select {
	case got := <-done:
		assert.GreaterOrEqual(t, got.Sub(start), 100*time.Millisecond)
	case <-time.After(2 * time.Second):
		t.Fatal("helper scheduled task was not processed")
	}
}

func TestEnqueueAtHelper(t *testing.T) {
	t.Parallel()
	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()
	srv := broker.Server(asynq.Config{Concurrency: 1})
	defer srv.Shutdown()
	done := make(chan time.Time, 1)
	require.NoError(t, srv.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		done <- time.Now()
		return nil
	})))
	at := time.Now().Add(150 * time.Millisecond)
	_, err := EnqueueAt(broker.Client(), at, asynq.NewTask("at:helper", nil))
	require.NoError(t, err)
	select {
	case got := <-done:
		assert.False(t, got.Before(at.Add(-50*time.Millisecond)), "helper task ran too early: got=%v want-after=%v", got, at)
	case <-time.After(2 * time.Second):
		t.Fatal("helper at task was not processed")
	}
}

func TestMemoryRetry(t *testing.T) {
	t.Parallel()
	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()
	var attempts atomic.Int32
	srv := broker.Server(asynq.Config{
		Concurrency: 1,
		RetryDelayFunc: func(n int, err error, task *asynq.Task) time.Duration {
			return 10 * time.Millisecond
		},
	})
	defer srv.Shutdown()
	done := make(chan struct{}, 1)
	if err := srv.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		if attempts.Add(1) < 3 {
			return errors.New("boom")
		}
		done <- struct{}{}
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	_, err := broker.Client().Enqueue(asynq.NewTask("retry", nil), asynq.MaxRetry(5))
	require.NoError(t, err)
	select {
	case <-done:
		assert.EqualValues(t, 3, attempts.Load())
	case <-time.After(2 * time.Second):
		t.Fatal("retried task was not processed")
	}
}

func TestInspectorCanInspectFailedTasks(t *testing.T) {
	t.Parallel()
	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()

	inspector := broker.Inspector()
	srv := broker.Server(asynq.Config{
		Concurrency: 1,
		RetryDelayFunc: func(n int, err error, task *asynq.Task) time.Duration {
			return 10 * time.Millisecond
		},
	})
	defer srv.Shutdown()

	failed := make(chan struct{}, 1)
	require.NoError(t, srv.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		failed <- struct{}{}
		return errors.New("handler exploded")
	})))

	info, err := broker.Client().Enqueue(asynq.NewTask("failing", nil), asynq.MaxRetry(0))
	require.NoError(t, err)

	select {
	case <-failed:
	case <-time.After(2 * time.Second):
		t.Fatal("task did not fail")
	}

	taskInfo, err := inspector.GetTaskInfo(info.Queue, info.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, taskInfo.LastErr)

	archived, err := inspector.ListArchivedTasks(info.Queue)
	require.NoError(t, err)
	require.Len(t, archived, 1)
	assert.NotEmpty(t, archived[0].LastErr)
}

func TestInspectorListsPendingScheduledAndGroups(t *testing.T) {
	t.Parallel()
	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()
	inspector := broker.Inspector()
	_ = broker.Server(asynq.Config{
		GroupGracePeriod: time.Minute,
		GroupAggregator: asynq.GroupAggregatorFunc(func(group string, tasks []*asynq.Task) *asynq.Task {
			return asynq.NewTask("grouped:aggregate", nil)
		}),
	})

	_, err := broker.Client().Enqueue(asynq.NewTask("pending", nil))
	require.NoError(t, err)
	_, err = EnqueueIn(broker.Client(), time.Minute, asynq.NewTask("scheduled", nil))
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
}

func TestInspectorListsRetryTasks(t *testing.T) {
	t.Parallel()
	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()

	inspector := broker.Inspector()
	srv := broker.Server(asynq.Config{
		Concurrency: 1,
		RetryDelayFunc: func(n int, err error, task *asynq.Task) time.Duration {
			return time.Second
		},
	})
	defer srv.Shutdown()

	failed := make(chan struct{}, 1)
	if err := srv.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		failed <- struct{}{}
		return errors.New("retry me")
	})); err != nil {
		t.Fatal(err)
	}

	info, err := broker.Client().Enqueue(asynq.NewTask("retry-inspect", nil), asynq.MaxRetry(2))
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-failed:
	case <-time.After(2 * time.Second):
		t.Fatal("task did not fail into retry")
	}

	retryTasks, err := inspector.ListRetryTasks(info.Queue)
	if err != nil {
		t.Fatal(err)
	}
	if len(retryTasks) != 1 {
		t.Fatalf("got %d retry tasks, want 1", len(retryTasks))
	}
	if retryTasks[0].LastErr == "" || retryTasks[0].Retried != 1 {
		t.Fatalf("unexpected retry info: %+v", retryTasks[0])
	}
}

func TestInspectorCanListPaginated(t *testing.T) {
	t.Parallel()
	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()
	queue := "q"
	inspector := broker.Inspector()
	srv := broker.Server(asynq.Config{
		Concurrency: 1,
		Queues:      map[string]int{queue: 1},
	})
	defer srv.Shutdown()

	count := 5
	out := make(chan struct{}, 1)
	require.NoError(t, srv.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		out <- struct{}{}
		return nil
	})))

	for range count {
		_, err := broker.Client().Enqueue(asynq.NewTask("completes", nil), asynq.Retention(time.Hour), asynq.Queue(queue))
		require.NoError(t, err)
	}
	for range count {
		select {
		case <-out:
		case <-time.After(3 * time.Second):
			t.Fatal("timedout")
		}
	}

	tasks, err := inspector.ListCompletedTasks(queue)
	require.NoError(t, err)
	require.Len(t, tasks, count)

	tasks, err = inspector.ListCompletedTasks(queue, asynq.PageSize(-9000), asynq.Page(-9000))
	require.NoError(t, err)
	require.Len(t, tasks, 1)

	tasks, err = inspector.ListCompletedTasks(queue, asynq.PageSize(0), asynq.Page(0))
	require.NoError(t, err)
	require.Len(t, tasks, 1)

	tasks, err = inspector.ListCompletedTasks(queue, asynq.PageSize(2), asynq.Page(1))
	require.NoError(t, err)
	require.Len(t, tasks, 2)

	tasks, err = inspector.ListCompletedTasks(queue, asynq.PageSize(2), asynq.Page(3))
	require.NoError(t, err)
	require.Len(t, tasks, 1)

	tasks, err = inspector.ListCompletedTasks(queue, asynq.PageSize(2), asynq.Page(4))
	require.NoError(t, err)
	require.Len(t, tasks, 0)
}

func TestInspectorRunAndDeleteAllRetryTasks(t *testing.T) {
	t.Parallel()
	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()
	inspector := broker.Inspector()
	srv := broker.Server(asynq.Config{
		Concurrency: 1,
		RetryDelayFunc: func(n int, err error, task *asynq.Task) time.Duration {
			return time.Hour
		},
	})
	defer srv.Shutdown()

	fail := make(chan struct{}, 2)
	if err := srv.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		fail <- struct{}{}
		return errors.New("retry me")
	})); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if _, err := broker.Client().Enqueue(asynq.NewTask("retry-bulk", nil), asynq.MaxRetry(3)); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		select {
		case <-fail:
		case <-time.After(2 * time.Second):
			t.Fatal("task did not enter retry state")
		}
	}
	waitForRetryTasks(t, inspector, "default", 2)

	if n, err := inspector.RunAllRetryTasks("default"); err != nil || n != 2 {
		t.Fatalf("RunAllRetryTasks = (%d, %v), want (2, nil)", n, err)
	}

	// Allow them to fail back into retry, then bulk delete.
	for range 2 {
		select {
		case <-fail:
		case <-time.After(2 * time.Second):
			t.Fatal("rerun task did not execute")
		}
	}
	waitForRetryTasks(t, inspector, "default", 2)

	if n, err := inspector.DeleteAllRetryTasks("default"); err != nil || n != 2 {
		t.Fatalf("DeleteAllRetryTasks = (%d, %v), want (2, nil)", n, err)
	}
}

func TestInspectorListActiveTasks(t *testing.T) {
	t.Parallel()
	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()
	inspector := broker.Inspector()
	srv := broker.Server(asynq.Config{Concurrency: 1})
	defer srv.Shutdown()

	block := make(chan struct{})
	started := make(chan struct{}, 1)
	if err := srv.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		started <- struct{}{}
		<-block
		return nil
	})); err != nil {
		t.Fatal(err)
	}

	if _, err := broker.Client().Enqueue(asynq.NewTask("active", nil)); err != nil {
		t.Fatal(err)
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("task never became active")
	}

	active, err := inspector.ListActiveTasks("default")
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].Type != "active" {
		t.Fatalf("unexpected active tasks: %+v", active)
	}

	close(block)
}

func TestMemoryPauseQueue(t *testing.T) {
	t.Parallel()
	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()
	inspector := broker.Inspector()
	if err := inspector.PauseQueue("critical"); err != nil {
		t.Fatal(err)
	}
	srv := broker.Server(asynq.Config{Concurrency: 1, Queues: map[string]int{"critical": 1}})
	defer srv.Shutdown()
	done := make(chan struct{}, 1)
	if err := srv.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		done <- struct{}{}
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := broker.Client().Enqueue(asynq.NewTask("paused", nil), asynq.Queue("critical")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		t.Fatal("paused queue should not process tasks")
	case <-time.After(200 * time.Millisecond):
	}
	if err := inspector.UnpauseQueue("critical"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("unpaused queue did not resume")
	}
}

func TestInspectorDeleteTaskBeforeStart(t *testing.T) {
	t.Parallel()
	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()

	inspector := broker.Inspector()
	info, err := EnqueueIn(broker.Client(), time.Minute, asynq.NewTask("delete-me", nil))
	if err != nil {
		t.Fatal(err)
	}

	if err := inspector.DeleteTask(info.Queue, info.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := inspector.GetTaskInfo(info.Queue, info.ID); err == nil {
		t.Fatal("expected deleted task to be missing")
	}
}

func TestInspectorArchiveRunAndBulkOperations(t *testing.T) {
	t.Parallel()
	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()
	inspector := broker.Inspector()

	pendingInfo, err := broker.Client().Enqueue(asynq.NewTask("archive-me", nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := inspector.ArchiveTask(pendingInfo.Queue, pendingInfo.ID); err != nil {
		t.Fatal(err)
	}

	archived, err := inspector.ListArchivedTasks(pendingInfo.Queue)
	if err != nil {
		t.Fatal(err)
	}
	if len(archived) != 1 {
		t.Fatalf("expected 1 archived task, got %d", len(archived))
	}

	if err := inspector.RunTask(pendingInfo.Queue, pendingInfo.ID); err != nil {
		t.Fatal(err)
	}
	pending, err := inspector.ListPendingTasks(pendingInfo.Queue)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != pendingInfo.ID {
		t.Fatalf("unexpected pending after run: %+v", pending)
	}

	if _, err := EnqueueIn(broker.Client(), time.Hour, asynq.NewTask("sched-1", nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := EnqueueIn(broker.Client(), time.Hour, asynq.NewTask("sched-2", nil)); err != nil {
		t.Fatal(err)
	}
	if n, err := inspector.RunAllScheduledTasks("default"); err != nil || n != 2 {
		t.Fatalf("RunAllScheduledTasks = (%d, %v), want (2, nil)", n, err)
	}

	scheduled, err := inspector.ListScheduledTasks("default")
	if err != nil {
		t.Fatal(err)
	}
	if len(scheduled) != 0 {
		t.Fatalf("expected no scheduled tasks, got %+v", scheduled)
	}

	info1, err := EnqueueIn(broker.Client(), time.Hour, asynq.NewTask("delete-archived-1", nil))
	if err != nil {
		t.Fatal(err)
	}
	info2, err := EnqueueIn(broker.Client(), time.Hour, asynq.NewTask("delete-archived-2", nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := inspector.ArchiveTask(info1.Queue, info1.ID); err != nil {
		t.Fatal(err)
	}
	if err := inspector.ArchiveTask(info2.Queue, info2.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := inspector.DeleteAllArchivedTasks("default"); err != nil || n != 2 {
		t.Fatalf("DeleteAllArchivedTasks = (%d, %v), want (2, nil)", n, err)
	}
}

func TestMemoryPriorityQueues(t *testing.T) {
	t.Parallel()
	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()

	var got []string
	done := make(chan struct{}, 3)
	srv := broker.Server(asynq.Config{
		Concurrency:    1,
		Queues:         map[string]int{"critical": 2, "default": 1},
		StrictPriority: false,
	})
	defer srv.Shutdown()
	if err := srv.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		got = append(got, task.Type())
		done <- struct{}{}
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	client := broker.Client()
	for range 2 {
		if _, err := client.Enqueue(asynq.NewTask("critical", nil), asynq.Queue("critical")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.Enqueue(asynq.NewTask("default", nil), asynq.Queue("default")); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("priority tasks were not processed")
		}
	}
	if !slices.Equal(got, []string{"critical", "critical", "default"}) {
		t.Fatalf("unexpected weighted order: %v", got)
	}
}

func TestMemoryUniqueTask(t *testing.T) {
	t.Parallel()
	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()
	task := asynq.NewTask("unique", []byte("x"))
	if _, err := broker.Client().Enqueue(task, asynq.Unique(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := broker.Client().Enqueue(task, asynq.Unique(time.Minute)); !errors.Is(err, asynq.ErrDuplicateTask) {
		t.Fatalf("got err %v, want duplicate", err)
	}
}

func TestMemoryGroupAggregation(t *testing.T) {
	t.Parallel()
	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()
	var calls atomic.Int32
	srv := broker.Server(asynq.Config{
		Concurrency:      1,
		GroupGracePeriod: 50 * time.Millisecond,
		GroupMaxSize:     2,
		GroupAggregator: asynq.GroupAggregatorFunc(func(group string, tasks []*asynq.Task) *asynq.Task {
			return asynq.NewTask("batched", []byte{byte(len(tasks))})
		}),
	})
	defer srv.Shutdown()
	done := make(chan struct{}, 1)
	if err := srv.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		calls.Add(1)
		if task.Type() != "batched" || len(task.Payload()) != 1 || task.Payload()[0] != 2 {
			t.Fatalf("unexpected aggregated task: type=%s payload=%v", task.Type(), task.Payload())
		}
		done <- struct{}{}
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	client := broker.Client()
	if _, err := client.Enqueue(asynq.NewTask("a", []byte("1")), asynq.Group("g")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Enqueue(asynq.NewTask("a", []byte("2")), asynq.Group("g")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		if calls.Load() != 1 {
			t.Fatalf("got %d calls, want 1", calls.Load())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("aggregated task not processed")
	}
}

func TestMemoryPeriodicTask(t *testing.T) {
	t.Parallel()
	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()
	srv := broker.Server(asynq.Config{Concurrency: 1})
	defer srv.Shutdown()
	done := make(chan struct{}, 1)
	if err := srv.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		done <- struct{}{}
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	scheduler := broker.Scheduler(nil)
	if _, err := RegisterEvery(scheduler, time.Second, asynq.NewTask("periodic", nil)); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Start(); err != nil {
		t.Fatal(err)
	}
	defer scheduler.Shutdown()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("periodic task not processed")
	}
}

func TestMemoryPeriodicTaskManagerSyncsConfigs(t *testing.T) {
	t.Parallel()

	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()

	task1 := &asynq.PeriodicTaskConfig{Cronspec: "@every 1s", Task: asynq.NewTask("task1", nil)}
	task2 := &asynq.PeriodicTaskConfig{Cronspec: "@every 1s", Task: asynq.NewTask("task2", nil)}
	task3 := &asynq.PeriodicTaskConfig{Cronspec: "@every 1s", Task: asynq.NewTask("task3", nil)}
	provider := newFakePeriodicTaskConfigProvider([]*asynq.PeriodicTaskConfig{task1, task2})

	manager, err := broker.PeriodicTaskManager(PeriodicTaskManagerOpts{
		PeriodicTaskConfigProvider: provider,
		SyncInterval:               25 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	memMgr, ok := manager.(*periodicTaskManager)
	if !ok {
		t.Fatalf("manager type = %T, want *periodicTaskManager", manager)
	}

	if err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	defer manager.Shutdown()

	waitFor(t, 2*time.Second, func() bool {
		memMgr.mu.Lock()
		defer memMgr.mu.Unlock()
		return len(memMgr.entries) == 2
	})

	provider.SetConfigs([]*asynq.PeriodicTaskConfig{task1, task3})
	waitFor(t, 2*time.Second, func() bool {
		memMgr.mu.Lock()
		defer memMgr.mu.Unlock()
		_, hasTask1 := memMgr.entries[periodicTaskConfigHash(task1)]
		_, hasTask2 := memMgr.entries[periodicTaskConfigHash(task2)]
		_, hasTask3 := memMgr.entries[periodicTaskConfigHash(task3)]
		return len(memMgr.entries) == 2 && hasTask1 && !hasTask2 && hasTask3
	})

	provider.SetConfigs(nil)
	waitFor(t, 2*time.Second, func() bool {
		memMgr.mu.Lock()
		defer memMgr.mu.Unlock()
		return len(memMgr.entries) == 0
	})
}

func TestMemoryPeriodicTaskManagerUsesConfiguredLogger(t *testing.T) {
	t.Parallel()

	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()

	logger := &captureLogger{}
	manager, err := broker.PeriodicTaskManager(PeriodicTaskManagerOpts{
		PeriodicTaskConfigProvider: newFakePeriodicTaskConfigProvider([]*asynq.PeriodicTaskConfig{
			{Cronspec: "@every 1s", Task: asynq.NewTask("logged", nil)},
		}),
		SchedulerOpts: &asynq.SchedulerOpts{
			Logger:   logger,
			LogLevel: asynq.InfoLevel,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	manager.Shutdown()

	if !logger.contains("Successfully registered periodic task") {
		t.Fatalf("expected periodic task manager logs, got %v", logger.logs)
	}
	if !logger.contains("Scheduler timezone is set to") {
		t.Fatalf("expected scheduler logs, got %v", logger.logs)
	}
}

func TestNewPeriodicTaskManagerRequiresBrokerOrRedis(t *testing.T) {
	t.Parallel()

	_, err := NewPeriodicTaskManager(PeriodicTaskManagerOpts{
		PeriodicTaskConfigProvider: newFakePeriodicTaskConfigProvider(nil),
	})
	if err == nil {
		t.Fatal("expected constructor error without broker or redis")
	}
}

func TestTracingMiddlewarePropagatesProducerToConsumer(t *testing.T) {
	t.Parallel()
	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()

	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer func() {
		_ = provider.Shutdown(t.Context())
	}()

	traceConfig := TraceConfig{
		Tracer:     provider.Tracer("test"),
		Propagator: propagation.TraceContext{},
	}

	client := broker.Client()
	client.Use(TraceEnqueueMiddleware(traceConfig))

	server := broker.Server(asynq.Config{Concurrency: 1})
	server.Use(TraceHandlerMiddleware(traceConfig))
	defer server.Shutdown()

	done := make(chan struct{}, 1)
	if err := server.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		if !trace.SpanFromContext(ctx).SpanContext().IsValid() {
			t.Fatal("expected valid span context in task handler")
		}
		done <- struct{}{}
		return nil
	})); err != nil {
		t.Fatal(err)
	}

	rootCtx, rootSpan := provider.Tracer("test").Start(t.Context(), "root")
	if _, err := client.EnqueueContext(rootCtx, asynq.NewTask("trace:task", nil)); err != nil {
		t.Fatal(err)
	}
	rootSpan.End()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("traced task was not processed")
	}

	spans := spansByName(exporter.GetSpans())
	root := spans["root"]
	enqueue := spans["asynq enqueue trace:task"]
	process := spans["asynq process trace:task"]

	if enqueue.Parent.TraceID() != root.SpanContext.TraceID() || enqueue.Parent.SpanID() != root.SpanContext.SpanID() {
		t.Fatalf("enqueue parent = %s, want %s", enqueue.Parent.SpanID(), root.SpanContext.SpanID())
	}
	if process.Parent.TraceID() != enqueue.SpanContext.TraceID() || process.Parent.SpanID() != enqueue.SpanContext.SpanID() {
		t.Fatalf("process parent = %s, want %s", process.Parent.SpanID(), enqueue.SpanContext.SpanID())
	}
	if got := attributeValue(process.Attributes, "asynq.task.type"); got != "trace:task" {
		t.Fatalf("consumer span attribute = %q, want %q", got, "trace:task")
	}
}

func TestTracingMiddlewarePreservesTaskOptions(t *testing.T) {
	t.Parallel()
	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()

	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer func() {
		_ = provider.Shutdown(t.Context())
	}()

	client := broker.Client()
	client.Use(TraceEnqueueMiddleware(TraceConfig{
		Tracer:     provider.Tracer("test"),
		Propagator: propagation.TraceContext{},
	}))

	info, err := client.Enqueue(asynq.NewTask("trace:queue", nil, asynq.Queue("critical")))
	if err != nil {
		t.Fatal(err)
	}

	if info.Queue != "critical" {
		t.Fatalf("queue = %q, want %q", info.Queue, "critical")
	}
}

func TestWithTracingSchedulerOptsCreatesProducerSpan(t *testing.T) {
	t.Parallel()
	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()

	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer func() {
		_ = provider.Shutdown(t.Context())
	}()

	traceConfig := TraceConfig{
		Tracer:     provider.Tracer("test"),
		Propagator: propagation.TraceContext{},
	}

	server := broker.Server(asynq.Config{Concurrency: 1})
	server.Use(TraceHandlerMiddleware(traceConfig))
	defer server.Shutdown()

	done := make(chan struct{}, 1)
	if err := server.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		done <- struct{}{}
		return nil
	})); err != nil {
		t.Fatal(err)
	}

	scheduler := broker.Scheduler(WithTracingSchedulerOpts(nil, traceConfig))
	entryID, err := RegisterEvery(scheduler, 20*time.Millisecond, asynq.NewTask("trace:periodic", nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Start(); err != nil {
		t.Fatal(err)
	}
	defer scheduler.Shutdown()
	defer func() {
		_ = scheduler.Unregister(entryID)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduled traced task was not processed")
	}

	spans := spansByName(exporter.GetSpans())
	scheduled := spans["asynq schedule trace:periodic"]
	process := spans["asynq process trace:periodic"]
	if process.Parent.TraceID() != scheduled.SpanContext.TraceID() || process.Parent.SpanID() != scheduled.SpanContext.SpanID() {
		t.Fatalf("scheduled process parent = %s, want %s", process.Parent.SpanID(), scheduled.SpanContext.SpanID())
	}
}

func TestSchedulerInterfaceLifecycleAndUnregister(t *testing.T) {
	t.Parallel()
	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()

	var scheduler = broker.Scheduler(nil)
	entryID, err := RegisterEvery(scheduler, time.Second, asynq.NewTask("periodic:unregister", nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Start(); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Unregister(entryID); err != nil {
		t.Fatal(err)
	}
	scheduler.Shutdown()
}

func TestInspectorQueueLifecycleThroughInterface(t *testing.T) {
	t.Parallel()
	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()

	var inspector = broker.Inspector()
	if err := inspector.PauseQueue("ops"); err != nil {
		t.Fatal(err)
	}

	queues, err := inspector.Queues()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(queues, "ops") {
		t.Fatalf("expected queue list to contain paused queue, got %v", queues)
	}

	info, err := inspector.GetQueueInfo("ops")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Paused {
		t.Fatal("expected queue to be paused")
	}

	if err := inspector.UnpauseQueue("ops"); err != nil {
		t.Fatal(err)
	}

	info, err = inspector.GetQueueInfo("ops")
	if err != nil {
		t.Fatal(err)
	}
	if info.Paused {
		t.Fatal("expected queue to be unpaused")
	}
}

func TestRegisterEveryRejectsInvalidInterval(t *testing.T) {
	t.Parallel()
	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()
	_, err := RegisterEvery(broker.Scheduler(nil), 0, asynq.NewTask("bad", nil))
	require.Error(t, err)
}

func TestUpsertRecurringTaskAddsRecurringHeaders(t *testing.T) {
	t.Parallel()

	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()

	_, err := UpsertRecurringTask(t.Context(), broker.Client(), broker.Inspector(), RecurringTaskSpec[string]{
		Key:      "job-1",
		Task:     asynq.NewTask("recurring:roundtrip", []byte(`{"name":"alpha"}`)),
		Interval: time.Hour,
	})
	require.NoError(t, err)

	pending, err := broker.Inspector().ListPendingTasks("default")
	require.NoError(t, err)
	require.Len(t, pending, 1)

	key, ok, err := recurringTaskKey[string](pending[0].Headers)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "job-1", key)
	state, ok, err := decodeRecurringTaskState(pending[0].Headers)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "default", state.Queue)
	assert.Equal(t, time.Hour, state.Interval)
	version, ok := recurringTaskVersion(pending[0].Headers)
	assert.True(t, ok)
	assert.NotEmpty(t, version)
	assert.Equal(t, []byte(`{"name":"alpha"}`), pending[0].Payload)
}

func TestUpsertRecurringTaskUpdateSchedulesNextRun(t *testing.T) {
	t.Parallel()

	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()

	client := broker.Client()
	inspector := broker.Inspector()
	ctx := t.Context()

	_, err := UpsertRecurringTask(ctx, client, inspector, RecurringTaskSpec[string]{
		Key:      "job-1",
		Task:     asynq.NewTask("recurring:upsert", []byte("first")),
		Interval: time.Hour,
	})
	require.NoError(t, err)

	_, err = UpsertRecurringTask(ctx, client, inspector, RecurringTaskSpec[string]{
		Key:      "job-1",
		Task:     asynq.NewTask("recurring:upsert", []byte("second")),
		Interval: 2 * time.Hour,
	})
	require.NoError(t, err)

	pending, err := inspector.ListPendingTasks("default")
	require.NoError(t, err)
	assert.Len(t, pending, 0)

	scheduled, err := inspector.ListScheduledTasks("default")
	require.NoError(t, err)
	require.Len(t, scheduled, 1)

	key, ok, err := recurringTaskKey[string](scheduled[0].Headers)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "job-1", key)
	state, ok, err := decodeRecurringTaskState(scheduled[0].Headers)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, 2*time.Hour, state.Interval)
	assert.Equal(t, []byte("second"), scheduled[0].Payload)
}

func TestDeleteRecurringTaskRemovesPendingScheduledAndRetryCopies(t *testing.T) {
	t.Parallel()

	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()

	client := broker.Client()
	inspector := broker.Inspector()
	srv := broker.Server(asynq.Config{
		Concurrency: 1,
		RetryDelayFunc: func(n int, err error, task *asynq.Task) time.Duration {
			return time.Hour
		},
	})
	defer srv.Shutdown()

	failed := make(chan struct{}, 1)
	if err := srv.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		failed <- struct{}{}
		return errors.New("boom")
	})); err != nil {
		t.Fatal(err)
	}

	retryTask := asynq.NewTaskWithHeaders("recurring:delete", []byte("retry"), map[string]string{
		recurringTaskKeyHeader: `"job-1"`,
	})
	_, err := client.Enqueue(retryTask, asynq.MaxRetry(2))
	require.NoError(t, err)

	select {
	case <-failed:
	case <-time.After(2 * time.Second):
		t.Fatal("task did not enter retry")
	}

	pendingTask := asynq.NewTaskWithHeaders("recurring:delete", []byte("pending"), map[string]string{
		recurringTaskKeyHeader: `"job-1"`,
	})
	_, err = client.Enqueue(pendingTask, asynq.Queue("pending"))
	require.NoError(t, err)

	scheduledTask := asynq.NewTaskWithHeaders("recurring:delete", []byte("scheduled"), map[string]string{
		recurringTaskKeyHeader: `"job-1"`,
	})
	_, err = client.Enqueue(scheduledTask, asynq.ProcessIn(time.Hour))
	require.NoError(t, err)

	deleted, err := DeleteRecurringTask(t.Context(), inspector, "job-1")
	require.NoError(t, err)
	assert.Equal(t, 3, deleted)
}

func TestSelfReschedulingTaskMiddlewareReenqueuesLatestConfig(t *testing.T) {
	t.Parallel()

	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()

	client := broker.Client()
	inspector := broker.Inspector()

	server := broker.Server(asynq.Config{Concurrency: 1})
	server.Use(SelfReschedulingTaskMiddleware(client, inspector))
	defer server.Shutdown()

	done := make(chan struct{}, 1)
	if err := server.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		assert.Equal(t, "recurring:run", task.Type())
		assert.JSONEq(t, `{"count":1}`, string(task.Payload()))
		done <- struct{}{}
		return nil
	})); err != nil {
		t.Fatal(err)
	}

	if _, err := UpsertRecurringTask(t.Context(), client, inspector, RecurringTaskSpec[string]{
		Key:      "job-1",
		Task:     asynq.NewTask("recurring:run", []byte(`{"count":1}`)),
		Interval: 10 * time.Millisecond,
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("recurring task did not run")
	}

	var scheduled []*asynq.TaskInfo
	waitFor(t, 2*time.Second, func() bool {
		var err error
		scheduled, err = inspector.ListScheduledTasks("default")
		if err != nil {
			t.Fatalf("ListScheduledTasks() error = %v", err)
		}
		return len(scheduled) == 1
	})

	key, ok, err := recurringTaskKey[string](scheduled[0].Headers)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "job-1", key)
	state, ok, err := decodeRecurringTaskState(scheduled[0].Headers)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "default", state.Queue)
	assert.Equal(t, 10*time.Millisecond, state.Interval)
	assert.JSONEq(t, `{"count":1}`, string(scheduled[0].Payload))
}

func TestSelfReschedulingTaskMiddlewareSkipsNonRecurringTasks(t *testing.T) {
	t.Parallel()

	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()

	client := broker.Client()
	inspector := broker.Inspector()

	server := broker.Server(asynq.Config{Concurrency: 1})
	server.Use(SelfReschedulingTaskMiddleware(client, inspector))
	defer server.Shutdown()

	done := make(chan struct{}, 1)
	require.NoError(t, server.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		done <- struct{}{}
		return nil
	})))

	_, err := client.Enqueue(asynq.NewTask("normal:task", []byte(`{"name":"once"}`)))
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("non-recurring task did not run")
	}

	scheduled, err := inspector.ListScheduledTasks("default")
	require.NoError(t, err)
	assert.Len(t, scheduled, 0)
}

func TestRecurringTaskDeleteWhileActivePreventsReenqueue(t *testing.T) {
	t.Parallel()

	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()

	client := broker.Client()
	inspector := broker.Inspector()

	server := broker.Server(asynq.Config{Concurrency: 1})
	server.Use(SelfReschedulingTaskMiddleware(client, inspector))
	defer server.Shutdown()

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	require.NoError(t, server.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		started <- struct{}{}
		<-release
		return nil
	})))

	_, err := UpsertRecurringTask(t.Context(), client, inspector, RecurringTaskSpec[string]{
		Key:      "job-1",
		Task:     asynq.NewTask("recurring:delete-active", []byte(`{"count":1}`)),
		Interval: time.Hour,
	})
	require.NoError(t, err)

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("recurring task did not start")
	}

	deleted, err := DeleteRecurringTask(t.Context(), inspector, "job-1")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, deleted, 0)

	close(release)
	time.Sleep(50 * time.Millisecond)

	scheduled, err := inspector.ListScheduledTasks("default")
	require.NoError(t, err)
	assert.Len(t, scheduled, 0)
}

func TestRecurringTaskUpsertWhileActivePreventsOldReenqueue(t *testing.T) {
	t.Parallel()

	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()

	client := broker.Client()
	inspector := broker.Inspector()

	server := broker.Server(asynq.Config{Concurrency: 1})
	server.Use(SelfReschedulingTaskMiddleware(client, inspector))
	defer server.Shutdown()

	started := make(chan string, 2)
	release := make(chan struct{})
	require.NoError(t, server.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		started <- string(task.Payload())
		if string(task.Payload()) == `{"count":1}` {
			<-release
		}
		return nil
	})))

	_, err := UpsertRecurringTask(t.Context(), client, inspector, RecurringTaskSpec[string]{
		Key:      "job-1",
		Task:     asynq.NewTask("recurring:update-active", []byte(`{"count":1}`)),
		Interval: time.Hour,
	})
	require.NoError(t, err)

	select {
	case payload := <-started:
		assert.JSONEq(t, `{"count":1}`, payload)
	case <-time.After(2 * time.Second):
		t.Fatal("initial recurring task did not start")
	}

	_, err = UpsertRecurringTask(t.Context(), client, inspector, RecurringTaskSpec[string]{
		Key:      "job-1",
		Task:     asynq.NewTask("recurring:update-active", []byte(`{"count":2}`)),
		Interval: 25 * time.Millisecond,
	})
	require.NoError(t, err)

	close(release)

	var scheduled []*asynq.TaskInfo
	waitFor(t, 2*time.Second, func() bool {
		var err error
		scheduled, err = inspector.ListScheduledTasks("default")
		require.NoError(t, err)
		return len(scheduled) == 1
	})

	assert.JSONEq(t, `{"count":2}`, string(scheduled[0].Payload))
	state, ok, err := decodeRecurringTaskState(scheduled[0].Headers)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, 25*time.Millisecond, state.Interval)
	assert.Len(t, started, 0)
}

func TestRecurringTaskRedisInitialInsertRunsImmediately(t *testing.T) {
	t.Parallel()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	broker, err := New(Config{
		Mode: ModeRedis,
		Redis: asynq.RedisClientOpt{
			Addr: mr.Addr(),
		},
	})
	require.NoError(t, err)
	defer broker.Close()

	client := broker.Client()
	inspector := broker.Inspector()

	server := broker.Server(asynq.Config{Concurrency: 1})
	server.Use(SelfReschedulingTaskMiddleware(client, inspector))
	defer server.Shutdown()

	done := make(chan struct{}, 1)
	require.NoError(t, server.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		done <- struct{}{}
		return nil
	})))

	_, err = UpsertRecurringTask(t.Context(), client, inspector, RecurringTaskSpec[string]{
		Key:      "job-redis-1",
		Task:     asynq.NewTask("redis:recurring", []byte(`{"count":1}`)),
		Interval: 25 * time.Millisecond,
	})
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("redis recurring task did not run immediately")
	}

	var scheduled []*asynq.TaskInfo
	waitFor(t, 2*time.Second, func() bool {
		var err error
		scheduled, err = inspector.ListScheduledTasks("default")
		require.NoError(t, err)
		return len(scheduled) == 1
	})

	assert.JSONEq(t, `{"count":1}`, string(scheduled[0].Payload))
}

func TestRecurringTaskRedisDeleteWhileActivePreventsReenqueue(t *testing.T) {
	t.Parallel()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	broker, err := New(Config{
		Mode: ModeRedis,
		Redis: asynq.RedisClientOpt{
			Addr: mr.Addr(),
		},
	})
	require.NoError(t, err)
	defer broker.Close()

	client := broker.Client()
	inspector := broker.Inspector()

	server := broker.Server(asynq.Config{Concurrency: 1})
	server.Use(SelfReschedulingTaskMiddleware(client, inspector))
	defer server.Shutdown()

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	require.NoError(t, server.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		started <- struct{}{}
		<-release
		return nil
	})))

	_, err = UpsertRecurringTask(t.Context(), client, inspector, RecurringTaskSpec[string]{
		Key:      "job-redis-2",
		Task:     asynq.NewTask("redis:recurring-delete", []byte(`{"count":1}`)),
		Interval: 50 * time.Millisecond,
	})
	require.NoError(t, err)

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("redis recurring task did not start")
	}

	_, err = DeleteRecurringTask(t.Context(), inspector, "job-redis-2")
	require.NoError(t, err)
	close(release)

	time.Sleep(100 * time.Millisecond)
	scheduled, err := inspector.ListScheduledTasks("default")
	require.NoError(t, err)
	assert.Len(t, scheduled, 0)
}

func TestRecurringTaskRedisStateUsesConfiguredPrefix(t *testing.T) {
	t.Parallel()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	broker, err := New(Config{
		Mode:           ModeRedis,
		RedisKeyPrefix: "tenant-a",
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
	assert.Contains(t, keys, "tenant-a:asynqwrap:recurring-state:\"job-prefixed\"")
}

func TestCronSpecEvery(t *testing.T) {
	t.Parallel()

	spec, err := CronSpecEvery(5 * time.Minute)
	require.NoError(t, err)
	assert.Equal(t, "@every 5m0s", spec)
}

func TestMetricsHandler(t *testing.T) {
	t.Parallel()
	broker, _ := New(Config{Mode: ModeMemory})
	defer broker.Close()
	if _, err := broker.Client().Enqueue(asynq.NewTask("metrics", nil)); err != nil {
		t.Fatal(err)
	}
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	broker.MetricsHandler().ServeHTTP(resp, req)
	body, _ := io.ReadAll(resp.Result().Body)
	if !strings.Contains(string(body), "asynqwrap_queue_tasks") {
		t.Fatalf("metrics output missing queue metric: %s", string(body))
	}
}

func TestRedisRequiresConfig(t *testing.T) {
	t.Parallel()
	if _, err := New(Config{Mode: ModeRedis}); err == nil {
		t.Fatal("expected redis config error")
	}
}

func TestNewFromRedisClientRejectsNil(t *testing.T) {
	t.Parallel()
	var client redis.UniversalClient
	if _, err := NewFromRedisClient(client); err == nil {
		t.Fatal("expected nil redis client error")
	}
}

func spansByName(spans tracetest.SpanStubs) map[string]tracetest.SpanStub {
	out := make(map[string]tracetest.SpanStub, len(spans))
	for _, span := range spans {
		out[span.Name] = span
	}
	return out
}

func attributeValue(attrs []attribute.KeyValue, key string) string {
	for _, attr := range attrs {
		if string(attr.Key) == key {
			return attr.Value.AsString()
		}
	}
	return ""
}

func waitForRetryTasks(t *testing.T, inspector Inspector, queue string, want int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		tasks, err := inspector.ListRetryTasks(queue)
		if err != nil {
			t.Fatalf("ListRetryTasks(%q) error = %v", queue, err)
		}
		if len(tasks) == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	tasks, err := inspector.ListRetryTasks(queue)
	if err != nil {
		t.Fatalf("ListRetryTasks(%q) error = %v", queue, err)
	}
	t.Fatalf("retry task count = %d, want %d", len(tasks), want)
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}
