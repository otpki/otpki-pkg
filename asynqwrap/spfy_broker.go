package asynqwrap

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	spfystate "github.com/otpki/otpki-pkg/spfy/anchor"
	spfypubsub "github.com/otpki/otpki-pkg/spfy/pulse"
	"github.com/samber/oops"
)

type spfyBroker struct {
	store   spfystate.Store[[]byte]
	pubsub  spfypubsub.Backend
	cfg     Config
	metrics http.Handler
}

func newSpfyBroker(cfg Config) (Broker, error) {
	if cfg.Spfy.StateStore == nil {
		return nil, oops.New("spfy mode requires StateStore")
	}
	if cfg.Spfy.PubSub == nil {
		return nil, oops.New("spfy mode requires PubSub")
	}

	if cfg.Spfy.WorkerLeaseTTL <= 0 {
		cfg.Spfy.WorkerLeaseTTL = spfyDefaultLeaseTTL
	}
	if cfg.Spfy.PollInterval <= 0 {
		cfg.Spfy.PollInterval = spfyDefaultPollInterval
	}
	if cfg.Spfy.QueryPageSize <= 0 {
		cfg.Spfy.QueryPageSize = spfyDefaultQueryLimit
	}
	if cfg.Spfy.Now == nil {
		cfg.Spfy.Now = time.Now
	}

	b := &spfyBroker{
		store:  cfg.Spfy.StateStore,
		pubsub: cfg.Spfy.PubSub,
		cfg:    cfg,
	}
	b.metrics = newMetricsHandler(newSpfyCollector(b))
	return b, nil
}

func (b *spfyBroker) Client() Client {
	return newClientAdapter(&spfyClient{broker: b})
}

func (b *spfyBroker) Server(cfg asynq.Config) Server {
	return newServerAdapter(&spfyServer{
		broker:   b,
		cfg:      normalizeConfig(cfg),
		serverID: uuid.NewString(),
		logger:   newLeveledLogger(cfg.Logger, cfg.LogLevel),
		done:     make(chan struct{}),
		wake:     make(chan string, 1024),
	})
}

func (b *spfyBroker) Scheduler(opts *asynq.SchedulerOpts) Scheduler {
	return newSpfyScheduler(b, opts)
}

func (b *spfyBroker) PeriodicTaskManager(opts PeriodicTaskManagerOpts) (PeriodicTaskManager, error) {
	opts.Broker = b
	return NewPeriodicTaskManager(opts)
}

func (b *spfyBroker) Inspector() Inspector {
	return &spfyInspector{broker: b, recurring: &spfyRecurringStateStore{broker: b}}
}

func (b *spfyBroker) MetricsHandler() http.Handler { return b.metrics }

func (b *spfyBroker) Close() error {
	return nil
}

func (b *spfyBroker) now() time.Time { return b.cfg.Spfy.Now() }

func (b *spfyBroker) taskKey(id string) string { return b.stateKey("task", id) }
func (b *spfyBroker) queuesKey() string        { return b.stateKey("queues") }
func (b *spfyBroker) queueKey(queue string) string {
	return b.stateKey("queue", queue)
}
func (b *spfyBroker) queueTasksKey(queue string) string {
	return b.stateKey("queue-tasks", queue)
}
func (b *spfyBroker) uniqueKey(key string) string {
	return b.stateKey("unique", key)
}
func (b *spfyBroker) recurringKey(key string) string {
	return b.stateKey("recurring-state", key)
}
func (b *spfyBroker) topic(queue string) string {
	return prefixedTopicPath(b.cfg.RuntimeConfig, "queue", queue)
}

func (b *spfyBroker) stateKey(parts ...string) string {
	return prefixedStatePath(b.cfg.RuntimeConfig, parts...)
}

