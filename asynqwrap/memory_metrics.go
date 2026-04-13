package asynqwrap

import (
	"github.com/prometheus/client_golang/prometheus"
)

type memoryCollector struct {
	broker *memoryBroker

	sizeDesc      *prometheus.Desc
	latencyDesc   *prometheus.Desc
	pausedDesc    *prometheus.Desc
	processedDesc *prometheus.Desc
	failedDesc    *prometheus.Desc
}

func newMemoryCollector(b *memoryBroker) prometheus.Collector {
	return &memoryCollector{
		broker: b,
		sizeDesc: prometheus.NewDesc(
			"asynqwrap_queue_tasks",
			"Task counts by queue and state.",
			[]string{"backend", "queue", "state"},
			nil,
		),
		latencyDesc: prometheus.NewDesc(
			"asynqwrap_queue_latency_seconds",
			"Oldest pending task latency by queue.",
			[]string{"backend", "queue"},
			nil,
		),
		pausedDesc: prometheus.NewDesc(
			"asynqwrap_queue_paused",
			"Queue paused state.",
			[]string{"backend", "queue"},
			nil,
		),
		processedDesc: prometheus.NewDesc(
			"asynqwrap_queue_processed_total",
			"Processed task total by queue.",
			[]string{"backend", "queue"},
			nil,
		),
		failedDesc: prometheus.NewDesc(
			"asynqwrap_queue_failed_total",
			"Failed task total by queue.",
			[]string{"backend", "queue"},
			nil,
		),
	}
}

func (c *memoryCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.sizeDesc
	ch <- c.latencyDesc
	ch <- c.pausedDesc
	ch <- c.processedDesc
	ch <- c.failedDesc
}

func (c *memoryCollector) Collect(ch chan<- prometheus.Metric) {
	c.broker.mu.Lock()
	defer c.broker.mu.Unlock()

	for name := range c.broker.queues {
		info := c.broker.queueInfoLocked(name)
		emitQueueMetrics(ch, c.sizeDesc, c.latencyDesc, c.pausedDesc, c.processedDesc, c.failedDesc, "memory", info)
	}
}
