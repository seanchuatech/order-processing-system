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

	SQSPublishTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sqs_publish_total",
			Help: "Total number of SQS messages published",
		},
		[]string{"queue", "status"},
	)

	SQSPublishDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "sqs_publish_duration_seconds",
			Help:    "Latency of SQS message publication in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"queue", "status"},
	)

	SNSPublishTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sns_publish_total",
			Help: "Total number of SNS messages published",
		},
		[]string{"topic", "status"},
	)

	SNSPublishDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "sns_publish_duration_seconds",
			Help:    "Latency of SNS message publication in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"topic", "status"},
	)
)