func (b *spfyBroker) stateMetadata(ttl time.Duration) spfystate.Metadata {
	out := make(spfystate.Metadata, len(b.cfg.Spfy.StateMetadata)+1)
	for k, v := range b.cfg.Spfy.StateMetadata {
		out[k] = []string{v}
	}
	if ttl > 0 {
		seconds := max(int64((ttl+time.Second-1)/time.Second), 1)
		out[spfyMetadataTTL] = []string{strconv.FormatInt(seconds, 10)}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (b *spfyBroker) pubsubMetadata() spfypubsub.Header {
	if len(b.cfg.Spfy.PubSubMetadata) == 0 {
		return nil
	}
	out := make(spfypubsub.Header, len(b.cfg.Spfy.PubSubMetadata))
	for k, v := range b.cfg.Spfy.PubSubMetadata {
		out[k] = []string{v}
	}
	return out
}

func (b *spfyBroker) notifyQueue(ctx context.Context, queue string) error {
	body, _ := json.Marshal(map[string]string{spfyKindQueue: queue})
	return b.pubsub.Publish(
		ctx,
		b.topic(queue),
		spfypubsub.Message{Data: body, Header: b.pubsubMetadata()},
		spfypubsub.PublishOptions{Reliability: spfypubsub.Ephemeral},
	)
}

func (b *spfyBroker) supportsTransactions() bool { return true }

func (b *spfyBroker) transaction(ctx context.Context, ops ...spfyStateOperation) error {
	batch := make([]spfystate.Operation[[]byte], 0, len(ops))
	for _, op := range ops {
		encoded, err := encodeStateValue(op.Value)
		if err != nil {
			return err
		}
		batch = append(batch, spfystate.Operation[[]byte]{
			Type:     op.Type,
			Key:      op.Key,
			Value:    encoded,
			Expected: stateETag(op.Expected),
			Options:  op.Options,
		})
	}
	_, err := b.store.Batch(ctx, batch)
	return err
}

func (b *spfyBroker) applyStateOperation(ctx context.Context, op spfyStateOperation) error {
	value, err := encodeStateValue(op.Value)
	if err != nil {
		return err
	}
	switch op.Type {
	case spfystate.OpCreate:
		_, err = b.store.Create(ctx, op.Key, value, op.Options)
	case spfystate.OpCompareAndSet:
		_, err = b.store.CompareAndSet(ctx, op.Key, stateETag(op.Expected), value, op.Options)
	case spfystate.OpPut:
		_, err = b.store.Put(ctx, op.Key, value, op.Options)
	case spfystate.OpDelete:
		if op.Expected != nil {
			return b.transaction(ctx, op)
		}
		err = b.store.Delete(ctx, op.Key)
	default:
		err = oops.Errorf("unsupported state operation %q", op.Type)
	}
	return err
}

func (b *spfyBroker) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := b.store.Get(ctx, b.queuesKey())
	if err != nil && !errors.Is(err, spfystate.ErrNotFound) {
		return err
	}
	return nil
}

func (b *spfyBroker) getState(ctx context.Context, key string) ([]byte, *string, bool, error) {
	entry, err := b.store.Get(ctx, key)
	if err != nil {
		if errors.Is(err, spfystate.ErrNotFound) {
			return nil, nil, false, nil
		}
		return nil, nil, false, err
	}
	return slices.Clone(entry.Value), etagString(entry.ETag), true, nil
}

func (b *spfyBroker) getTask(ctx context.Context, id string) (*spfyTaskEnvelope, bool, error) {
	key := b.taskKey(id)
	data, etag, ok, err := b.getState(ctx, key)
	if err != nil || !ok {
		return nil, false, err
	}
	doc, err := decodeSpfyTaskDoc(data)
	if err != nil {
		return nil, false, err
	}
	return &spfyTaskEnvelope{key: key, etag: etag, doc: doc}, true, nil
}

func (b *spfyBroker) getQueueDoc(ctx context.Context, queue string) (*spfyQueueDoc, *string, bool, error) {
	data, etag, ok, err := b.getState(ctx, b.queueKey(queue))
	if err != nil || !ok {
		return nil, nil, false, err
	}
	doc, err := decodeSpfyQueueDoc(data)
	if err != nil {
		return nil, nil, false, err
	}
	return doc, etag, true, nil
}

func (b *spfyBroker) getStringSetDoc(ctx context.Context, key string) (*spfyStringSetDoc, *string, bool, error) {
	data, etag, ok, err := b.getState(ctx, key)
	if err != nil || !ok {
		return nil, nil, false, err
	}
	doc, err := decodeSpfyStringSetDoc(data)
	if err != nil {
		return nil, nil, false, err
	}
	return doc, etag, true, nil
}

func (b *spfyBroker) ensureQueue(ctx context.Context, queue string) error {
	if queue == "" {
		queue = defaultRecurringTaskQueue
	}
	for range 5 {
		_, _, ok, err := b.getQueueDoc(ctx, queue)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}

		queuesDoc, queuesETag, queuesOK, err := b.getStringSetDoc(ctx, b.queuesKey())
		if err != nil {
			return err
		}

		now := b.now()
		queueDoc := &spfyQueueDoc{Kind: spfyKindQueue, Queue: queue, CreatedAt: now, UpdatedAt: now}
		indexDoc := buildStringSetDoc(spfyKindQueueSet, queuesDoc, queue)
		if err := b.applyStateOperation(
			ctx,
			spfySetRequest(b.queuesKey(), indexDoc, queuesETag, !queuesOK, b.stateMetadata(0)),
		); err != nil {
			if isETagConflict(err) {
				continue
			}
			return err
		}
		err = b.applyStateOperation(ctx, spfySetRequest(b.queueKey(queue), queueDoc, nil, true, b.stateMetadata(0)))
		if err == nil || isETagConflict(err) {
			return nil
		}
		return err
	}
	return oops.New("ensure queue failed after ETag retries")
}

