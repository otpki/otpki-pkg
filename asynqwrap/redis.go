package asynqwrap

import (
	"net/http"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

type redisBroker struct {
	opt       asynq.RedisConnOpt
	client    redis.UniversalClient
	inspector *asynq.Inspector
	recurring recurringStateStore
	metrics   http.Handler
}

func newRedisBroker(opt asynq.RedisConnOpt, redisKeyPrefix string) Broker {
	client, ok := opt.MakeRedisClient().(redis.UniversalClient)
	if !ok {
		panic("asynqwrap: unsupported RedisConnOpt")
	}
	inspector := asynq.NewInspectorFromRedisClient(client)
	return &redisBroker{
		opt:       opt,
		client:    client,
		inspector: inspector,
		recurring: newRedisRecurringStateStore(client, redisKeyPrefix),
		metrics:   newMetricsHandler(newRedisCollector(inspector)),
	}
}

func newRedisBrokerFromClient(client redis.UniversalClient, redisKeyPrefix string) Broker {
	inspector := asynq.NewInspectorFromRedisClient(client)
	return &redisBroker{
		client:    client,
		inspector: inspector,
		recurring: newRedisRecurringStateStore(client, redisKeyPrefix),
		metrics:   newMetricsHandler(newRedisCollector(inspector)),
	}
}

func (b *redisBroker) Client() Client {
	return newClientAdapter(asynq.NewClientFromRedisClient(b.client))
}

func (b *redisBroker) Server(cfg asynq.Config) Server {
	return newServerAdapter(asynq.NewServerFromRedisClient(b.client, cfg))
}

func (b *redisBroker) Scheduler(opts *asynq.SchedulerOpts) Scheduler {
	return asynq.NewSchedulerFromRedisClient(b.client, opts)
}

func (b *redisBroker) PeriodicTaskManager(opts PeriodicTaskManagerOpts) (PeriodicTaskManager, error) {
	opts.Broker = b
	return NewPeriodicTaskManager(opts)
}

func (b *redisBroker) Inspector() Inspector {
	return &redisInspector{inspector: b.inspector, recurring: b.recurring}
}

func (b *redisBroker) MetricsHandler() http.Handler {
	return b.metrics
}

func (b *redisBroker) Close() error {
	if b.client != nil {
		return b.client.Close()
	}
	return nil
}

type redisInspector struct {
	inspector *asynq.Inspector
	recurring recurringStateStore
}

func (i *redisInspector) recurringStateStore() recurringStateStore { return i.recurring }

func (i *redisInspector) Close() error {
	return nil
}

func (i *redisInspector) Groups(queue string) ([]*asynq.GroupInfo, error) {
	return i.inspector.Groups(queue)
}

func (i *redisInspector) GetQueueInfo(queue string) (*asynq.QueueInfo, error) {
	return i.inspector.GetQueueInfo(queue)
}

func (i *redisInspector) GetTaskInfo(queue, id string) (*asynq.TaskInfo, error) {
	return i.inspector.GetTaskInfo(queue, id)
}

func (i *redisInspector) ListArchivedTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	return i.inspector.ListArchivedTasks(queue, opts...)
}

func (i *redisInspector) ListCompletedTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	return i.inspector.ListCompletedTasks(queue, opts...)
}

func (i *redisInspector) ListActiveTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	return i.inspector.ListActiveTasks(queue, opts...)
}

func (i *redisInspector) ListPendingTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	return i.inspector.ListPendingTasks(queue, opts...)
}

func (i *redisInspector) ListRetryTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	return i.inspector.ListRetryTasks(queue, opts...)
}

func (i *redisInspector) ListScheduledTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	return i.inspector.ListScheduledTasks(queue, opts...)
}

func (i *redisInspector) ArchiveTask(queue, id string) error {
	return i.inspector.ArchiveTask(queue, id)
}

func (i *redisInspector) DeleteTask(queue, id string) error {
	return i.inspector.DeleteTask(queue, id)
}

func (i *redisInspector) DeleteAllArchivedTasks(queue string) (int, error) {
	return i.inspector.DeleteAllArchivedTasks(queue)
}

func (i *redisInspector) DeleteAllRetryTasks(queue string) (int, error) {
	return i.inspector.DeleteAllRetryTasks(queue)
}

func (i *redisInspector) PauseQueue(queue string) error {
	return i.inspector.PauseQueue(queue)
}

func (i *redisInspector) Queues() ([]string, error) {
	return i.inspector.Queues()
}

func (i *redisInspector) RunTask(queue, id string) error {
	return i.inspector.RunTask(queue, id)
}

func (i *redisInspector) RunAllArchivedTasks(queue string) (int, error) {
	return i.inspector.RunAllArchivedTasks(queue)
}

func (i *redisInspector) RunAllRetryTasks(queue string) (int, error) {
	return i.inspector.RunAllRetryTasks(queue)
}

func (i *redisInspector) RunAllScheduledTasks(queue string) (int, error) {
	return i.inspector.RunAllScheduledTasks(queue)
}

func (i *redisInspector) UnpauseQueue(queue string) error {
	return i.inspector.UnpauseQueue(queue)
}
