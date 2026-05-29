package asynqwrap

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/samber/oops"
)

type spfyInspector struct {
	broker    *spfyBroker
	recurring recurringStateStore
}

func (i *spfyInspector) recurringStateStore() recurringStateStore { return i.recurring }
func (i *spfyInspector) Close() error                             { return nil }

func (i *spfyInspector) Groups(queue string) ([]*asynq.GroupInfo, error) {
	ctx := context.Background()
	tasks, err := i.broker.listTaskEnvelopesByFilter(ctx, []string{queue}, func(doc *spfyTaskDoc) bool {
		return doc.State == asynq.TaskStateAggregating.String()
	}, func(a, b *spfyTaskDoc) int {
		if cmp := strings.Compare(a.Group, b.Group); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.ID, b.ID)
	}, 0)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, env := range tasks {
		doc, _ := env.snapshot()
		counts[doc.Group]++
	}
	out := make([]*asynq.GroupInfo, 0, len(counts))
	for group, count := range counts {
		out = append(out, &asynq.GroupInfo{Group: group, Size: count})
	}
	slices.SortFunc(out, func(a, b *asynq.GroupInfo) int { return strings.Compare(a.Group, b.Group) })
	return out, nil
}

func (i *spfyInspector) GetQueueInfo(queue string) (*asynq.QueueInfo, error) {
	ctx := context.Background()
	now := i.broker.now()
	_ = i.broker.promoteDue(ctx, now)
	qdoc, _, ok, err := i.broker.getQueueDoc(ctx, queue)
	if err != nil {
		return nil, err
	}
	if !ok {
		qdoc = &spfyQueueDoc{Kind: spfyKindQueue, Queue: queue}
	}
	info := &asynq.QueueInfo{
		Queue:          queue,
		Paused:         qdoc.Paused,
		ProcessedTotal: int(qdoc.ProcessedTotal),
		FailedTotal:    int(qdoc.FailedTotal),
		Processed:      int(qdoc.ProcessedTotal),
		Failed:         int(qdoc.FailedTotal),
		Timestamp:      now,
	}
	states := []asynq.TaskState{
		asynq.TaskStatePending,
		asynq.TaskStateActive,
		asynq.TaskStateScheduled,
		asynq.TaskStateRetry,
		asynq.TaskStateArchived,
		asynq.TaskStateCompleted,
		asynq.TaskStateAggregating,
	}
	for _, st := range states {
		tasks, err := i.listByState(ctx, queue, st)
		if err != nil {
			return nil, err
		}
		switch st {
		case asynq.TaskStatePending:
			info.Pending = len(tasks)
			if len(tasks) > 0 {
				oldest := tasks[0]
				info.Latency = now.Sub(oldest.NextProcessAt)
			}
		case asynq.TaskStateActive:
			info.Active = len(tasks)
		case asynq.TaskStateScheduled:
			info.Scheduled = len(tasks)
		case asynq.TaskStateRetry:
			info.Retry = len(tasks)
		case asynq.TaskStateArchived:
			info.Archived = len(tasks)
		case asynq.TaskStateCompleted:
			info.Completed = len(tasks)
		case asynq.TaskStateAggregating:
			info.Aggregating = len(tasks)
			groups := make(map[string]struct{})
			for _, t := range tasks {
				groups[t.Group] = struct{}{}
			}
			info.Groups = len(groups)
		}
	}
	info.Size = info.Pending +
		info.Active +
		info.Scheduled +
		info.Retry +
		info.Archived +
		info.Completed +
		info.Aggregating
	return info, nil
}

func (i *spfyInspector) GetTaskInfo(queue, id string) (*asynq.TaskInfo, error) {
	env, ok, err := i.broker.getTask(context.Background(), id)
	if err != nil {
		return nil, err
	}
	if !ok || env.doc.Queue != queue {
		return nil, oops.Errorf("task %q not found in queue %q", id, queue)
	}
	return env.doc.taskInfo(), nil
}

func (i *spfyInspector) ListActiveTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	return i.listByStateWithOptions(queue, asynq.TaskStateActive, opts...)
}
func (i *spfyInspector) ListCompletedTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	return i.listByStateWithOptions(queue, asynq.TaskStateCompleted, opts...)
}
func (i *spfyInspector) ListPendingTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	return i.listByStateWithOptions(queue, asynq.TaskStatePending, opts...)
}
func (i *spfyInspector) ListArchivedTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	return i.listByStateWithOptions(queue, asynq.TaskStateArchived, opts...)
}
func (i *spfyInspector) ListRetryTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	return i.listByStateWithOptions(queue, asynq.TaskStateRetry, opts...)
}
func (i *spfyInspector) ListScheduledTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	return i.listByStateWithOptions(queue, asynq.TaskStateScheduled, opts...)
}