func (b *spfyBroker) listQueues(ctx context.Context) ([]string, error) {
	doc, _, ok, err := b.getStringSetDoc(ctx, b.queuesKey())
	if err != nil || !ok || doc == nil {
		return nil, err
	}
	return append([]string(nil), doc.Items...), nil
}

func (b *spfyBroker) getQueueTaskIDs(ctx context.Context, queue string) ([]string, *string, bool, error) {
	doc, etag, ok, err := b.getStringSetDoc(ctx, b.queueTasksKey(queue))
	if err != nil || !ok || doc == nil {
		return nil, etag, ok, err
	}
	return append([]string(nil), doc.Items...), etag, true, nil
}

func (b *spfyBroker) loadTaskEnvelopes(ctx context.Context, ids []string) ([]*spfyTaskEnvelope, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	out := make([]*spfyTaskEnvelope, 0, len(ids))
	for _, id := range ids {
		env, ok, err := b.getTask(ctx, id)
		if err != nil {
			return nil, oops.Errorf("load task %q: %w", id, err)
		}
		if !ok || env == nil || env.doc == nil || env.doc.Kind != spfyKindTask {
			continue
		}
		out = append(out, env)
	}
	return out, nil
}

func (b *spfyBroker) listTaskEnvelopesForQueues(ctx context.Context, queues []string) ([]*spfyTaskEnvelope, error) {
	if len(queues) == 0 {
		return nil, nil
	}
	ids := make([]string, 0)
	seen := make(map[string]struct{})
	for _, queue := range queues {
		queueIDs, _, ok, err := b.getQueueTaskIDs(ctx, queue)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		for _, id := range queueIDs {
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	return b.loadTaskEnvelopes(ctx, ids)
}

func (b *spfyBroker) listTaskEnvelopesByFilter(
	ctx context.Context,
	queues []string,
	match func(*spfyTaskDoc) bool,
	sortFn func(a, b *spfyTaskDoc) int,
	limit int,
) ([]*spfyTaskEnvelope, error) {
	envs, err := b.listTaskEnvelopesForQueues(ctx, queues)
	if err != nil {
		return nil, err
	}
	filtered := make([]*spfyTaskEnvelope, 0, len(envs))
	for _, env := range envs {
		doc, _ := env.snapshot()
		if doc == nil || !match(doc) {
			continue
		}
		filtered = append(filtered, env)
	}
	slices.SortFunc(filtered, func(a, b *spfyTaskEnvelope) int {
		adoc, _ := a.snapshot()
		bdoc, _ := b.snapshot()
		return sortFn(adoc, bdoc)
	})
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

func (b *spfyBroker) updateStringSet(
	ctx context.Context,
	key string,
	mutate func(*spfyStringSetDoc) *spfyStringSetDoc,
) error {
	for range 5 {
		current, etag, ok, err := b.getStringSetDoc(ctx, key)
		if err != nil {
			return err
		}
		next := mutate(current)
		if err := b.applyStateOperation(ctx, spfySetRequest(key, next, etag, !ok, b.stateMetadata(0))); err != nil {
			if isETagConflict(err) {
				continue
			}
			return err
		}
		return nil
	}
	return oops.Errorf("update string set %q failed after ETag retries", key)
}

func (b *spfyBroker) addStringSetItems(ctx context.Context, key, kind string, items ...string) error {
	return b.updateStringSet(ctx, key, func(current *spfyStringSetDoc) *spfyStringSetDoc {
		return buildStringSetDoc(kind, current, items...)
	})
}

func (b *spfyBroker) removeStringSetItems(ctx context.Context, key string, items ...string) error {
	return b.updateStringSet(ctx, key, func(current *spfyStringSetDoc) *spfyStringSetDoc {
		return removeFromStringSetDoc(current, items...)
	})
}

func (b *spfyBroker) deleteStateKey(ctx context.Context, key string, etag *string) error {
	return b.applyStateOperation(ctx, spfyDeleteRequest(key, etag, b.stateMetadata(0)))
}

func (b *spfyBroker) isCurrentRecurringTask(ctx context.Context, headers map[string]string) (bool, error) {
	key, ok := rawRecurringTaskKey(headers)
	if !ok {
		return true, nil
	}
	version, ok := recurringTaskVersion(headers)
	if !ok {
		return true, nil
	}
	currentVersion, exists, err := (&spfyRecurringStateStore{broker: b}).Get(ctx, key)
	if err != nil {
		return false, err
	}
	return exists && currentVersion == version, nil
}

func (b *spfyBroker) discardTask(ctx context.Context, env *spfyTaskEnvelope) error {
	doc, etag := env.snapshot()
	if err := b.deleteStateKey(ctx, env.key, etag); err != nil {
		return err
	}
	if doc.UniqueKey != "" {
		_ = b.deleteStateKey(ctx, b.uniqueKey(doc.UniqueKey), nil)
	}
	_ = b.removeStringSetItems(ctx, b.queueTasksKey(doc.Queue), doc.ID)
	return nil
}

func (b *spfyBroker) promoteDue(ctx context.Context, now time.Time) error {
	states := []string{asynq.TaskStateScheduled.String(), asynq.TaskStateRetry.String()}
	queues, err := b.listQueues(ctx)
	if err != nil {
		return err
	}
	for _, st := range states {
		tasks, err := b.listTaskEnvelopesByFilter(ctx, queues, func(doc *spfyTaskDoc) bool {
			return doc.State == st && doc.NextProcessAtUnixNano <= now.UnixNano()
		}, func(a, b *spfyTaskDoc) int {
			if a.NextProcessAtUnixNano != b.NextProcessAtUnixNano {
				return compareInt64(a.NextProcessAtUnixNano, b.NextProcessAtUnixNano)
			}
			return strings.Compare(a.ID, b.ID)
		}, 0)
		if err != nil {
			return err
		}
		for _, env := range tasks {
			doc, etag := env.snapshot()
			if doc.NextProcessAt.After(now) {
				continue
			}
			doc.State = asynq.TaskStatePending.String()
			doc.ProcessAt = time.Time{}
			doc.NextProcessAt = now
			doc.refreshUnixFields()
			err := b.applyStateOperation(ctx, spfySetRequest(env.key, doc, etag, false, b.stateMetadata(0)))
			if err != nil {
				if isETagConflict(err) {
					continue
				}
				return err
			}
			_ = b.notifyQueue(ctx, doc.Queue)
		}
	}
	return nil
}

func (b *spfyBroker) recoverLeases(ctx context.Context, now time.Time, cfg asynq.Config) error {
	queues, err := b.listQueues(ctx)
	if err != nil {
		return err
	}
	tasks, err := b.listTaskEnvelopesByFilter(ctx, queues, func(doc *spfyTaskDoc) bool {
		return doc.State == asynq.TaskStateActive.String() && doc.LeaseUntilUnixNano <= now.UnixNano()
	}, func(a, b *spfyTaskDoc) int {
		if a.LeaseUntilUnixNano != b.LeaseUntilUnixNano {
			return compareInt64(a.LeaseUntilUnixNano, b.LeaseUntilUnixNano)
		}
		return strings.Compare(a.ID, b.ID)
	}, 0)
	if err != nil {
		return err
	}
	server := &spfyServer{broker: b, cfg: normalizeConfig(cfg)}
	for _, env := range tasks {
		doc, _ := env.snapshot()
		rec := doc.taskRecord()
		rec.info.State = asynq.TaskStateActive
		rec.info.IsOrphaned = true
		if err := server.failTask(ctx, env, asynq.ErrLeaseExpired, rec); err != nil && !isETagConflict(err) {
			return err
		}
	}
	return nil
}

func (b *spfyBroker) flushGroups(ctx context.Context, now time.Time, cfg asynq.Config) error {
	queues, err := b.listQueues(ctx)
	if err != nil {
		return err
	}
	tasks, err := b.listTaskEnvelopesByFilter(ctx, queues, func(doc *spfyTaskDoc) bool {
		return doc.State == asynq.TaskStateAggregating.String()
	}, func(a, b *spfyTaskDoc) int {
		if a.CreatedAtUnixNano != b.CreatedAtUnixNano {
			return compareInt64(a.CreatedAtUnixNano, b.CreatedAtUnixNano)
		}
		return strings.Compare(a.ID, b.ID)
	}, 0)
	if err != nil {
		return err
	}
	groups := make(map[string][]*spfyTaskEnvelope)
	for _, env := range tasks {
		doc, _ := env.snapshot()
		key := doc.Queue + "\x00" + doc.Group
		groups[key] = append(groups[key], env)
	}
	for _, envs := range groups {
		if len(envs) == 0 {
			continue
		}
		doc0, _ := envs[0].snapshot()
		if cfg.GroupAggregator == nil {
			if err := b.releaseGroup(ctx, envs, now); err != nil {
				return err
			}
			continue
		}
		if !groupReadyDocs(envs, cfg, now) {
			continue
		}
		if err := b.aggregateGroup(ctx, doc0.Group, envs, cfg.GroupAggregator, now); err != nil {
			return err
		}
	}
	return nil
}

func (b *spfyBroker) releaseGroup(ctx context.Context, envs []*spfyTaskEnvelope, now time.Time) error {
	if b.supportsTransactions() {
		ops := make([]spfyStateOperation, 0, len(envs))
		queues := make(map[string]struct{})
		for _, env := range envs {
			doc, etag := env.snapshot()
			doc.State = asynq.TaskStatePending.String()
			doc.NextProcessAt = now
			doc.ProcessAt = time.Time{}
			doc.refreshUnixFields()
			ops = append(ops, spfySetRequest(env.key, doc, etag, false, b.stateMetadata(0)))
			queues[doc.Queue] = struct{}{}
		}
		if len(ops) > 0 {
			if err := b.transaction(ctx, ops...); err != nil {
				if isETagConflict(err) {
					return nil
				}
				return err
			}
		}
		for q := range queues {
			_ = b.notifyQueue(ctx, q)
		}
		return nil
	}
	queues := make(map[string]struct{})
	for _, env := range envs {
		doc, etag := env.snapshot()
		doc.State = asynq.TaskStatePending.String()
		doc.NextProcessAt = now
		doc.ProcessAt = time.Time{}
		doc.refreshUnixFields()
		if err := b.applyStateOperation(
			ctx,
			spfySetRequest(env.key, doc, etag, false, b.stateMetadata(0)),
		); err != nil &&
			!isETagConflict(err) {
			return err
		}
		queues[doc.Queue] = struct{}{}
	}
	for q := range queues {
		_ = b.notifyQueue(ctx, q)
	}
	return nil
}

func (b *spfyBroker) aggregateGroup(
	ctx context.Context,
	group string,
	envs []*spfyTaskEnvelope,
	aggregator asynq.GroupAggregator,
	now time.Time,
) error {
	tasks := make([]*asynq.Task, 0, len(envs))
	for _, env := range envs {
		doc, _ := env.snapshot()
		tasks = append(
			tasks,
			asynq.NewTaskWithHeaders(doc.Type, append([]byte(nil), doc.Payload...), cloneHeaders(doc.Headers)),
		)
	}
	aggregated := aggregator.Aggregate(group, tasks)
	if aggregated == nil {
		return b.releaseGroup(ctx, envs, now)
	}
	baseDoc, baseETag := envs[0].snapshot()
	baseDoc.Type = aggregated.Type()
	baseDoc.Payload = append([]byte(nil), aggregated.Payload()...)
	baseDoc.Headers = cloneHeaders(aggregated.Headers())
	baseDoc.State = asynq.TaskStatePending.String()
	baseDoc.NextProcessAt = now
	baseDoc.ProcessAt = time.Time{}
	baseDoc.refreshUnixFields()
	if b.supportsTransactions() {
		ops := []spfyStateOperation{
			spfySetRequest(envs[0].key, baseDoc, baseETag, false, b.stateMetadata(0)),
		}
		removedIDs := make([]string, 0, max(len(envs)-1, 0))
		for _, env := range envs[1:] {
			doc, etag := env.snapshot()
			removedIDs = append(removedIDs, doc.ID)
			ops = append(ops, spfyDeleteRequest(env.key, etag, b.stateMetadata(0)))
			if doc.UniqueKey != "" {
				ops = append(ops, spfyDeleteRequest(b.uniqueKey(doc.UniqueKey), nil, b.stateMetadata(0)))
			}
		}
		if len(removedIDs) > 0 {
			taskSetDoc, taskSetETag, taskSetOK, err := b.getStringSetDoc(ctx, b.queueTasksKey(baseDoc.Queue))
			if err != nil {
				return err
			}
			if taskSetOK {
				ops = append(
					ops,
					spfySetRequest(
						b.queueTasksKey(baseDoc.Queue),
						removeFromStringSetDoc(taskSetDoc, removedIDs...),
						taskSetETag,
						false,
						b.stateMetadata(0),
					),
				)
			}
		}
		if err := b.transaction(ctx, ops...); err != nil {
			if isETagConflict(err) {
				return nil
			}
			return err
		}
		_ = b.notifyQueue(ctx, baseDoc.Queue)
		return nil
	}
	if err := b.applyStateOperation(
		ctx,
		spfySetRequest(envs[0].key, baseDoc, baseETag, false, b.stateMetadata(0)),
	); err != nil {
		if isETagConflict(err) {
			return nil
		}
		return err
	}
	removedIDs := make([]string, 0, max(len(envs)-1, 0))
	for _, env := range envs[1:] {
		doc, etag := env.snapshot()
		removedIDs = append(removedIDs, doc.ID)
		if doc.UniqueKey != "" {
			_ = b.deleteStateKey(ctx, b.uniqueKey(doc.UniqueKey), nil)
		}
		if err := b.deleteStateKey(ctx, env.key, etag); err != nil && !isETagConflict(err) {
			return err
		}
	}
	if len(removedIDs) > 0 {
		if err := b.removeStringSetItems(
			ctx,
			b.queueTasksKey(baseDoc.Queue),
			removedIDs...); err != nil &&
			!isETagConflict(err) {
			return err
		}
	}
	_ = b.notifyQueue(ctx, baseDoc.Queue)
	return nil
}
