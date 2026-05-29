package asynqwrap

import (
	"context"
	"errors"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	spfypubsub "github.com/otpki/otpki-pkg/spfy/pulse"
	"github.com/samber/oops"
)

type spfyServer struct {
	broker   *spfyBroker
	cfg      asynq.Config
	serverID string
	logger   *leveledLogger

	mu       sync.Mutex
	started  bool
	stopped  bool
	done     chan struct{}
	stopPull chan struct{}
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	rrIndex  int
	wake     chan string
}

func (s *spfyServer) Ping() error { return s.broker.Ping() }

func (s *spfyServer) Start(handler asynq.Handler) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return nil
	}
	s.started = true
	s.stopPull = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel

	for _, q := range s.queueNames() {
		s.wg.Add(1)
		go s.subscribeLoop(ctx, q)
	}
	for range s.workerCount() {
		s.wg.Add(1)
		go s.workerLoop(handler)
	}
	return nil
}

func (s *spfyServer) Run(handler asynq.Handler) error {
	if err := s.Start(handler); err != nil {
		return err
	}
	<-s.done
	return nil
}

func (s *spfyServer) Stop() {
	s.mu.Lock()
	if !s.started || s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	close(s.stopPull)
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()

	s.wg.Wait()
	close(s.done)
}

func (s *spfyServer) Shutdown() { s.Stop() }

func (s *spfyServer) workerCount() int {
	if s.cfg.Concurrency <= 0 {
		return 1
	}
	return s.cfg.Concurrency
}

func (s *spfyServer) queueNames() []string {
	queues := s.cfg.Queues
	if len(queues) == 0 {
		queues = map[string]int{defaultRecurringTaskQueue: 1}
	}
	names := make([]string, 0, len(queues))
	for name := range queues {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func (s *spfyServer) subscribeLoop(ctx context.Context, queue string) {
	defer s.wg.Done()
	sub, err := s.broker.pubsub.Subscribe(
		ctx,
		s.broker.topic(queue),
		func(ctx context.Context, msg spfypubsub.Message) error {
			select {
			case s.wake <- queue:
			default:
			}
			return nil
		},
		spfypubsub.SubscribeOptions{Reliability: spfypubsub.Ephemeral},
	)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		s.logger.Errorf("spfy pubsub subscribe %q failed: %v", queue, err)
		return
	}
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := sub.Close(shutdownCtx); err != nil {
		s.logger.Errorf("spfy pubsub subscription close %q failed: %v", queue, err)
	}
}

func (s *spfyServer) workerLoop(handler asynq.Handler) {
	defer s.wg.Done()
	for {
		env := s.nextTask()
		if env == nil {
			return
		}
		s.processTask(handler, env)
	}
}

func (s *spfyServer) nextTask() *spfyTaskEnvelope {
	ticker := time.NewTicker(s.broker.cfg.Spfy.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopPull:
			return nil
		default:
		}
		ctx, cancel := context.WithTimeout(context.Background(), s.broker.cfg.Spfy.PollInterval)
		now := s.broker.now()
		_ = s.broker.promoteDue(ctx, now)
		_ = s.broker.flushGroups(ctx, now, s.cfg)
		_ = s.broker.recoverLeases(ctx, now, s.cfg)
		env, err := s.claimNext(ctx, now)
		cancel()
		if err != nil {
			s.logger.Errorf("spfy claim task failed: %v", err)
		} else if env != nil {
			return env
		}
		select {
		case <-s.stopPull:
			return nil
		case <-s.wake:
		case <-ticker.C:
		}
	}
}

func (s *spfyServer) claimNext(ctx context.Context, now time.Time) (*spfyTaskEnvelope, error) {
	for _, queue := range s.candidateQueues() {
		qdoc, _, ok, err := s.broker.getQueueDoc(ctx, queue)
		if err != nil {
			return nil, err
		}
		if ok && qdoc.Paused {
			continue
		}
		tasks, err := s.broker.listTaskEnvelopesByFilter(ctx, []string{queue}, func(doc *spfyTaskDoc) bool {
			return doc.State == asynq.TaskStatePending.String()
		}, func(a, b *spfyTaskDoc) int {
			if a.NextProcessAtUnixNano != b.NextProcessAtUnixNano {
				return compareInt64(a.NextProcessAtUnixNano, b.NextProcessAtUnixNano)
			}
			if a.CreatedAtUnixNano != b.CreatedAtUnixNano {
				return compareInt64(a.CreatedAtUnixNano, b.CreatedAtUnixNano)
			}
			return strings.Compare(a.ID, b.ID)
		}, 10)
		if err != nil {
			return nil, err
		}
		for _, env := range tasks {
			doc, _ := env.snapshot()
			current, err := s.broker.isCurrentRecurringTask(ctx, doc.Headers)
			if err != nil {
				return nil, err
			}
			if !current {
				if err := s.broker.discardTask(ctx, env); err != nil {
					if isETagConflict(err) {
						continue
					}
					return nil, err
				}
				continue
			}
			claimed, err := s.tryClaim(ctx, env, now)
			if err != nil {
				return nil, err
			}
			if claimed != nil {
				return claimed, nil
			}
		}
	}
	return nil, nil
}

