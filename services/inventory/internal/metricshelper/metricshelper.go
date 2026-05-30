package metricshelper

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	SQSConsumeTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sqs_consume_total",
			Help: "Total number of SQS messages consumed",
		},
		[]string{"queue", "status"},
	)

	SQSConsumeDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "sqs_consume_duration_seconds",
			Help:    "Latency of SQS message processing in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"queue", "status"},
	)
)