func (i *spfyInspector) listByStateWithOptions(
	queue string,
	st asynq.TaskState,
	opts ...asynq.ListOption,
) ([]*asynq.TaskInfo, error) {
	tasks, err := i.listByState(context.Background(), queue, st)
	if err != nil {
		return nil, err
	}
	listOpts := composeListOptions(opts...)
	size := max(listOpts.pageSize, 1)
	pageNum := max(listOpts.pageNum-1, 0)
	start := pageNum * size
	end := min(start+size, len(tasks))
	var out []*asynq.TaskInfo
	for idx := start; idx < end; idx++ {
		out = append(out, tasks[idx])
	}
	return out, nil
}

func (i *spfyInspector) listByState(
	ctx context.Context,
	queue string,
	st asynq.TaskState,
) ([]*asynq.TaskInfo, error) {
	tasks, err := i.broker.listTaskEnvelopesByFilter(ctx, []string{queue}, func(doc *spfyTaskDoc) bool {
		return doc.State == st.String()
	}, func(a, b *spfyTaskDoc) int {
		if a.NextProcessAtUnixNano != b.NextProcessAtUnixNano {
			return compareInt64(a.NextProcessAtUnixNano, b.NextProcessAtUnixNano)
		}
		return strings.Compare(a.ID, b.ID)
	}, 0)
	if err != nil {
		return nil, err
	}
	out := make([]*asynq.TaskInfo, 0, len(tasks))
	for _, env := range tasks {
		out = append(out, env.doc.taskInfo())
	}
	return out, nil
}

func (i *spfyInspector) ArchiveTask(queue, id string) error {
	return i.transitionTask(queue, id, func(doc *spfyTaskDoc) error {
		switch doc.State {
		case asynq.TaskStatePending.String(), asynq.TaskStateScheduled.String(), asynq.TaskStateRetry.String():
		default:
			return oops.Errorf("task %q in queue %q cannot be archived from state %s", id, queue, doc.State)
		}
		doc.State = asynq.TaskStateArchived.String()
		doc.ProcessAt = time.Time{}
		doc.NextProcessAt = time.Time{}
		return nil
	}, false)
}

func (i *spfyInspector) DeleteTask(queue, id string) error {
	ctx := context.Background()

	for range 5 {
		env, ok, err := i.broker.getTask(ctx, id)
		if err != nil {
			return err
		}
		if !ok || env.doc.Queue != queue {
			return oops.Errorf("task %q not found in queue %q", id, queue)
		}
		if env.doc.State == asynq.TaskStateActive.String() {
			return oops.Errorf("task %q is already active in queue %q", id, queue)
		}

		if i.broker.supportsTransactions() {
			ops := []spfyStateOperation{
				spfyDeleteRequest(env.key, env.currentETag(), i.broker.stateMetadata(0)),
			}
			if env.doc.UniqueKey != "" {
				ops = append(
					ops,
					spfyDeleteRequest(i.broker.uniqueKey(env.doc.UniqueKey), nil, i.broker.stateMetadata(0)),
				)
			}

			taskSetDoc, taskSetETag, taskSetOK, err := i.broker.getStringSetDoc(ctx, i.broker.queueTasksKey(queue))
			if err != nil {
				return err
			}
			if taskSetOK {
				ops = append(
					ops,
					spfySetRequest(
						i.broker.queueTasksKey(queue),
						removeFromStringSetDoc(taskSetDoc, id),
						taskSetETag,
						false,
						i.broker.stateMetadata(0),
					),
				)
			}

			if err := i.broker.transaction(ctx, ops...); err != nil {
				if isETagConflict(err) {
					continue
				}
				return err
			}
			return nil
		}

		if err := i.broker.deleteStateKey(ctx, env.key, env.currentETag()); err != nil {
			if isETagConflict(err) {
				continue
			}
			return err
		}
		if env.doc.UniqueKey != "" {
			_ = i.broker.deleteStateKey(ctx, i.broker.uniqueKey(env.doc.UniqueKey), nil)
		}
		_ = i.broker.removeStringSetItems(ctx, i.broker.queueTasksKey(queue), id)
		return nil
	}

	return oops.Errorf("delete task %q from queue %q failed after ETag retries", id, queue)
}

