package asynqwrap

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"github.com/samber/oops"
)

const defaultPeriodicTaskSyncInterval = 3 * time.Minute

type periodicTaskManager struct {
	scheduler    Scheduler
	provider     asynq.PeriodicTaskConfigProvider
	syncInterval time.Duration
	logger       *leveledLogger

	done chan struct{}
	wg   sync.WaitGroup

	mu      sync.Mutex
	entries map[string]string
}

type redisPeriodicTaskManager struct {
	manager *asynq.PeriodicTaskManager
}

func (m *redisPeriodicTaskManager) Start() error { return m.manager.Start() }
func (m *redisPeriodicTaskManager) Run() error   { return m.manager.Run() }
func (m *redisPeriodicTaskManager) Shutdown()    { m.manager.Shutdown() }

func newPeriodicTaskManager(scheduler Scheduler, opts PeriodicTaskManagerOpts) (PeriodicTaskManager, error) {
	if opts.PeriodicTaskConfigProvider == nil {
		return nil, oops.Errorf("PeriodicTaskConfigProvider cannot be nil")
	}
	if scheduler == nil {
		return nil, oops.Errorf("scheduler cannot be nil")
	}
	syncInterval := opts.SyncInterval
	if syncInterval == 0 {
		syncInterval = defaultPeriodicTaskSyncInterval
	}
	var schedOpts asynq.SchedulerOpts
	if opts.SchedulerOpts != nil {
		schedOpts = *opts.SchedulerOpts
	}
	return &periodicTaskManager{
		scheduler:    scheduler,
		provider:     opts.PeriodicTaskConfigProvider,
		syncInterval: syncInterval,
		logger:       newLeveledLogger(schedOpts.Logger, schedOpts.LogLevel),
		done:         make(chan struct{}),
		entries:      make(map[string]string),
	}, nil
}

func (m *periodicTaskManager) Start() error {
	if err := m.initialSync(); err != nil {
		return oops.Errorf("asynq: %w", err)
	}
	if err := m.scheduler.Start(); err != nil {
		return oops.Errorf("asynq: %w", err)
	}
	m.wg.Go(func() {
		ticker := time.NewTicker(m.syncInterval)
		defer ticker.Stop()
		for {
			select {
			case <-m.done:
				m.logger.Debugf("Stopping periodic task syncer")
				return
			case <-ticker.C:
				m.sync()
			}
		}
	})
	return nil
}

func (m *periodicTaskManager) Run() error {
	if err := m.Start(); err != nil {
		return err
	}
	<-m.done
	m.logger.Debugf("PeriodicTaskManager exiting")
	return nil
}

func (m *periodicTaskManager) Shutdown() {
	select {
	case <-m.done:
	default:
		close(m.done)
	}
	m.wg.Wait()
	m.scheduler.Shutdown()
}

func (m *periodicTaskManager) initialSync() error {
	configs, err := m.provider.GetConfigs()
	if err != nil {
		return oops.Errorf("initial call to GetConfigs failed: %w", err)
	}
	for _, c := range configs {
		if err := validatePeriodicTaskConfig(c); err != nil {
			return oops.Errorf("initial call to GetConfigs contained an invalid config: %w", err)
		}
	}
	m.add(configs)
	return nil
}

func (m *periodicTaskManager) sync() {
	configs, err := m.provider.GetConfigs()
	if err != nil {
		m.logger.Errorf("Failed to get periodic task configs: %v", err)
		return
	}
	for _, c := range configs {
		if err := validatePeriodicTaskConfig(c); err != nil {
			m.logger.Errorf("Failed to sync: GetConfigs returned an invalid config: %v", err)
			return
		}
	}
	m.remove(m.diffRemoved(configs))
	m.add(m.diffAdded(configs))
}

func (m *periodicTaskManager) add(configs []*asynq.PeriodicTaskConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range configs {
		entryID, err := m.scheduler.Register(c.Cronspec, c.Task, c.Opts...)
		if err != nil {
			m.logger.Errorf(
				"Failed to register periodic task: cronspec=%q task=%q err=%v",
				c.Cronspec,
				c.Task.Type(),
				err,
			)
			continue
		}
		m.entries[periodicTaskConfigHash(c)] = entryID
		m.logger.Infof(
			"Successfully registered periodic task: cronspec=%q task=%q, entryID=%s",
			c.Cronspec,
			c.Task.Type(),
			entryID,
		)
	}
}

func (m *periodicTaskManager) remove(removed map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for hash, entryID := range removed {
		if err := m.scheduler.Unregister(entryID); err != nil {
			m.logger.Errorf("Failed to unregister periodic task: %v", err)
			continue
		}
		delete(m.entries, hash)
		m.logger.Infof("Successfully unregistered periodic task: entryID=%s", entryID)
	}
}

func (m *periodicTaskManager) diffRemoved(configs []*asynq.PeriodicTaskConfig) map[string]string {
	incoming := make(map[string]struct{}, len(configs))
	for _, c := range configs {
		incoming[periodicTaskConfigHash(c)] = struct{}{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	removed := make(map[string]string)
	for hash, entryID := range m.entries {
		if _, ok := incoming[hash]; !ok {
			removed[hash] = entryID
		}
	}
	return removed
}

func (m *periodicTaskManager) diffAdded(configs []*asynq.PeriodicTaskConfig) []*asynq.PeriodicTaskConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	var added []*asynq.PeriodicTaskConfig
	for _, c := range configs {
		if _, ok := m.entries[periodicTaskConfigHash(c)]; !ok {
			added = append(added, c)
		}
	}
	return added
}

func validatePeriodicTaskConfig(c *asynq.PeriodicTaskConfig) error {
	if c == nil {
		return oops.Errorf("PeriodicTaskConfig cannot be nil")
	}
	if c.Task == nil {
		return oops.Errorf("PeriodicTaskConfig.Task cannot be nil")
	}
	if c.Cronspec == "" {
		return oops.Errorf("PeriodicTaskConfig.Cronspec cannot be empty")
	}
	return nil
}

func periodicTaskConfigHash(c *asynq.PeriodicTaskConfig) string {
	h := sha256.New()
	_, _ = h.Write([]byte(c.Cronspec))
	_, _ = h.Write([]byte(c.Task.Type()))
	_, _ = h.Write(c.Task.Payload())
	opts := make([]string, 0, len(c.Opts))
	for _, opt := range c.Opts {
		opts = append(opts, opt.String())
	}
	sort.Strings(opts)
	for _, opt := range opts {
		_, _ = h.Write([]byte(opt))
	}
	return hex.EncodeToString(h.Sum(nil))
}
