package asynqwrap

import (
	"container/heap"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/hibiken/asynq"
	"github.com/samber/oops"
)

func (b *memoryBroker) enqueueRecordLocked(rec *taskRecord) {
	now := time.Now()

	switch {
	case rec.processAt.After(now):
		b.scheduleRecordLocked(rec, asynq.TaskStateScheduled)
	case rec.info.Group != "":
		b.enqueueGroupedRecordLocked(rec, now)
	default:
		b.enqueuePendingRecordLocked(rec, now)
	}
}

func (b *memoryBroker) scheduleRecordLocked(rec *taskRecord, state asynq.TaskState) {
	rec.info.State = state
	rec.info.NextProcessAt = rec.processAt
	heap.Push(&b.scheduled, rec)
	b.signalMaintenanceLocked()
}

func (b *memoryBroker) enqueueGroupedRecordLocked(rec *taskRecord, now time.Time) {
	q := b.getOrCreateQueueLocked(rec.info.Queue)
	group := q.groups[rec.info.Group]
	if group == nil {
		group = &taskGroup{firstAt: now}
		q.groups[rec.info.Group] = group
	}
	group.tasks = append(group.tasks, rec)
	group.lastAt = now
	rec.info.State = asynq.TaskStateAggregating
	b.signalMaintenanceLocked()
}

func (b *memoryBroker) enqueuePendingRecordLocked(rec *taskRecord, now time.Time) {
	q := b.getOrCreateQueueLocked(rec.info.Queue)
	rec.info.State = asynq.TaskStatePending
	rec.info.NextProcessAt = now
	q.pending = append(q.pending, rec)
	b.cond.Broadcast()
}

func (b *memoryBroker) getOrCreateQueueLocked(name string) *memoryQueue {
	if name == "" {
		name = defaultRecurringTaskQueue
	}
	if q := b.queues[name]; q != nil {
		return q
	}
	q := &memoryQueue{name: name, groups: make(map[string]*taskGroup)}
	b.queues[name] = q
	return q
}

func (b *memoryBroker) getQueueLocked(name string) *memoryQueue {
	return b.queues[name]
}