func (s *spfyServer) candidateQueues() []string {
	queues := s.cfg.Queues
	if len(queues) == 0 {
		queues = map[string]int{defaultRecurringTaskQueue: 1}
	}
	if s.cfg.StrictPriority {
		type pair struct {
			name   string
			weight int
		}
		pairs := make([]pair, 0, len(queues))
		for name, weight := range queues {
			pairs = append(pairs, pair{name: name, weight: weight})
		}
		slices.SortFunc(pairs, func(a, b pair) int {
			if a.weight == b.weight {
				return strings.Compare(a.name, b.name)
			}
			if a.weight > b.weight {
				return -1
			}
			return 1
		})
		out := make([]string, 0, len(pairs))
		for _, p := range pairs {
			if p.weight > 0 {
				out = append(out, p.name)
			}
		}
		return out
	}
	expanded := expandWeightedQueues(queues)
	if len(expanded) == 0 {
		expanded = []string{defaultRecurringTaskQueue}
	}
	s.mu.Lock()
	start := s.rrIndex % len(expanded)
	s.rrIndex = (start + 1) % len(expanded)
	s.mu.Unlock()

	out := make([]string, 0, len(expanded))
	for i := range expanded {
		out = append(out, expanded[(start+i)%len(expanded)])
	}
	return out
}

func (s *spfyServer) tryClaim(ctx context.Context, env *spfyTaskEnvelope, now time.Time) (*spfyTaskEnvelope, error) {
	doc, etag := env.snapshot()
	if doc.State != asynq.TaskStatePending.String() {
		return nil, nil
	}
	doc.State = asynq.TaskStateActive.String()
	doc.LeaseID = s.serverID + ":" + uuid.NewString()
	doc.LeaseUntil = now.Add(s.broker.cfg.Spfy.WorkerLeaseTTL)
	doc.IsOrphaned = false
	doc.NextProcessAt = time.Time{}
	doc.refreshUnixFields()
	err := s.broker.applyStateOperation(ctx, spfySetRequest(env.key, doc, etag, false, s.broker.stateMetadata(0)))
	if err != nil {
		if isETagConflict(err) {
			return nil, nil
		}
		return nil, err
	}
	latest, ok, err := s.broker.getTask(ctx, doc.ID)
	if err != nil || !ok {
		return nil, err
	}
	return latest, nil
}

func (s *spfyServer) processTask(handler asynq.Handler, env *spfyTaskEnvelope) {
	doc, _ := env.snapshot()
	rec := doc.taskRecord()
	ctx, cancel := s.taskContext(rec)
	defer cancel()

	heartbeatCtx, heartbeatCancel := context.WithCancel(context.Background())
	heartbeatDone := make(chan struct{})
	go s.heartbeat(heartbeatCtx, env, heartbeatDone)

	err := s.runHandler(ctx, handler, rec)
	heartbeatCancel()
	<-heartbeatDone
	if err := s.finishProcessing(context.Background(), env, err); err != nil {
		s.logger.Errorf("spfy finish task %q failed: %v", doc.ID, err)
	}
}

func (s *spfyServer) taskContext(rec *taskRecord) (context.Context, context.CancelFunc) {
	base := context.Background()
	if s.cfg.BaseContext != nil {
		base = s.cfg.BaseContext()
	}
	if deadline, ok := taskDeadline(rec.info, s.broker.now()); ok {
		return context.WithDeadline(base, deadline)
	}
	return base, func() {}
}

func (s *spfyServer) runHandler(ctx context.Context, handler asynq.Handler, rec *taskRecord) error {
	errCh := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				err := oops.Errorf("panic recovered: %v\n%s", r, debug.Stack())
				s.logger.Errorf("%v", err)
				errCh <- err
			}
		}()
		errCh <- handler.ProcessTask(ctx, rec.task)
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *spfyServer) heartbeat(ctx context.Context, env *spfyTaskEnvelope, done chan<- struct{}) {
	defer close(done)
	interval := s.broker.cfg.Spfy.WorkerLeaseTTL / 2
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.extendLease(ctx, env); err != nil {
				return
			}
		}
	}
}

