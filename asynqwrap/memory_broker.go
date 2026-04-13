package asynqwrap

import (
	"net/http"
	"time"

	"github.com/hibiken/asynq"
)

func newMemoryBroker() Broker {
	b := &memoryBroker{
		tasks:          make(map[string]*taskRecord),
		queues:         make(map[string]*memoryQueue),
		completed:      make(map[string]*taskRecord),
		archived:       make(map[string]*taskRecord),
		active:         make(map[string]*taskRecord),
		uniqueKeys:     make(map[string]time.Time),
		maintenance:    make(chan struct{}, 1),
		recurringState: newMemoryRecurringStateStore(),
	}
	b.cond = syncCond(&b.mu)
	b.metrics = newMetricsHandler(newMemoryCollector(b))
	go b.maintenanceLoop()
	return b
}

func (b *memoryBroker) Client() Client {
	return newClientAdapter(&memoryClient{broker: b})
}

func (b *memoryBroker) Server(cfg asynq.Config) Server {
	b.mu.Lock()
	b.applyGroupConfigLocked(cfg)
	b.serverSeq++
	id := b.serverSeq
	b.mu.Unlock()

	if cfg.GroupGracePeriod <= 0 {
		cfg.GroupGracePeriod = time.Minute
	}

	return newServerAdapter(&memoryServer{
		broker:   b,
		cfg:      normalizeConfig(cfg),
		serverID: id,
		logger:   newLeveledLogger(cfg.Logger, cfg.LogLevel),
		done:     make(chan struct{}),
	})
}

func (b *memoryBroker) Scheduler(opts *asynq.SchedulerOpts) Scheduler {
	return newMemoryScheduler(b, opts)
}

func (b *memoryBroker) PeriodicTaskManager(opts PeriodicTaskManagerOpts) (PeriodicTaskManager, error) {
	opts.Broker = b
	return NewPeriodicTaskManager(opts)
}

func (b *memoryBroker) Inspector() Inspector {
	return &memoryInspector{broker: b}
}

func (b *memoryBroker) MetricsHandler() http.Handler {
	return b.metrics
}

func (b *memoryBroker) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	b.cond.Broadcast()
	b.mu.Unlock()

	select {
	case b.maintenance <- struct{}{}:
	default:
	}
	return nil
}

func (b *memoryBroker) applyGroupConfigLocked(cfg asynq.Config) {
	if cfg.GroupAggregator != nil {
		b.groupAggregator = cfg.GroupAggregator
	}
	if cfg.GroupGracePeriod > 0 {
		b.groupGracePeriod = cfg.GroupGracePeriod
	}
	if cfg.GroupMaxDelay > 0 {
		b.groupMaxDelay = cfg.GroupMaxDelay
	}
	if cfg.GroupMaxSize > 0 {
		b.groupMaxSize = cfg.GroupMaxSize
	}
}