func (b *memoryBroker) queueNamesLocked() []string {
	out := make([]string, 0, len(b.queues))
	for name := range b.queues {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

func (b *memoryBroker) groupInfosLocked(queue string) []*asynq.GroupInfo {
	q := b.getQueueLocked(queue)
	if q == nil {
		return nil
	}
	out := make([]*asynq.GroupInfo, 0, len(q.groups))
	for name, group := range q.groups {
		out = append(out, &asynq.GroupInfo{
			Group: name,
			Size:  len(group.tasks),
		})
	}
	slices.SortFunc(out, func(a, b *asynq.GroupInfo) int {
		switch {
		case a.Group < b.Group:
			return -1
		case a.Group > b.Group:
			return 1
		default:
			return 0
		}
	})
	return out
}

func (b *memoryBroker) findTaskLocked(queue, id string) *taskRecord {
	if rec := b.tasks[id]; rec != nil && rec.info.Queue == queue {
		return rec
	}
	if rec := b.completed[id]; rec != nil && rec.info.Queue == queue {
		return rec
	}
	if rec := b.archived[id]; rec != nil && rec.info.Queue == queue {
		return rec
	}
	return nil
}

func (b *memoryBroker) listTasksByStateLocked(
	queue string,
	state asynq.TaskState,
	opts ...asynq.ListOption,
) []*asynq.TaskInfo {
	var out []*asynq.TaskInfo

	switch state {
	case asynq.TaskStateActive:
		for _, rec := range b.active {
			if rec.info.Queue == queue {
				out = append(out, cloneTaskInfo(rec.info))
			}
		}
	case asynq.TaskStatePending:
		if q := b.getQueueLocked(queue); q != nil {
			for _, rec := range q.pending {
				out = append(out, cloneTaskInfo(rec.info))
			}
		}
	case asynq.TaskStateArchived:
		for _, rec := range b.archived {
			if rec.info.Queue == queue {
				out = append(out, cloneTaskInfo(rec.info))
			}
		}
	case asynq.TaskStateScheduled:
		for _, rec := range b.scheduled {
			if rec.info.Queue == queue && rec.info.State == asynq.TaskStateScheduled {
				out = append(out, cloneTaskInfo(rec.info))
			}
		}
	case asynq.TaskStateRetry:
		for _, rec := range b.scheduled {
			if rec.info.Queue == queue && rec.info.State == asynq.TaskStateRetry {
				out = append(out, cloneTaskInfo(rec.info))
			}
		}
	case asynq.TaskStateCompleted:
		for _, rec := range b.completed {
			if rec.info.Queue == queue && rec.info.State == asynq.TaskStateCompleted {
				out = append(out, cloneTaskInfo(rec.info))
			}
		}
	case asynq.TaskStateAggregating:
		// These states are not exposed through the wrapper's list methods.
	default:
		// Unknown states are intentionally ignored.
	}

	slices.SortFunc(out, func(a, b *asynq.TaskInfo) int {
		if a.NextProcessAt.Before(b.NextProcessAt) {
			return -1
		}
		if a.NextProcessAt.After(b.NextProcessAt) {
			return 1
		}
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		default:
			return 0
		}
	})

	listOpts := composeListOptions(opts...)
	total := len(out)
	size := max(listOpts.pageSize, 1)
	pageNum := max(listOpts.pageNum-1, 0)
	start := pageNum * size
	end := min(start+size, total)
	page := []*asynq.TaskInfo{}
	for i := start; i < end; i++ {
		page = append(page, out[i])
	}

	return page
}

func (b *memoryBroker) archiveTaskLocked(queue, id string) error {
	rec := b.findTaskLocked(queue, id)
	if rec == nil {
		return oops.Errorf("task %q not found in queue %q", id, queue)
	}
	switch rec.info.State {
	case asynq.TaskStatePending, asynq.TaskStateScheduled, asynq.TaskStateRetry:
	default:
		return oops.Errorf("task %q in queue %q cannot be archived from state %s", id, queue, rec.info.State)
	}

	b.removeTaskFromCurrentStateLocked(rec)
	rec.info.State = asynq.TaskStateArchived
	rec.processAt = time.Time{}
	rec.info.NextProcessAt = time.Time{}
	b.archived[rec.info.ID] = rec
	return nil
}

func (b *memoryBroker) deleteTaskLocked(queue, id string) error {
	rec := b.findTaskLocked(queue, id)
	if rec == nil {
		return oops.Errorf("task %q not found in queue %q", id, queue)
	}
	if _, active := b.active[id]; active {
		return oops.Errorf("task %q is already active in queue %q", id, queue)
	}

	b.removeTaskFromCurrentStateLocked(rec)

	delete(b.completed, id)
	delete(b.archived, id)
	delete(b.tasks, id)
	if rec.uniqueKey != "" {
		delete(b.uniqueKeys, rec.uniqueKey)
	}
	return nil
}

func (b *memoryBroker) runTaskLocked(queue, id string) error {
	rec := b.findTaskLocked(queue, id)
	if rec == nil {
		return oops.Errorf("task %q not found in queue %q", id, queue)
	}
	switch rec.info.State {
	case asynq.TaskStateScheduled, asynq.TaskStateRetry, asynq.TaskStateArchived:
	default:
		return oops.Errorf("task %q in queue %q cannot be run from state %s", id, queue, rec.info.State)
	}

	b.removeTaskFromCurrentStateLocked(rec)
	rec.processAt = time.Time{}
	b.enqueuePendingRecordLocked(rec, time.Now())
	return nil
}

func (b *memoryBroker) runAllTasksInStateLocked(queue string, state asynq.TaskState) int {
	ids := b.collectTaskIDsByStateLocked(queue, state)
	for _, id := range ids {
		_ = b.runTaskLocked(queue, id)
	}
	return len(ids)
}

func (b *memoryBroker) deleteAllTasksInStateLocked(queue string, state asynq.TaskState) int {
	ids := b.collectTaskIDsByStateLocked(queue, state)
	for _, id := range ids {
		_ = b.deleteTaskLocked(queue, id)
	}
	return len(ids)
}

func (b *memoryBroker) collectTaskIDsByStateLocked(queue string, state asynq.TaskState) []string {
	tasks := b.listTasksByStateLocked(queue, state)
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	return ids
}

func (b *memoryBroker) removeTaskFromCurrentStateLocked(rec *taskRecord) {
	switch rec.info.State {
	case asynq.TaskStatePending:
		b.deletePendingTaskLocked(rec.info.Queue, rec.info.ID)
	case asynq.TaskStateScheduled, asynq.TaskStateRetry, asynq.TaskStateCompleted:
		if rec.heapIndex >= 0 && rec.heapIndex < b.scheduled.Len() {
			heap.Remove(&b.scheduled, rec.heapIndex)
		}
	case asynq.TaskStateArchived:
		delete(b.archived, rec.info.ID)
	case asynq.TaskStateAggregating:
		b.deleteGroupedTaskLocked(rec.info.Queue, rec.info.ID)
	case asynq.TaskStateActive:
		delete(b.active, rec.info.ID)
	default:
		// Nothing to remove for unknown states.
	}
}

func (b *memoryBroker) deletePendingTaskLocked(queue, id string) {
	q := b.getQueueLocked(queue)
	if q == nil {
		return
	}
	for idx, rec := range q.pending {
		if rec.info.ID == id {
			q.pending = append(q.pending[:idx], q.pending[idx+1:]...)
			return
		}
	}
}

func (b *memoryBroker) deleteGroupedTaskLocked(queue, id string) {
	q := b.getQueueLocked(queue)
	if q == nil {
		return
	}
	for name, group := range q.groups {
		for idx, rec := range group.tasks {
			if rec.info.ID == id {
				group.tasks = append(group.tasks[:idx], group.tasks[idx+1:]...)
				if len(group.tasks) == 0 {
					delete(q.groups, name)
				}
				return
			}
		}
	}
}

func (b *memoryBroker) popReadyLocked(cfg asynq.Config, rrIndex *int) *taskRecord {
	q := b.selectQueueLocked(cfg, rrIndex)
	if q == nil || len(q.pending) == 0 {
		return nil
	}

	rec := q.pending[0]
	q.pending = q.pending[1:]
	rec.info.State = asynq.TaskStateActive
	rec.info.IsOrphaned = false
	b.active[rec.info.ID] = rec
	return rec
}

func (b *memoryBroker) selectQueueLocked(cfg asynq.Config, rrIndex *int) *memoryQueue {
	queues := cfg.Queues
	if len(queues) == 0 {
		queues = map[string]int{defaultRecurringTaskQueue: 1}
	}
	if cfg.StrictPriority {
		return b.selectStrictQueueLocked(queues)
	}
	return b.selectWeightedQueueLocked(queues, rrIndex)
}

func (b *memoryBroker) selectStrictQueueLocked(queues map[string]int) *memoryQueue {
	type pair struct {
		name   string
		weight int
	}

	all := make([]pair, 0, len(queues))
	for name, weight := range queues {
		all = append(all, pair{name: name, weight: weight})
	}

	slices.SortFunc(all, func(a, b pair) int {
		if a.weight == b.weight {
			switch {
			case a.name < b.name:
				return -1
			case a.name > b.name:
				return 1
			default:
				return 0
			}
		}
		if a.weight > b.weight {
			return -1
		}
		return 1
	})

	for _, item := range all {
		q := b.queues[item.name]
		if q != nil && !q.paused && len(q.pending) > 0 {
			return q
		}
	}
	return nil
}

func (b *memoryBroker) selectWeightedQueueLocked(queues map[string]int, rrIndex *int) *memoryQueue {
	expanded := expandWeightedQueues(queues)
	if len(expanded) == 0 {
		expanded = []string{defaultRecurringTaskQueue}
	}

	start := 0
	if rrIndex != nil {
		start = *rrIndex % len(expanded)
	}

	for i := range expanded {
		name := expanded[(start+i)%len(expanded)]
		q := b.queues[name]
		if q != nil && !q.paused && len(q.pending) > 0 {
			if rrIndex != nil {
				*rrIndex = (start + i + 1) % len(expanded)
			}
			return q
		}
	}
	return nil
}

func expandWeightedQueues(queues map[string]int) []string {
	names := make([]string, 0, len(queues))
	for name := range queues {
		names = append(names, name)
	}
	slices.Sort(names)

	expanded := make([]string, 0, len(queues))
	for _, name := range names {
		weight := queues[name]
		if weight <= 0 {
			continue
		}
		for range weight {
			expanded = append(expanded, name)
		}
	}
	return expanded
}

func (b *memoryBroker) finishTaskLocked(rec *taskRecord, now time.Time) {
	q := b.getOrCreateQueueLocked(rec.info.Queue)
	q.processedTotal++

	rec.info.State = asynq.TaskStateCompleted
	rec.info.CompletedAt = now
	rec.info.LastErr = ""

	if rec.uniqueKey != "" {
		delete(b.uniqueKeys, rec.uniqueKey)
	}
	if rec.retention <= 0 {
		delete(b.tasks, rec.info.ID)
		return
	}

	rec.processAt = now.Add(rec.retention)
	b.completed[rec.info.ID] = rec
	b.scheduleRecordLocked(rec, asynq.TaskStateCompleted)
}

func (b *memoryBroker) failTaskLocked(
	ctx context.Context,
	cfg asynq.Config,
	rec *taskRecord,
	err error,
	now time.Time,
) {
	q := b.getOrCreateQueueLocked(rec.info.Queue)
	q.processedTotal++
	q.failedTotal++

	rec.info.Retried++
	rec.info.LastErr = err.Error()
	rec.info.LastFailedAt = now

	if cfg.ErrorHandler != nil {
		cfg.ErrorHandler.HandleError(ctx, rec.task, err)
	}
	if errors.Is(err, asynq.SkipRetry) || rec.info.Retried > rec.info.MaxRetry {
		rec.info.State = asynq.TaskStateArchived
		rec.processAt = time.Time{}
		b.archived[rec.info.ID] = rec
		return
	}

	rec.processAt = now.Add(retryDelay(cfg, rec.info.Retried, err, rec.task))
	b.scheduleRecordLocked(rec, asynq.TaskStateRetry)
}

func (b *memoryBroker) queueInfoLocked(queue string) *asynq.QueueInfo {
	now := time.Now()
	b.expireLocked(now)
	b.flushGroupsLocked(now)

	q := b.getOrCreateQueueLocked(queue)
	info := &asynq.QueueInfo{
		Queue:          queue,
		Pending:        len(q.pending),
		Paused:         q.paused,
		ProcessedTotal: int(q.processedTotal),
		FailedTotal:    int(q.failedTotal),
		Processed:      int(q.processedTotal),
		Failed:         int(q.failedTotal),
		Timestamp:      now,
	}

	for _, rec := range b.active {
		if rec.info.Queue == queue {
			info.Active++
		}
	}
	for _, rec := range b.scheduled {
		if rec.info.Queue != queue {
			continue
		}
		switch rec.info.State {
		case asynq.TaskStateScheduled:
			info.Scheduled++
		case asynq.TaskStateRetry:
			info.Retry++
		case asynq.TaskStateCompleted:
			info.Completed++
		case asynq.TaskStatePending, asynq.TaskStateActive, asynq.TaskStateArchived, asynq.TaskStateAggregating:
			// These states are tracked elsewhere in QueueInfo.
		default:
			// Unknown states are ignored.
		}
	}
	for _, rec := range b.archived {
		if rec.info.Queue == queue {
			info.Archived++
		}
	}
	for _, group := range q.groups {
		info.Groups++
		info.Aggregating += len(group.tasks)
	}

	info.Size = info.Pending +
		info.Active +
		info.Scheduled +
		info.Retry +
		info.Archived +
		info.Completed +
		info.Aggregating
	if len(q.pending) > 0 {
		info.Latency = now.Sub(q.pending[0].createdAt)
	}
	return info
}

func (b *memoryBroker) expireLocked(now time.Time) {
	b.expireUniqueKeysLocked(now)
	for b.scheduled.Len() > 0 {
		rec := b.scheduled[0]
		if rec.processAt.After(now) {
			return
		}

		heap.Pop(&b.scheduled)
		switch rec.info.State {
		case asynq.TaskStateCompleted:
			delete(b.completed, rec.info.ID)
			delete(b.tasks, rec.info.ID)
		case asynq.TaskStateScheduled, asynq.TaskStateRetry:
			b.enqueueDueLocked(rec, now)
		case asynq.TaskStatePending, asynq.TaskStateActive, asynq.TaskStateArchived, asynq.TaskStateAggregating:
			// These states should not be in the scheduled heap, but ignore defensively.
		default:
			// Unknown states are ignored.
		}
	}
}

func (b *memoryBroker) expireUniqueKeysLocked(now time.Time) {
	for key, until := range b.uniqueKeys {
		if !until.IsZero() && !now.Before(until) {
			delete(b.uniqueKeys, key)
		}
	}
}

func (b *memoryBroker) enqueueDueLocked(rec *taskRecord, now time.Time) {
	rec.processAt = time.Time{}
	if rec.info.Group != "" {
		b.enqueueGroupedRecordLocked(rec, now)
		return
	}
	b.enqueuePendingRecordLocked(rec, now)
}

func (b *memoryBroker) flushGroupsLocked(now time.Time) {
	for _, q := range b.queues {
		for name, group := range q.groups {
			switch {
			case len(group.tasks) == 0:
				delete(q.groups, name)
			case b.groupAggregator == nil:
				b.releaseGroupLocked(q, name, group.tasks, now)
			case b.groupReadyLocked(group, now):
				b.aggregateGroupLocked(q, name, group, now)
			}
		}
	}
}

func (b *memoryBroker) releaseGroupLocked(q *memoryQueue, name string, records []*taskRecord, now time.Time) {
	for _, rec := range records {
		rec.info.State = asynq.TaskStatePending
		rec.info.NextProcessAt = now
		q.pending = append(q.pending, rec)
	}
	delete(q.groups, name)
	b.cond.Broadcast()
}

func (b *memoryBroker) aggregateGroupLocked(q *memoryQueue, name string, group *taskGroup, now time.Time) {
	aggregatedTask := b.groupAggregator.Aggregate(name, collectTasks(group.tasks))
	if aggregatedTask == nil {
		b.releaseGroupLocked(q, name, group.tasks, now)
		return
	}

	base := group.tasks[0]
	for _, rec := range group.tasks[1:] {
		delete(b.tasks, rec.info.ID)
		delete(b.completed, rec.info.ID)
		delete(b.archived, rec.info.ID)
	}

	base.task = aggregatedTask
	base.info.Type = aggregatedTask.Type()
	base.info.Payload = append([]byte(nil), aggregatedTask.Payload()...)
	base.info.Headers = cloneHeaders(aggregatedTask.Headers())
	base.info.State = asynq.TaskStatePending
	base.info.NextProcessAt = now

	q.pending = append(q.pending, base)
	delete(q.groups, name)
	b.cond.Broadcast()
}

func (b *memoryBroker) groupReadyLocked(group *taskGroup, now time.Time) bool {
	if b.groupMaxSize > 0 && len(group.tasks) >= b.groupMaxSize {
		return true
	}

	grace := b.groupGracePeriod
	if grace <= 0 {
		grace = time.Minute
	}

	if now.Sub(group.lastAt) >= grace {
		return true
	}
	if b.groupMaxDelay > 0 && now.Sub(group.firstAt) >= b.groupMaxDelay {
		return true
	}
	return false
}

func (b *memoryBroker) signalMaintenanceLocked() {
	select {
	case b.maintenance <- struct{}{}:
	default:
	}
	b.cond.Broadcast()
}

func (b *memoryBroker) maintenanceLoop() {
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()

	for {
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			return
		}

		now := time.Now()
		b.expireLocked(now)
		b.flushGroupsLocked(now)
		next := b.nextWakeLocked(now)
		b.mu.Unlock()

		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(next)

		select {
		case <-b.maintenance:
		case <-timer.C:
		}
	}
}

func (b *memoryBroker) nextWakeLocked(now time.Time) time.Duration {
	next := time.Second
	if b.scheduled.Len() > 0 {
		next = maxDuration(0, time.Until(b.scheduled[0].processAt))
	}

	for _, q := range b.queues {
		for _, group := range q.groups {
			next = minDuration(next, maxDuration(0, group.lastAt.Add(groupGrace(b.groupGracePeriod)).Sub(now)))
			if b.groupMaxDelay > 0 {
				next = minDuration(next, maxDuration(0, group.firstAt.Add(b.groupMaxDelay).Sub(now)))
			}
		}
	}

	if next <= 0 {
		return 10 * time.Millisecond
	}
	return next
}

func groupGrace(d time.Duration) time.Duration {
	if d <= 0 {
		return time.Minute
	}
	return d
}

func minDuration(a, b time.Duration) time.Duration {
	if b < a {
		return b
	}
	return a
}

func maxDuration(a, b time.Duration) time.Duration {
	if b > a {
		return b
	}
	return a
}