func (s *spfyServer) extendLease(ctx context.Context, env *spfyTaskEnvelope) error {
	doc, etag := env.snapshot()
	if doc.State != asynq.TaskStateActive.String() || doc.LeaseID == "" {
		return oops.New("task is no longer active")
	}
	doc.LeaseUntil = s.broker.now().Add(s.broker.cfg.Spfy.WorkerLeaseTTL)
	doc.refreshUnixFields()
	if err := s.broker.applyStateOperation(
		ctx,
		spfySetRequest(env.key, doc, etag, false, s.broker.stateMetadata(0)),
	); err != nil {
		return err
	}
	latest, ok, err := s.broker.getTask(context.Background(), doc.ID)
	if err != nil || !ok {
		return err
	}
	env.replace(latest.doc, latest.etag)
	return nil
}

func (s *spfyServer) finishProcessing(ctx context.Context, env *spfyTaskEnvelope, taskErr error) error {
	if taskErr == nil || errors.Is(taskErr, asynq.RevokeTask) {
		return s.completeTask(ctx, env)
	}
	doc, _ := env.snapshot()
	return s.failTask(ctx, env, taskErr, doc.taskRecord())
}

func (s *spfyServer) completeTask(ctx context.Context, env *spfyTaskEnvelope) error {
	for range 5 {
		doc, etag := env.snapshot()
		if doc.State != asynq.TaskStateActive.String() {
			return nil
		}
		now := s.broker.now()
		qdoc, qetag, ok, err := s.broker.getQueueDoc(ctx, doc.Queue)
		if err != nil {
			return err
		}
		if !ok {
			if err := s.broker.ensureQueue(ctx, doc.Queue); err != nil {
				return err
			}
			continue
		}
		qdoc.ProcessedTotal++
		qdoc.UpdatedAt = now
		if s.broker.supportsTransactions() {
			ops := []spfyStateOperation{
				spfySetRequest(s.broker.queueKey(doc.Queue), qdoc, qetag, false, s.broker.stateMetadata(0)),
			}
			if doc.UniqueKey != "" {
				ops = append(ops, spfyDeleteRequest(s.broker.uniqueKey(doc.UniqueKey), nil, s.broker.stateMetadata(0)))
			}
			if doc.RetentionNanos <= 0 {
				taskSetDoc, taskSetETag, taskSetOK, err := s.broker.getStringSetDoc(
					ctx,
					s.broker.queueTasksKey(doc.Queue),
				)
				if err != nil {
					return err
				}
				if taskSetOK {
					ops = append(
						ops,
						spfySetRequest(
							s.broker.queueTasksKey(doc.Queue),
							removeFromStringSetDoc(taskSetDoc, doc.ID),
							taskSetETag,
							false,
							s.broker.stateMetadata(0),
						),
					)
				}
				ops = append(ops, spfyDeleteRequest(env.key, etag, s.broker.stateMetadata(0)))
			} else {
				doc.State = asynq.TaskStateCompleted.String()
				doc.CompletedAt = now
				doc.LastErr = ""
				doc.LeaseID = ""
				doc.LeaseUntil = time.Time{}
				doc.IsOrphaned = false
				doc.NextProcessAt = now.Add(time.Duration(doc.RetentionNanos))
				doc.refreshUnixFields()
				ops = append(
					ops,
					spfySetRequest(
						env.key,
						doc,
						etag,
						false,
						s.broker.stateMetadata(time.Duration(doc.RetentionNanos)),
					),
				)
			}
			if err := s.broker.transaction(ctx, ops...); err != nil {
				if isETagConflict(err) {
					latest, ok, getErr := s.broker.getTask(ctx, doc.ID)
					if getErr != nil || !ok {
						return getErr
					}
					env.replace(latest.doc, latest.etag)
					continue
				}
				return err
			}
			return nil
		}
		if doc.RetentionNanos <= 0 {
			if err := s.broker.deleteStateKey(ctx, env.key, etag); err != nil {
				if isETagConflict(err) {
					latest, ok, getErr := s.broker.getTask(ctx, doc.ID)
					if getErr != nil || !ok {
						return getErr
					}
					env.replace(latest.doc, latest.etag)
					continue
				}
				return err
			}
		} else {
			doc.State = asynq.TaskStateCompleted.String()
			doc.CompletedAt = now
			doc.LastErr = ""
			doc.LeaseID = ""
			doc.LeaseUntil = time.Time{}
			doc.IsOrphaned = false
			doc.NextProcessAt = now.Add(time.Duration(doc.RetentionNanos))
			doc.refreshUnixFields()
			if err := s.broker.applyStateOperation(
				ctx,
				spfySetRequest(
					env.key,
					doc,
					etag,
					false,
					s.broker.stateMetadata(time.Duration(doc.RetentionNanos)),
				),
			); err != nil {
				if isETagConflict(err) {
					latest, ok, getErr := s.broker.getTask(ctx, doc.ID)
					if getErr != nil || !ok {
						return getErr
					}
					env.replace(latest.doc, latest.etag)
					continue
				}
				return err
			}
		}
		if err := s.broker.applyStateOperation(
			ctx,
			spfySetRequest(s.broker.queueKey(doc.Queue), qdoc, qetag, false, s.broker.stateMetadata(0)),
		); err != nil {
			if isETagConflict(err) {
				continue
			}
			return err
		}
		if doc.UniqueKey != "" {
			_ = s.broker.deleteStateKey(ctx, s.broker.uniqueKey(doc.UniqueKey), nil)
		}
		if doc.RetentionNanos <= 0 {
			_ = s.broker.removeStringSetItems(ctx, s.broker.queueTasksKey(doc.Queue), doc.ID)
		}
		return nil
	}
	return oops.New("complete task failed after ETag retries")
}

