package chatapps

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// MessagesAggregatedTotal counts total messages aggregated
	MessagesAggregatedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "hotplex_aggregator_messages_aggregated_total",
			Help: "Total number of messages that have been aggregated",
		},
		[]string{"event_type", "platform"},
	)

	// MessagesFlushedTotal counts total flushes by reason
	MessagesFlushedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "hotplex_aggregator_messages_flushed_total",
			Help: "Total number of times the buffer was flushed",
		},
		[]string{"event_type", "platform", "reason"},
	)

	// MessagesDroppedTotal counts dropped messages
	MessagesDroppedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "hotplex_aggregator_messages_dropped_total",
			Help: "Total number of messages dropped due to buffer overflow",
		},
		[]string{"event_type", "platform", "reason"},
	)

	// BufferSizeGauge tracks current buffer size per platform
	BufferSizeGauge = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "hotplex_aggregator_buffer_size",
			Help: "Current number of messages in the buffer per platform",
		},
		[]string{"platform"},
	)

	// BufferDurationHistogram tracks time from first message to flush
	BufferDurationHistogram = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "hotplex_aggregator_buffer_duration_seconds",
			Help:    "Time in seconds from first message arrival to buffer flush",
			Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0},
		},
		[]string{"platform"},
	)

	// MessageSizeHistogram tracks message size distribution
	MessageSizeHistogram = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "hotplex_aggregator_message_size_bytes",
			Help:    "Size distribution of aggregated messages in bytes",
			Buckets: []float64{64, 256, 512, 1024, 2048, 4096, 8192, 16384, 32768},
		},
		[]string{"event_type", "platform"},
	)
)
