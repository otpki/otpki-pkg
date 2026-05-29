package asynqwrap

import "github.com/prometheus/client_golang/prometheus"

type spfyCollector struct {
	broker *spfyBroker

	sizeDesc      *prometheus.Desc
	latencyDesc   *prometheus.Desc
	pausedDesc    *prometheus.Desc
	processedDesc *prometheus.Desc
	failedDesc    *prometheus.Desc
}

func newSpfyCollector(b *spfyBroker) prometheus.Collector {
	return &spfyCollector{
		broker: b,
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

func (c *spfyCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.sizeDesc
	ch <- c.latencyDesc
	ch <- c.pausedDesc
	ch <- c.processedDesc
	ch <- c.failedDesc
}

func (c *spfyCollector) Collect(ch chan<- prometheus.Metric) {
	inspector := c.broker.Inspector()
	queues, err := inspector.Queues()
	if err != nil {
		return
	}
	for _, q := range queues {
		info, err := inspector.GetQueueInfo(q)
		if err != nil {
			continue
		}
		emitQueueMetrics(ch, c.sizeDesc, c.latencyDesc, c.pausedDesc, c.processedDesc, c.failedDesc, "spfy", info)
	}
}
