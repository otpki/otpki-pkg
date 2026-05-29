package asynqwrap

import (
	"net/http"

	"github.com/hibiken/asynq"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	metricsLabelBackend = "backend"
	metricsLabelQueue   = "queue"
	metricsLabelState   = "state"
)

func newMetricsHandler(c prometheus.Collector) http.Handler {
	reg := prometheus.NewRegistry()
	reg.MustRegister(c)
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
}

type redisCollector struct {
	inspector *asynq.Inspector

	sizeDesc      *prometheus.Desc
	latencyDesc   *prometheus.Desc
	pausedDesc    *prometheus.Desc
	processedDesc *prometheus.Desc
	failedDesc    *prometheus.Desc
}

func newRedisCollector(inspector *asynq.Inspector) prometheus.Collector {
	return &redisCollector{
		inspector: inspector,
		sizeDesc: prometheus.NewDesc(
			"asynqwrap_queue_tasks",
			"Task counts by queue and state.",
			[]string{metricsLabelBackend, metricsLabelQueue, metricsLabelState},
			nil,
		),
		latencyDesc: prometheus.NewDesc(
			"asynqwrap_queue_latency_seconds",
			"Oldest pending task latency by queue.",
			[]string{metricsLabelBackend, metricsLabelQueue},
			nil,
		),
		pausedDesc: prometheus.NewDesc(
			"asynqwrap_queue_paused",
			"Queue paused state.",
			[]string{metricsLabelBackend, metricsLabelQueue},
			nil,
		),
		processedDesc: prometheus.NewDesc(
			"asynqwrap_queue_processed_total",
			"Processed task total by queue.",
			[]string{metricsLabelBackend, metricsLabelQueue},
			nil,
		),
		failedDesc: prometheus.NewDesc(
			"asynqwrap_queue_failed_total",
			"Failed task total by queue.",
			[]string{metricsLabelBackend, metricsLabelQueue},
			nil,
		),
	}
}

func (c *redisCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.sizeDesc
	ch <- c.latencyDesc
	ch <- c.pausedDesc
	ch <- c.processedDesc
	ch <- c.failedDesc
}

func (c *redisCollector) Collect(ch chan<- prometheus.Metric) {
	queues, err := c.inspector.Queues()
	if err != nil {
		return
	}
	for _, q := range queues {
		info, err := c.inspector.GetQueueInfo(q)
		if err != nil {
			continue
		}
		emitQueueMetrics(ch, c.sizeDesc, c.latencyDesc, c.pausedDesc, c.processedDesc, c.failedDesc, "redis", info)
	}
}

func emitQueueMetrics(
	ch chan<- prometheus.Metric,
	sizeDesc, latencyDesc, pausedDesc, processedDesc, failedDesc *prometheus.Desc,
	backend string,
	info *asynq.QueueInfo,
) {
	states := map[string]float64{
		"pending":     float64(info.Pending),
		"active":      float64(info.Active),
		"scheduled":   float64(info.Scheduled),
		"retry":       float64(info.Retry),
		"archived":    float64(info.Archived),
		"completed":   float64(info.Completed),
		"aggregating": float64(info.Aggregating),
	}
	for state, value := range states {
		ch <- prometheus.MustNewConstMetric(sizeDesc, prometheus.GaugeValue, value, backend, info.Queue, state)
	}
	paused := 0.0
	if info.Paused {
		paused = 1
	}
	ch <- prometheus.MustNewConstMetric(latencyDesc, prometheus.GaugeValue, info.Latency.Seconds(), backend, info.Queue)
	ch <- prometheus.MustNewConstMetric(pausedDesc, prometheus.GaugeValue, paused, backend, info.Queue)
	ch <- prometheus.MustNewConstMetric(
		processedDesc,
		prometheus.CounterValue,
		float64(info.ProcessedTotal),
		backend,
		info.Queue,
	)
	ch <- prometheus.MustNewConstMetric(
		failedDesc,
		prometheus.CounterValue,
		float64(info.FailedTotal),
		backend,
		info.Queue,
	)
}