func (i *spfyInspector) DeleteAllArchivedTasks(queue string) (int, error) {
	return i.deleteAll(queue, asynq.TaskStateArchived)
}
func (i *spfyInspector) DeleteAllRetryTasks(queue string) (int, error) {
	return i.deleteAll(queue, asynq.TaskStateRetry)
}

func (i *spfyInspector) deleteAll(queue string, st asynq.TaskState) (int, error) {
	tasks, err := i.listByState(context.Background(), queue, st)
	if err != nil {
		return 0, err
	}
	var deleted int
	for _, task := range tasks {
		if err := i.DeleteTask(queue, task.ID); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func (i *spfyInspector) PauseQueue(queue string) error {
	return i.updateQueue(queue, func(doc *spfyQueueDoc) { doc.Paused = true })
}

func (i *spfyInspector) UnpauseQueue(queue string) error {
	if err := i.updateQueue(queue, func(doc *spfyQueueDoc) { doc.Paused = false }); err != nil {
		return err
	}
	return i.broker.notifyQueue(context.Background(), queue)
}

func (i *spfyInspector) Queues() ([]string, error) {
	return i.broker.listQueues(context.Background())
}

func (i *spfyInspector) RunTask(queue, id string) error {
	return i.transitionTask(queue, id, func(doc *spfyTaskDoc) error {
		switch doc.State {
		case asynq.TaskStateScheduled.String(), asynq.TaskStateRetry.String(), asynq.TaskStateArchived.String():
		default:
			return oops.Errorf("task %q in queue %q cannot be run from state %s", id, queue, doc.State)
		}
		doc.State = asynq.TaskStatePending.String()
		doc.ProcessAt = time.Time{}
		doc.NextProcessAt = i.broker.now()
		return nil
	}, true)
}

func (i *spfyInspector) RunAllArchivedTasks(queue string) (int, error) {
	return i.runAll(queue, asynq.TaskStateArchived)
}
func (i *spfyInspector) RunAllRetryTasks(queue string) (int, error) {
	return i.runAll(queue, asynq.TaskStateRetry)
}
func (i *spfyInspector) RunAllScheduledTasks(queue string) (int, error) {
	return i.runAll(queue, asynq.TaskStateScheduled)
}

func (i *spfyInspector) runAll(queue string, st asynq.TaskState) (int, error) {
	tasks, err := i.listByState(context.Background(), queue, st)
	if err != nil {
		return 0, err
	}
	var ran int
	for _, task := range tasks {
		if err := i.RunTask(queue, task.ID); err != nil {
			return ran, err
		}
		ran++
	}
	return ran, nil
}

func (i *spfyInspector) updateQueue(queue string, mutate func(*spfyQueueDoc)) error {
	ctx := context.Background()
	if err := i.broker.ensureQueue(ctx, queue); err != nil {
		return err
	}
	for range 5 {
		doc, etag, ok, err := i.broker.getQueueDoc(ctx, queue)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		mutate(doc)
		doc.UpdatedAt = i.broker.now()
		if err := i.broker.applyStateOperation(
			ctx,
			spfySetRequest(i.broker.queueKey(queue), doc, etag, false, i.broker.stateMetadata(0)),
		); err != nil {
			if isETagConflict(err) {
				continue
			}
			return err
		}
		return nil
	}
	return oops.New("update queue failed after ETag retries")
}

func (i *spfyInspector) transitionTask(queue, id string, mutate func(*spfyTaskDoc) error, notify bool) error {
	ctx := context.Background()
	for range 5 {
		env, ok, err := i.broker.getTask(ctx, id)
		if err != nil {
			return err
		}
		if !ok || env.doc.Queue != queue {
			return oops.Errorf("task %q not found in queue %q", id, queue)
		}
		doc := env.doc.clone()
		if err := mutate(doc); err != nil {
			return err
		}
		doc.refreshUnixFields()
		if err := i.broker.applyStateOperation(
			ctx,
			spfySetRequest(env.key, doc, env.currentETag(), false, i.broker.stateMetadata(0)),
		); err != nil {
			if isETagConflict(err) {
				continue
			}
			return err
		}
		if notify {
			_ = i.broker.notifyQueue(ctx, queue)
		}
		return nil
	}
	return oops.New("transition task failed after ETag retries")
}