func (s *spfyServer) failTask(ctx context.Context, env *spfyTaskEnvelope, taskErr error, rec *taskRecord) error {
	for range 5 {
		doc, etag := env.snapshot()
		if doc.State != asynq.TaskStateActive.String() {
			return nil
		}
		now := s.broker.now()
		qdoc, qetag, ok, err := s.broker.getQueueDoc(ctx, doc.Queue)
		if err != nil {
			return err
		}
		if !ok {
			if err := s.broker.ensureQueue(ctx, doc.Queue); err != nil {
				return err
			}
			continue
		}
		qdoc.ProcessedTotal++
		qdoc.FailedTotal++
		qdoc.UpdatedAt = now

		doc.Retried++
		doc.LastErr = taskErr.Error()
		doc.LastFailedAt = now
		doc.LeaseID = ""
		doc.LeaseUntil = time.Time{}
		doc.IsOrphaned = false
		if s.cfg.ErrorHandler != nil {
			s.cfg.ErrorHandler.HandleError(ctx, rec.task, taskErr)
		}
		if errors.Is(taskErr, asynq.SkipRetry) || doc.Retried > doc.MaxRetry {
			doc.State = asynq.TaskStateArchived.String()
			doc.ProcessAt = time.Time{}
			doc.NextProcessAt = time.Time{}
		} else {
			delay := retryDelay(s.cfg, doc.Retried, taskErr, rec.task)
			doc.ProcessAt = now.Add(delay)
			doc.NextProcessAt = doc.ProcessAt
			doc.State = asynq.TaskStateRetry.String()
		}
		doc.refreshUnixFields()
		if s.broker.supportsTransactions() {
			if err := s.broker.transaction(
				ctx,
				spfySetRequest(s.broker.queueKey(doc.Queue), qdoc, qetag, false, s.broker.stateMetadata(0)),
				spfySetRequest(env.key, doc, etag, false, s.broker.stateMetadata(0)),
			); err != nil {
				if isETagConflict(err) {
					latest, ok, getErr := s.broker.getTask(ctx, doc.ID)
					if getErr != nil || !ok {
						return getErr
					}
					env.replace(latest.doc, latest.etag)
					continue
				}
				return err
			}
			if doc.State == asynq.TaskStatePending.String() {
				_ = s.broker.notifyQueue(ctx, doc.Queue)
			}
			return nil
		}
		if err := s.broker.applyStateOperation(
			ctx,
			spfySetRequest(env.key, doc, etag, false, s.broker.stateMetadata(0)),
		); err != nil {
			if isETagConflict(err) {
				latest, ok, getErr := s.broker.getTask(ctx, doc.ID)
				if getErr != nil || !ok {
					return getErr
				}
				env.replace(latest.doc, latest.etag)
				continue
			}
			return err
		}
		if err := s.broker.applyStateOperation(
			ctx,
			spfySetRequest(s.broker.queueKey(doc.Queue), qdoc, qetag, false, s.broker.stateMetadata(0)),
		); err != nil {
			if isETagConflict(err) {
				continue
			}
			return err
		}
		if doc.State == asynq.TaskStatePending.String() {
			_ = s.broker.notifyQueue(ctx, doc.Queue)
		}
		return nil
	}
	return oops.New("fail task failed after ETag retries")
}
